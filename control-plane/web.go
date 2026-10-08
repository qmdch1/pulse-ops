package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
)

// UI source is shipped in the binary; no runtime file server or Node toolchain.
//
//go:embed web
var webFiles embed.FS

type staticAsset struct {
	body, compressed  []byte
	contentType, etag string
}

var staticOnce sync.Once
var staticFiles map[string]staticAsset

func prepareStatic() {
	staticFiles = map[string]staticAsset{}
	_ = fs.WalkDir(webFiles, "web", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, e := webFiles.ReadFile(name)
		if e != nil {
			return e
		}
		var zipped bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&zipped, gzip.BestCompression)
		_, _ = writer.Write(data)
		_ = writer.Close()
		sum := sha256.Sum256(data)
		typ := mime.TypeByExtension(path.Ext(name))
		if typ == "" {
			typ = "application/octet-stream"
		}
		if strings.HasSuffix(name, ".mjs") {
			typ = "text/javascript; charset=utf-8"
		}
		staticFiles[strings.TrimPrefix(name, "web/")] = staticAsset{data, zipped.Bytes(), typ, hex.EncodeToString(sum[:16])}
		return nil
	})
}
func acceptsGzip(value string) bool {
	for _, part := range strings.Split(value, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if fields[0] != "gzip" {
			continue
		}
		ok := true
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if f == "q=0" || f == "q=0.0" || f == "q=0.00" || f == "q=0.000" {
				ok = false
			}
		}
		return ok
	}
	return false
}
func serveStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if r.URL.Path == "/" {
		name = "index.html"
	}
	file, ok := staticFiles[name]
	if !ok || strings.HasSuffix(name, "LICENSE") || strings.HasSuffix(name, "manifest.json") {
		http.NotFound(w, r)
		return
	}
	data, etag := file.body, `"`+file.etag+`"`
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("Content-Type", file.contentType)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		data = file.compressed
		etag = `"` + file.etag + `-gzip"`
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, name, buildTime, bytes.NewReader(data))
}

type gzipResponse struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func (w gzipResponse) Write(b []byte) (int, error) { return w.writer.Write(b) }
func jsonCompressed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) || r.Method == "HEAD" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		zw, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer zw.Close()
		next.ServeHTTP(gzipResponse{w, zw}, r)
	})
}
func (s *Service) webHandler(mode, user, password, inventory string) http.Handler {
	staticOnce.Do(prepareStatic)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serveStatic)
	mux.HandleFunc("GET /assets/", serveStatic)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "mode": mode, "runtime": "go", "frontend": "native-es-modules"})
	})
	mux.Handle("/api/control/", http.StripPrefix("/api/control", s.apiHandler()))
	mux.HandleFunc("GET /api/monitoring", func(w http.ResponseWriter, r *http.Request) { s.monitoring(w, r, mode) })
	mux.HandleFunc("GET /api/connections", func(w http.ResponseWriter, r *http.Request) { serveInventory(w, inventory, mode) })
	installBrowserTests(mux, mode)
	api := jsonCompressed(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		if r.URL.Path == "/terminal/ws" {
			s.terminal(w, r)
			return
		} // One-time, expiring ticket + exact Origin.
		if r.URL.Path == "/api/health" && r.Method == "GET" {
			mux.ServeHTTP(w, r)
			return
		}
		// Test mode has no operator login. Serve only the configured origin hosts so
		// a DNS-rebinding page cannot read the unauthenticated API.
		if mode == "test" && !s.allowedHost(r.Host) {
			fail(w, 403, "허용된 주소로 접속하세요")
			return
		}
		if mode != "test" {
			if mode != "production" || user == "" || len(password) < 24 {
				fail(w, 503, "운영 접근 계정을 설정하세요")
				return
			}
			u, p, ok := r.BasicAuth()
			actual := sha256.Sum256([]byte(u + "\x00" + p))
			expected := sha256.Sum256([]byte(user + "\x00" + password))
			if !ok || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="PULSE OPS", charset="UTF-8"`)
				fail(w, 401, "인증이 필요합니다")
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			u, e := url.Parse(origin)
			if e != nil || !s.origins[origin] || u.Host != r.Host || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "현재 대시보드에서 요청하세요")
				return
			}
			if r.URL.RawQuery != "" {
				fail(w, 400, "지원하지 않는 요청입니다")
				return
			}
			s.invalidateSnapshot()
			defer s.invalidateSnapshot()
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})
}
func serveInventory(w http.ResponseWriter, file, mode string) {
	hosts := []map[string]string{}
	if file != "" {
		if f, e := os.Open(file); e == nil {
			defer f.Close()
			var data struct {
				Hosts []map[string]any `json:"hosts"`
			}
			b, e := io.ReadAll(io.LimitReader(f, 100001))
			if e == nil && len(b) <= 100000 && json.Unmarshal(b, &data) == nil {
				for _, h := range data.Hosts {
					if len(hosts) >= 100 {
						break
					}
					entry := map[string]string{"status": "not_connected"}
					for k, limit := range map[string]int{"alias": 100, "hostname": 200, "user": 80, "port": 5, "proxyJump": 200, "platform": 30} {
						if v, ok := h[k].(string); ok {
							entry[k] = string([]rune(v)[:min(len([]rune(v)), limit)])
						}
					}
					hosts = append(hosts, entry)
				}
			}
		}
	}
	writeJSON(w, 200, map[string]any{"hosts": hosts, "mode": mode, "configured": true, "notice": "SSH 설정의 연결 후보입니다. 자동 접속하거나 개인 키를 읽지 않습니다."})
}
