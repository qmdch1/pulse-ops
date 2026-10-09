package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Operational records share the registry cipher. Event notes, delivery
// evidence, and saved views never contain endpoint credentials.
func (s *Store) putOperationLocked(kind, id string, at int64, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	b := s.aead.Seal(nonce, nonce, raw, []byte(kind+":"+id))
	_, err = s.db.Exec("INSERT INTO operations(kind,id,at,payload) VALUES(?,?,?,?) ON CONFLICT(kind,id) DO UPDATE SET at=excluded.at,payload=excluded.payload", kind, id, at, b)
	return err
}
func (s *Store) operationsLocked(kind string) (map[string]json.RawMessage, error) {
	rows, err := s.db.Query("SELECT id,payload FROM operations WHERE kind=? ORDER BY at DESC", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var id string
		var b []byte
		if err = rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		n := s.aead.NonceSize()
		if len(b) < n {
			return nil, errors.New("encrypted operation corrupt")
		}
		raw, e := s.aead.Open(nil, b[:n], b[n:], []byte(kind+":"+id))
		if e != nil {
			return nil, e
		}
		out[id] = raw
	}
	return out, rows.Err()
}
func operationList[T any](s *Store, kind string) ([]T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := s.operationsLocked(kind)
	if err != nil {
		return nil, err
	}
	items := []T{}
	for _, b := range raw {
		var item T
		if err = json.Unmarshal(b, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
func validateLabels(values []string, limit int) error {
	if len(values) > limit {
		return fmt.Errorf("항목은 최대 %d개입니다", limit)
	}
	seen := map[string]bool{}
	for _, v := range values {
		if strings.TrimSpace(v) != v || v == "" || len([]rune(v)) > 80 || strings.ContainsAny(v, "\x00\r\n") || seen[v] {
			return errors.New("빈 항목·중복·80자를 초과한 항목을 확인하세요")
		}
		seen[v] = true
	}
	return nil
}

type EventNote struct {
	Text string `json:"text"`
	At   string `json:"at"`
}
type EventRecord struct {
	ID         string            `json:"id"`
	Event      NotificationEvent `json:"event"`
	ResolvedAt string            `json:"resolvedAt"`
	Workflow   string            `json:"workflow"`
	Notes      []EventNote       `json:"notes"`
	Version    int               `json:"version"`
	UpdatedAt  string            `json:"updatedAt"`
}

// Called under the store lock before routing. Monitoring recovery and operator
// workflow are separate: acknowledging an incident never hides live evidence.
func (s *Store) journalLocked(asset Asset, current map[string]NotificationEvent, recoverable map[string]bool, at int64) error {
	raw, err := s.operationsLocked("event")
	if err != nil {
		return err
	}
	active := map[string]EventRecord{}
	for _, b := range raw {
		var v EventRecord
		if json.Unmarshal(b, &v) != nil {
			continue
		}
		if v.Event.AssetID == asset.ID && v.ResolvedAt == "" {
			active[v.Event.RuleID+":"+asset.ID] = v
		}
	}
	for key, event := range current {
		if _, ok := active[key]; ok {
			continue
		}
		event.Tags = asset.Tags
		record := EventRecord{ID: ID(), Event: event, Workflow: "open", Notes: []EventNote{}, Version: 1, UpdatedAt: event.At}
		if err = s.putOperationLocked("event", record.ID, at, record); err != nil {
			return err
		}
	}
	for key, v := range active {
		if _, ok := current[key]; ok || !recoverable[v.Event.RuleID] {
			continue
		}
		v.ResolvedAt = time.Unix(at, 0).UTC().Format(time.RFC3339)
		v.Event.Status = "resolved"
		v.UpdatedAt = v.ResolvedAt
		v.Version++
		if err = s.putOperationLocked("event", v.ID, at, v); err != nil {
			return err
		}
	}
	return nil
}

type MuteWindow struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	AssetIDs     []string `json:"assetIds"`
	Environments []string `json:"environments"`
	Tags         []string `json:"tags"`
	StartsAt     int64    `json:"startsAt"`
	EndsAt       int64    `json:"endsAt"`
	Version      int      `json:"version"`
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func matchesScope(ids, envs, tags []string, event NotificationEvent) bool {
	if len(ids) > 0 && !contains(ids, event.AssetID) || len(envs) > 0 && !contains(envs, event.Environment) {
		return false
	}
	for _, tag := range tags {
		if !contains(event.Tags, tag) {
			return false
		}
	}
	return true
}
func (s *Store) mutedLocked(event NotificationEvent, at int64) bool {
	raw, err := s.operationsLocked("mute")
	if err != nil {
		return true
	}
	for _, b := range raw {
		var m MuteWindow
		if json.Unmarshal(b, &m) == nil && m.StartsAt <= at && at < m.EndsAt && matchesScope(m.AssetIDs, m.Environments, m.Tags, event) {
			return true
		}
	}
	return false
}

type SavedView struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	AssetIDs       []string `json:"assetIds"`
	Range          int      `json:"range"`
	RefreshSeconds int      `json:"refreshSeconds"`
	Version        int      `json:"version"`
}
type DeliveryRecord struct {
	ID              string            `json:"id"`
	IntegrationID   string            `json:"integrationId"`
	IntegrationName string            `json:"integrationName"`
	Provider        string            `json:"provider"`
	Event           NotificationEvent `json:"event"`
	Status          string            `json:"status"`
	Attempts        int               `json:"attempts"`
	Created         int64             `json:"created"`
	At              int64             `json:"at"`
	Error           string            `json:"error"`
	Version         int               `json:"version"`
}

func (s *Store) historyLocked(i Integration, d notificationDelivery, status, message string, at int64) error {
	version := 1
	var encrypted []byte
	if err := s.db.QueryRow("SELECT payload FROM operations WHERE kind='delivery' AND id=?", d.ID).Scan(&encrypted); err == nil {
		n := s.aead.NonceSize()
		if len(encrypted) < n {
			return errors.New("encrypted delivery corrupt")
		}
		b, err := s.aead.Open(nil, encrypted[:n], encrypted[n:], []byte("delivery:"+d.ID))
		if err != nil {
			return err
		}
		var old DeliveryRecord
		if json.Unmarshal(b, &old) == nil {
			version = old.Version + 1
		}
	}
	return s.putOperationLocked("delivery", d.ID, at, DeliveryRecord{ID: d.ID, IntegrationID: i.ID, IntegrationName: i.Name, Provider: i.Provider, Event: d.Event, Status: status, Attempts: d.Attempts, Created: d.Created, At: at, Error: message, Version: version})
}
func validName(name string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 120 && !strings.ContainsAny(name, "\x00\r\n")
}
func (s *Service) installOperationsAPI(api *http.ServeMux) {
	api.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		items, err := operationList[EventRecord](s.store, "event")
		if err != nil {
			fail(w, 503, "이벤트 이력을 읽을 수 없습니다")
			return
		}
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
		if len(items) > 5000 {
			items = items[:5000]
		}
		writeJSON(w, 200, items)
	})
	api.HandleFunc("PUT /events/{id}", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Version  int    `json:"version"`
			Workflow string `json:"workflow"`
			Note     string `json:"note"`
		}
		if !decode(w, r, &input) {
			return
		}
		if !contains([]string{"open", "acknowledged", "in_progress", "resolved"}, input.Workflow) || len([]rune(input.Note)) > 2000 {
			fail(w, 400, "처리 상태와 2000자 이내 메모를 확인하세요")
			return
		}
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		raw, err := s.store.operationsLocked("event")
		var v EventRecord
		if err != nil || json.Unmarshal(raw[r.PathValue("id")], &v) != nil {
			fail(w, 404, "이벤트 이력이 없습니다")
			return
		}
		if v.Version != input.Version {
			fail(w, 409, "다른 변경이 저장되었습니다. 다시 불러오세요")
			return
		}
		if strings.TrimSpace(input.Note) != "" {
			if len(v.Notes) >= 100 {
				fail(w, 409, "메모는 이벤트당 최대 100개입니다")
				return
			}
			v.Notes = append(v.Notes, EventNote{Text: strings.TrimSpace(input.Note), At: nowString()})
		}
		v.Workflow = input.Workflow
		v.UpdatedAt = nowString()
		v.Version++
		if s.store.putOperationLocked("event", v.ID, time.Now().Unix(), v) != nil {
			fail(w, 503, "저장하지 못했습니다")
			return
		}
		s.store.Record(v.ID, "event.workflow", v.Workflow)
		writeJSON(w, 200, v)
	})
	api.HandleFunc("GET /deliveries", func(w http.ResponseWriter, r *http.Request) {
		items, err := operationList[DeliveryRecord](s.store, "delivery")
		if err != nil {
			fail(w, 503, "전송 이력을 읽을 수 없습니다")
			return
		}
		id, status, q := r.URL.Query().Get("integration"), r.URL.Query().Get("status"), strings.ToLower(r.URL.Query().Get("q"))
		out := []DeliveryRecord{}
		for _, v := range items {
			if id != "" && v.IntegrationID != id || status != "" && v.Status != status || q != "" && !strings.Contains(strings.ToLower(v.Event.Title+" "+v.Event.AssetName+" "+v.IntegrationName), q) {
				continue
			}
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At || out[i].At == out[j].At && out[i].ID < out[j].ID })
		if len(out) > 500 {
			out = out[:500]
		}
		writeJSON(w, 200, out)
	})
	api.HandleFunc("POST /deliveries/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Version int `json:"version"`
		}
		if !decode(w, r, &input) {
			return
		}
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		raw, err := s.store.operationsLocked("delivery")
		var v DeliveryRecord
		if err != nil || json.Unmarshal(raw[r.PathValue("id")], &v) != nil {
			fail(w, 404, "전송 기록이 없습니다")
			return
		}
		if v.Version != input.Version || v.Status != "failed" {
			fail(w, 409, "실패한 기록의 최신 버전만 재전송할 수 있습니다")
			return
		}
		all, err := s.store.integrationsLocked()
		i, ok := all[v.IntegrationID]
		if err != nil || !ok || !i.Integration.Enabled {
			fail(w, 409, "수신 연동을 활성화한 뒤 다시 시도하세요")
			return
		}
		if s.store.mutedLocked(v.Event, time.Now().Unix()) {
			fail(w, 409, "음소거·점검 시간에는 재전송할 수 없습니다")
			return
		}
		if !routeMatches(i.Integration, v.Event) {
			fail(w, 409, "현재 수신 대상 조건에 맞지 않습니다")
			return
		}
		if len(i.Queue) >= 1000 {
			fail(w, 409, "전송 대기열이 가득 찼습니다")
			return
		}
		// Keep the old evidence and use a new delivery ID for manual attempts.
		d := notificationDelivery{ID: ID(), Event: v.Event, Next: time.Now().Unix(), Created: time.Now().Unix()}
		i.Queue = append(i.Queue, d)
		if s.store.putIntegrationLocked(i) != nil {
			fail(w, 503, "재전송을 예약하지 못했습니다")
			return
		}
		_ = s.store.historyLocked(i.Integration, d, "queued", "", time.Now().Unix())
		v.Status = "requeued"
		v.Version++
		v.At = time.Now().Unix()
		_ = s.store.putOperationLocked("delivery", v.ID, v.At, v)
		s.store.Record(v.ID, "notification.manual_retry", "queued")
		writeJSON(w, 200, map[string]string{"message": "현재 수신처로 재전송을 예약했습니다"})
	})
	for _, kind := range []string{"mute", "view"} {
		kind := kind
		route := map[string]string{"mute": "mutes", "view": "views"}[kind]
		api.HandleFunc("GET /"+route, func(w http.ResponseWriter, r *http.Request) {
			if kind == "mute" {
				items, e := operationList[MuteWindow](s.store, kind)
				if e != nil {
					fail(w, 503, "점검 시간을 읽을 수 없습니다")
					return
				}
				sort.Slice(items, func(i, j int) bool { return items[i].StartsAt > items[j].StartsAt })
				writeJSON(w, 200, items)
			} else {
				items, e := operationList[SavedView](s.store, kind)
				if e != nil {
					fail(w, 503, "저장된 보기를 읽을 수 없습니다")
					return
				}
				sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
				writeJSON(w, 200, items)
			}
		})
		save := func(w http.ResponseWriter, r *http.Request) {
			var value any
			var id, name string
			var version int
			var ids []string
			if kind == "mute" {
				var v MuteWindow
				if !decode(w, r, &v) {
					return
				}
				if v.StartsAt < 0 || v.EndsAt <= v.StartsAt || v.EndsAt-v.StartsAt > 31*86400 || v.EndsAt <= time.Now().Unix() {
					fail(w, 400, "앞으로 종료하는 31일 이내 점검 시간을 입력하세요")
					return
				}
				if validateLabels(v.Environments, 20) != nil || validateLabels(v.Tags, 20) != nil {
					fail(w, 400, "환경·태그를 확인하세요")
					return
				}
				value = &v
				id, name, version, ids = v.ID, v.Name, v.Version, v.AssetIDs
			} else {
				var v SavedView
				if !decode(w, r, &v) {
					return
				}
				if !contains([]string{"900", "3600", "21600", "86400"}, strconv.Itoa(v.Range)) || !contains([]string{"0", "15", "30", "60"}, strconv.Itoa(v.RefreshSeconds)) {
					fail(w, 400, "조회 기간과 새로고침을 확인하세요")
					return
				}
				value = &v
				id, name, version, ids = v.ID, v.Name, v.Version, v.AssetIDs
			}
			pathID := r.PathValue("id")
			if id != "" && id != pathID || !validName(name) || validateLabels(ids, 200) != nil {
				fail(w, 400, "이름·선택 대상·ID를 확인하세요")
				return
			}
			id = pathID
			s.store.mu.Lock()
			defer s.store.mu.Unlock()
			raw, err := s.store.operationsLocked(kind)
			if err != nil {
				fail(w, 503, "설정을 읽을 수 없습니다")
				return
			}
			if id == "" {
				if len(raw) >= 100 {
					fail(w, 409, "최대 100개까지 저장할 수 있습니다")
					return
				}
				id = ID()
				version = 1
			} else {
				var old struct{ Version int }
				if json.Unmarshal(raw[id], &old) != nil {
					fail(w, 404, "설정이 없습니다")
					return
				}
				if old.Version != version {
					fail(w, 409, "다른 변경이 저장되었습니다. 다시 불러오세요")
					return
				}
				version++
			}
			all, err := s.store.allLocked()
			if err != nil {
				fail(w, 503, "등록 목록을 읽을 수 없습니다")
				return
			}
			for _, assetID := range ids {
				if _, ok := all[assetID]; !ok {
					fail(w, 400, "등록된 인프라를 선택하세요")
					return
				}
			}
			if v, ok := value.(*MuteWindow); ok {
				v.ID = id
				v.Version = version
				v.Name = strings.TrimSpace(name)
			} else {
				v := value.(*SavedView)
				v.ID = id
				v.Version = version
				v.Name = strings.TrimSpace(name)
			}
			if s.store.putOperationLocked(kind, id, time.Now().Unix(), value) != nil {
				fail(w, 503, "저장하지 못했습니다")
				return
			}
			s.store.Record(id, kind+".save", "ok")
			writeJSON(w, 200, value)
		}
		api.HandleFunc("POST /"+route, save)
		api.HandleFunc("PUT /"+route+"/{id}", save)
		api.HandleFunc("DELETE /"+route+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			s.store.mu.Lock()
			defer s.store.mu.Unlock()
			_, err := s.store.db.Exec("DELETE FROM operations WHERE kind=? AND id=?", kind, r.PathValue("id"))
			if err != nil {
				fail(w, 503, "삭제하지 못했습니다")
				return
			}
			s.store.Record(r.PathValue("id"), kind+".delete", "ok")
			writeJSON(w, 200, map[string]bool{"ok": true})
		})
	}
	api.HandleFunc("POST /assets/{id}/clone", func(w http.ResponseWriter, r *http.Request) {
		record, err := s.store.Get(r.PathValue("id"))
		if err != nil {
			fail(w, 404, "인프라가 없습니다")
			return
		}
		all, err := s.store.All()
		if err != nil || len(all) >= 200 {
			fail(w, 409, "등록 한도를 확인하세요")
			return
		}
		a := record.Asset
		a.ID = ""
		a.Name += " 복사"
		a.SSH.Fingerprint = ""
		// Copy configuration only. Credentials and host trust require explicit input.
		saved, err := s.store.Save(AssetInput{Asset: a})
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, saved)
	})
	api.HandleFunc("POST /assets/import", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Assets []AssetInput `json:"assets"`
		}
		if !decode(w, r, &input) {
			return
		}
		all, err := s.store.All()
		if err != nil || len(input.Assets) == 0 || len(input.Assets) > 100 || len(all)+len(input.Assets) > 200 {
			fail(w, 400, "한 번에 1–100개, 전체 200개 이내로 등록하세요")
			return
		}
		for _, a := range input.Assets {
			if a.ID != "" || validateAsset(a.Asset, all) != nil {
				fail(w, 400, "모든 행의 종류·주소·태그·연결 관계를 확인하세요")
				return
			}
		}
		results := []map[string]any{}
		for index, a := range input.Assets {
			saved, e := s.store.Save(a)
			item := map[string]any{"row": index + 1}
			if e != nil {
				item["error"] = e.Error()
			} else {
				item["asset"] = saved
			}
			results = append(results, item)
		}
		writeJSON(w, 200, results)
	})
	api.HandleFunc("POST /assets/bulk", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Action string `json:"action"`
			Items  []struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"items"`
			Environment *string   `json:"environment"`
			Tags        *[]string `json:"tags"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Items) == 0 || len(input.Items) > 100 || !contains([]string{"connect", "pause", "update"}, input.Action) {
			fail(w, 400, "일괄 작업과 1–100개 대상을 확인하세요")
			return
		}
		results := []map[string]any{}
		for _, item := range input.Items {
			result := map[string]any{"id": item.ID}
			a, err := s.store.Get(item.ID)
			if err == nil && a.Asset.Version != item.Version {
				err = errors.New("다른 변경이 저장되었습니다")
			}
			if err == nil {
				switch input.Action {
				case "pause":
					_, err = s.store.enableVersion(item.ID, false, item.Version)
					if err == nil {
						s.store.forgetAssetNotifications(item.ID)
					}
				case "connect":
					err = connectable(a.Asset)
					if err == nil {
						_, err = s.store.enableVersion(item.ID, true, item.Version)
						if err == nil {
							s.mu.Lock()
							delete(s.retry, item.ID)
							s.mu.Unlock()
							go func(id string) {
								ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
								defer cancel()
								_ = s.collect(ctx, id)
							}(item.ID)
						}
					}
				case "update":
					v := a.Asset
					if input.Environment != nil {
						v.Environment = *input.Environment
					}
					if input.Tags != nil {
						v.Tags = *input.Tags
					}
					_, err = s.store.Save(AssetInput{Asset: v})
					if err == nil {
						s.store.forgetAssetNotifications(item.ID)
					}
				}
			}
			if err != nil {
				result["error"] = err.Error()
			} else {
				result["status"] = "ok"
				s.store.Record(item.ID, "asset.bulk."+input.Action, "ok")
			}
			results = append(results, result)
		}
		writeJSON(w, 200, results)
	})
	api.HandleFunc("POST /assets/{id}/diagnose", s.diagnoseAsset)
}
func connectable(a Asset) error {
	if a.Address == "" && !(a.Kind == "server" && a.SSH.Host != "") && !(a.Kind == "application" && a.MetricsURL != "") {
		return errors.New("연결 주소를 입력하세요")
	}
	if a.Kind == "server" {
		_, _, user := sshEndpoint(a)
		if user == "" {
			return errors.New("SSH username을 입력하세요")
		}
		if a.SSH.Fingerprint == "" && os.Getenv("CONTROL_KNOWN_HOSTS") == "" {
			return errors.New("SSH 서버 지문을 저장하세요")
		}
	}
	return nil
}

type DiagnosticStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func (s *Service) diagnoseAsset(w http.ResponseWriter, r *http.Request) {
	record, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		fail(w, 404, "등록된 인프라가 없습니다")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	steps := []DiagnosticStep{}
	if err = connectable(record.Asset); err != nil {
		writeJSON(w, 200, []DiagnosticStep{{"연결 설정", "failed", err.Error()}})
		return
	}
	a := record.Asset
	host, port := a.Address, a.Port
	if a.Kind == "server" {
		host, port, _ = sshEndpoint(a)
	} else if a.Kind == "http" || a.Kind == "application" {
		raw := a.Address
		if a.MetricsURL != "" {
			raw = a.MetricsURL
		}
		u, _ := checkedURL(raw)
		host = u.Hostname()
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
		if u.Port() != "" {
			port, _ = strconv.Atoi(u.Port())
		}
	} else if port == 0 {
		port = map[string]int{"postgres": 5432, "mysql": 3306, "mariadb": 3306, "oracle": 1521, "redis": 6379}[a.Kind]
	}
	if a.SSH.JumpID != "" {
		steps = append(steps, DiagnosticStep{"네트워크 경로", "skipped", "SSH 경유 경로의 DNS·포트는 아래 프로토콜 진단에서 확인합니다"})
	} else {
		dnsCtx, c := context.WithTimeout(ctx, 4*time.Second)
		_, e := net.DefaultResolver.LookupHost(dnsCtx, host)
		c()
		if e != nil {
			writeJSON(w, 200, append(steps, DiagnosticStep{"DNS", "failed", "주소가 해석되지 않습니다. 호스트 이름·DNS·네트워크를 확인하세요"}))
			return
		}
		steps = append(steps, DiagnosticStep{"DNS", "ok", "주소 해석 성공"})
		tcpCtx, c := context.WithTimeout(ctx, 4*time.Second)
		conn, e := (&net.Dialer{}).DialContext(tcpCtx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		c()
		if e != nil {
			writeJSON(w, 200, append(steps, DiagnosticStep{"TCP 포트", "failed", "포트에 연결하지 못했습니다. 서비스 실행·방화벽·접근 경로를 확인하세요"}))
			return
		}
		conn.Close()
		steps = append(steps, DiagnosticStep{"TCP 포트", "ok", "포트 연결 성공"})
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		writeJSON(w, 200, append(steps, DiagnosticStep{"프로토콜", "failed", "진단 대기 시간이 초과했습니다"}))
		return
	}
	probe := newService(s.store, nil)
	var values map[string]float64
	switch a.Kind {
	case "server":
		values, _, err = probe.collectSSH(ctx, record)
	case "postgres":
		values, _, err = probe.collectPostgres(ctx, record)
	case "mysql", "mariadb":
		values, _, err = probe.collectMySQL(ctx, record)
	case "oracle":
		values, _, err = probe.collectOracle(ctx, record)
	case "redis":
		values, _, err = probe.collectRedis(ctx, record)
	case "http":
		values, err = probe.collectHTTP(ctx, record)
	case "application":
		values, _, err = probe.collectApplication(ctx, record)
	}
	if err != nil {
		steps = append(steps, DiagnosticStep{"프로토콜·인증·TLS", "failed", err.Error() + " · 계정·인증서·서버 지문·경유 서버를 확인하세요"})
	} else {
		steps = append(steps, DiagnosticStep{"프로토콜·인증·TLS", "ok", "실제 읽기 요청 성공"})
		if code, ok := values["http-status"]; ok && code >= 400 {
			hint := "서비스 URL·라우팅·업무 응답을 확인하세요"
			if code == 401 || code == 403 {
				hint = "HTTP 계정·인증 방식·접근 권한을 확인하세요"
			}
			steps = append(steps, DiagnosticStep{"HTTP 응답", "failed", fmt.Sprintf("HTTP %.0f · %s", code, hint)})
		}
		if v, ok := values["db-monitoring-ready"]; ok && v == 0 {
			steps = append(steps, DiagnosticStep{"통계 권한", "warning", "연결은 성공했지만 일부 통계가 없습니다. 모니터링 읽기 권한과 DB 버전을 확인하세요"})
		} else {
			steps = append(steps, DiagnosticStep{"지표 조회", "ok", fmt.Sprintf("%d개 지표 관측", len(values))})
		}
	}
	s.store.Record(a.ID, "asset.diagnose", "finished")
	writeJSON(w, 200, steps)
}
