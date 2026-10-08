package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type previousSample struct {
	At     time.Time
	Values map[string]float64
}
type Service struct {
	store    *Store
	token    string
	origins  map[string]bool
	mu       sync.Mutex
	tickets  map[string]terminalTicket
	sessions int
	busy     map[string]bool
	previous map[string]previousSample
	history  map[string][]previousSample
	retry    map[string]time.Time
	failures map[string]int
	slots    chan struct{}
}

func newService(store *Store, token string, origins []string) *Service {
	s := &Service{store: store, token: token, origins: map[string]bool{}, tickets: map[string]terminalTicket{}, busy: map[string]bool{}, previous: map[string]previousSample{}, history: map[string][]previousSample{}, retry: map[string]time.Time{}, failures: map[string]int{}, slots: make(chan struct{}, 4)}
	for _, origin := range origins {
		if u, e := url.Parse(strings.TrimSpace(origin)); e == nil && u.Host != "" {
			s.origins[u.Scheme+"://"+u.Host] = true
		}
	}
	return s
}
func (s *Service) originHosts() []string {
	out := []string{}
	for origin := range s.origins {
		u, _ := url.Parse(origin)
		out = append(out, u.Host)
	}
	return out
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(value); e != nil {
		fail(w, 400, "입력 형식 또는 크기를 확인하세요")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "하나의 JSON 객체만 허용됩니다")
		return false
	}
	return true
}
func (s *Service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /terminal/ws", s.terminal)
	api := http.NewServeMux()
	api.HandleFunc("GET /assets", func(w http.ResponseWriter, r *http.Request) {
		assets, e := s.store.List()
		if e != nil {
			fail(w, 503, "등록 목록을 읽을 수 없습니다")
			return
		}
		writeJSON(w, 200, map[string]any{"assets": assets, "configured": true})
	})
	api.HandleFunc("POST /assets", func(w http.ResponseWriter, r *http.Request) {
		var input AssetInput
		if !decode(w, r, &input) {
			return
		}
		if input.ID != "" {
			fail(w, 400, "새 등록에는 ID를 지정하지 않습니다")
			return
		}
		all, e := s.store.All()
		if e != nil || len(all) >= 200 {
			fail(w, 409, "인프라 등록 한도 또는 저장소 상태를 확인하세요")
			return
		}
		asset, e := s.store.Save(input)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		writeJSON(w, 201, asset)
	})
	api.HandleFunc("PUT /assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input AssetInput
		if !decode(w, r, &input) {
			return
		}
		if input.ID != "" && input.ID != r.PathValue("id") {
			fail(w, 400, "ID 불일치")
			return
		}
		input.ID = r.PathValue("id")
		asset, e := s.store.Save(input)
		if e != nil {
			fail(w, 409, e.Error())
			return
		}
		s.mu.Lock()
		delete(s.previous, asset.ID)
		delete(s.history, asset.ID)
		delete(s.retry, asset.ID)
		delete(s.failures, asset.ID)
		s.mu.Unlock()
		writeJSON(w, 200, asset)
	})
	api.HandleFunc("DELETE /assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if e := s.store.Delete(r.PathValue("id")); e != nil {
			fail(w, 409, e.Error())
			return
		}
		s.store.Record(r.PathValue("id"), "asset.delete", "ok")
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	api.HandleFunc("POST /assets/{id}/fingerprint", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		_, fingerprint, e := s.sshConnect(ctx, r.PathValue("id"), true)
		if e != nil {
			fail(w, 422, e.Error())
			return
		}
		s.store.Record(r.PathValue("id"), "ssh.fingerprint.inspect", "ok")
		writeJSON(w, 200, map[string]string{"fingerprint": fingerprint, "notice": "표시된 지문을 별도로 확인한 뒤 등록 정보에 저장하세요. 인증은 수행하지 않았습니다."})
	})
	api.HandleFunc("POST /assets/{id}/connect", func(w http.ResponseWriter, r *http.Request) {
		asset, e := s.store.Get(r.PathValue("id"))
		if e != nil {
			fail(w, 404, e.Error())
			return
		}
		if asset.Asset.Address == "" && !(asset.Asset.Kind == "server" && asset.Asset.SSH.Host != "") && !(asset.Asset.Kind == "application" && asset.Asset.MetricsURL != "") {
			fail(w, 422, "주소를 입력한 뒤 연결하세요. 빈 값으로 등록한 초안은 보존됩니다")
			return
		}
		if asset.Asset.Kind == "server" {
			_, _, user := sshEndpoint(asset.Asset)
			if user == "" {
				fail(w, 422, "SSH username을 입력하세요")
				return
			}
			if asset.Asset.SSH.Fingerprint == "" && os.Getenv("CONTROL_KNOWN_HOSTS") == "" {
				fail(w, 422, "SSH 서버 지문을 확인해 저장한 뒤 연결하세요")
				return
			}
		}
		s.mu.Lock()
		delete(s.retry, asset.Asset.ID)
		s.mu.Unlock()
		updated, e := s.store.Enable(asset.Asset.ID, true)
		if e != nil {
			fail(w, 409, e.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		e = s.collect(ctx, updated.ID)
		if e != nil {
			fail(w, 422, e.Error())
			return
		}
		record, _ := s.store.Get(updated.ID)
		writeJSON(w, 200, publicAsset(record))
	})
	api.HandleFunc("POST /assets/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		asset, e := s.store.Enable(r.PathValue("id"), false)
		if e != nil {
			fail(w, 404, e.Error())
			return
		}
		writeJSON(w, 200, asset)
	})
	api.HandleFunc("POST /terminals", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			AssetID string `json:"assetId"`
		}
		if !decode(w, r, &input) {
			return
		}
		asset, e := s.store.Get(input.AssetID)
		if e != nil {
			fail(w, 404, e.Error())
			return
		}
		host, _, user := sshEndpoint(asset.Asset)
		if host == "" || user == "" {
			fail(w, 422, "터미널을 열려면 SSH 주소와 username을 입력하세요")
			return
		}
		if asset.Asset.SSH.Fingerprint == "" && os.Getenv("CONTROL_KNOWN_HOSTS") == "" {
			fail(w, 422, "서버 지문을 확인한 뒤 저장하세요")
			return
		}
		ticket := ID()
		s.mu.Lock()
		for key, v := range s.tickets {
			if time.Now().After(v.Expires) {
				delete(s.tickets, key)
			}
		}
		if len(s.tickets) >= 32 {
			s.mu.Unlock()
			fail(w, 429, "대기 중인 세션이 많습니다")
			return
		}
		s.tickets[ticket] = terminalTicket{AssetID: input.AssetID, Expires: time.Now().Add(30 * time.Second)}
		s.mu.Unlock()
		writeJSON(w, 201, map[string]string{"ticket": ticket, "path": "/terminal/ws"})
	})
	api.HandleFunc("GET /observations", func(w http.ResponseWriter, r *http.Request) {
		duration, e := strconv.Atoi(r.URL.Query().Get("range"))
		if e != nil || duration != 900 && duration != 3600 && duration != 21600 && duration != 86400 {
			fail(w, 400, "조회 기간을 확인하세요")
			return
		}
		assets, e := s.store.List()
		if e != nil {
			fail(w, 503, "등록 목록을 읽을 수 없습니다")
			return
		}
		ids := []string{}
		requested := r.URL.Query().Get("asset")
		for _, a := range assets {
			if requested == "" || requested == "all" || requested == a.ID {
				ids = append(ids, a.ID)
			}
		}
		if requested != "" && requested != "all" && len(ids) == 0 {
			fail(w, 404, "등록되지 않은 인프라입니다")
			return
		}
		end := time.Now().Unix()
		step := int64(max(15, duration/120))
		observations, e := s.store.Observations(ids, end-int64(duration), step)
		if e != nil {
			fail(w, 503, "관측 이력을 읽을 수 없습니다")
			return
		}
		evaluation, e := s.store.Observations(ids, end-3600, 15)
		if e != nil {
			fail(w, 503, "이벤트 평가 이력을 읽을 수 없습니다")
			return
		}
		writeJSON(w, 200, map[string]any{"assets": assets, "observations": observations, "evaluation": evaluation, "end": end, "step": step, "connected": true})
	})
	api.HandleFunc("GET /audit", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, s.store.Audit()) })
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.token) < 32 || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
			fail(w, 401, "인증이 필요합니다")
			return
		}
		api.ServeHTTP(w, r)
	}))
	return mux
}
func (s *Store) Enable(id string, enabled bool) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.allLocked()
	if e != nil {
		return Asset{}, e
	}
	record, ok := all[id]
	if !ok {
		return Asset{}, errors.New("등록된 인프라가 없습니다")
	}
	record.Asset.Enabled = enabled
	record.Asset.Version++
	record.Asset.UpdatedAt = nowString()
	if enabled {
		record.Asset.Status = "connecting"
		record.Asset.Message = "연결 확인 중"
	} else {
		record.Asset.Status = "paused"
		record.Asset.Message = "수집 일시정지"
	}
	b, e := s.seal(id, record)
	if e != nil {
		return Asset{}, e
	}
	_, e = s.db.Exec("UPDATE assets SET payload=? WHERE id=?", b, id)
	return publicAsset(record), e
}
func (s *Service) run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	cycles := 0
	for {
		all, e := s.store.All()
		if e == nil {
			for id, record := range all {
				if record.Asset.Enabled {
					go func(id string) {
						collectCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
						defer cancel()
						_ = s.collect(collectCtx, id)
					}(id)
				}
			}
		}
		cycles++
		if cycles%240 == 0 {
			s.store.Purge()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func main() {
	dir := os.Getenv("CONTROL_DATA_DIR")
	if dir == "" {
		dir = "./data"
	}
	token := os.Getenv("CONTROL_API_TOKEN")
	if len(token) < 32 {
		log.Fatal("CONTROL_API_TOKEN must have at least 32 characters")
	}
	store, e := openStore(dir)
	if e != nil {
		log.Fatal("Unable to open encrypted registry")
	}
	defer store.db.Close()
	service := newService(store, token, strings.Split(os.Getenv("CONTROL_ALLOWED_ORIGINS"), ","))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if os.Getenv("CONTROL_TEST_SEED") == "true" {
		if os.Getenv("MONITORING_MODE") != "test" {
			log.Fatal("Test registration is forbidden outside test mode")
		}
		if e = service.seed(ctx); e != nil {
			log.Printf("Test registration deferred: %v", e)
		}
	}
	go service.run(ctx)
	address := os.Getenv("CONTROL_LISTEN")
	if address == "" {
		address = "127.0.0.1:7080"
	}
	server := &http.Server{Addr: address, Handler: service.handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	go func() {
		<-ctx.Done()
		c, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = server.Shutdown(c)
	}()
	log.Printf("PULSE control service listening on %s", address)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
