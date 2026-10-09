package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedStaticAuthCacheAndCompression(t *testing.T) {
	s := newService(testStore(t), []string{"https://ops.example.invalid"})
	password := strings.Repeat("test", 8)
	handler := s.webHandler("production", "운영자", password, "")
	get := func(path, encoding, etag string, auth bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if auth {
			r.SetBasicAuth("운영자", password)
		}
		r.Header.Set("Accept-Encoding", encoding)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/", "/assets/app.js", "/api/monitoring", "/api/control/assets", "/api/connections"} {
		if w := get(path, "", "", false); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, w.Code)
		}
	}
	if w := get("/api/health", "", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), `"runtime":"go"`) || len(staticBuild) != 16 || !strings.Contains(w.Body.String(), `"build":"`+staticBuild+`"`) {
		t.Fatal("public health failed")
	}
	changed := map[string]staticAsset{}
	for name, file := range staticFiles {
		changed[name] = file
	}
	app := changed["app.js"]
	app.etag = "different"
	changed["app.js"] = app
	if uiBuild(staticFiles) != staticBuild || uiBuild(changed) == staticBuild {
		t.Fatal("UI build id must follow every embedded file")
	}
	raw := get("/assets/app.js", "", "", true)
	compressed := get("/assets/app.js", "gzip", "", true)
	if raw.Code != 200 || compressed.Code != 200 || compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("embedded script not served")
	}
	reader, e := gzip.NewReader(compressed.Body)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := io.ReadAll(reader)
	if e != nil || !bytes.Equal(decoded, raw.Body.Bytes()) {
		t.Fatal("compressed content differs")
	}
	if compressed.Header().Get("ETag") == raw.Header().Get("ETag") {
		t.Fatal("encoding variants share ETag")
	}
	if w := get("/assets/app.js", "gzip", compressed.Header().Get("ETag"), true); w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal("conditional request failed")
	}
	if w := get("/assets/app.js", "gzip;q=0", "", true); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("disabled compression accepted")
	}
	if raw.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(raw.Header().Get("Content-Security-Policy"), "worker-src 'self'") {
		t.Fatal("security policy missing")
	}
	for _, path := range []string{"/package.json", "/assets/../../service.go", "/assets/missing.js", "/api/admin"} {
		w := get(path, "", "", true)
		if w.Code == 200 {
			t.Fatalf("unexpected public file %s", path)
		}
	}
	r := httptest.NewRequest("HEAD", "/assets/style.css", nil)
	r.SetBasicAuth("운영자", password)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal("HEAD response invalid")
	}
	w = get("/", "", "", true)
	if !strings.Contains(w.Body.String(), "/assets/bootstrap.js") || strings.Contains(w.Body.String(), "_next/") {
		t.Fatal("root does not serve native UI")
	}
}

func TestUnifiedWriteOriginAndCacheInvalidation(t *testing.T) {
	s := newService(testStore(t), []string{"https://ops.example.invalid"})
	handler := s.webHandler("test", "", "", "")
	for _, tc := range []struct {
		origin, host, site string
		code               int
	}{{"", "ops.example.invalid", "", 403}, {"https://foreign.invalid", "ops.example.invalid", "", 403}, {"https://ops.example.invalid", "wrong.invalid", "", 403}, {"https://ops.example.invalid", "ops.example.invalid", "cross-site", 403}, {"https://ops.example.invalid", "ops.example.invalid", "same-origin", 201}} {
		s.snapshots.entries = map[string]cachedSnapshot{"fixture": {at: time.Now()}}
		r := httptest.NewRequest("POST", "https://ops.example.invalid/api/control/assets", strings.NewReader(`{"kind":"server"}`))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("origin %q host %q: %d %s", tc.origin, tc.host, w.Code, w.Body.String())
		}
		if tc.code == 201 && len(s.snapshots.entries) != 0 {
			t.Fatal("write did not invalidate snapshot")
		}
	}
}

func TestProductionConfigFailsClosed(t *testing.T) {
	s := newService(testStore(t), nil)
	for _, mode := range []string{"production", "unknown"} {
		h := s.webHandler(mode, "", "", "")
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 503 {
			t.Fatal("missing config allowed")
		}
	}
}

func TestMetricResultsNullZeroFreshnessAndScope(t *testing.T) {
	assets := []Asset{{ID: "a", Name: "A", Enabled: true}, {ID: "b", Name: "B", Enabled: false}}
	observations := []Observation{{AssetID: "a", Time: 900, Values: map[string]float64{"p99": 100, "errors": 0}}, {AssetID: "a", Time: 1000, Values: map[string]float64{"p99": 0, "memory": math.NaN()}}, {AssetID: "a", Time: 1100, Values: map[string]float64{"p99": 999}}, {AssetID: "b", Time: 1000, Values: map[string]float64{"p99": 500}}}
	results := metricResults(assets, observations, 1000, 15)
	byID := map[string]metricResult{}
	for _, r := range results {
		byID[r.ID] = r
	}
	if len(results) != 123 {
		t.Fatalf("catalog count %d", len(results))
	}
	p := byID["p99"]
	if p.Latest != nil || len(p.Series) != 2 || *p.Series[0].Points[1].Value != 0 || len(p.Series[0].Points) != 2 {
		t.Fatal("percentiles combined, zero lost, or future data included")
	}
	if byID["errors"].Latest != nil || byID["errors"].State != "stale" || byID["memory"].State != "missing" {
		t.Fatal("missing sample or NaN became current")
	}
	solo := metricResults(assets[:1], observations, 1000, 15)
	for _, r := range solo {
		if r.ID == "p99" && (r.Latest == nil || *r.Latest != 0 || len(r.Series) != 1) {
			t.Fatal("single asset scope incorrect")
		}
	}
	stale := metricResults(assets, observations, 2000, 15)
	for _, r := range stale {
		if r.ID == "p99" && (r.State != "stale" || r.Series[0].Points[len(r.Series[0].Points)-1].Value != nil) {
			t.Fatal("stale tail did not break")
		}
	}
}

func TestMonitoringValidationAndEmbeddedCatalog(t *testing.T) {
	s := newService(testStore(t), []string{"http://localhost:13000"})
	h := s.webHandler("test", "", "", "")
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "localhost:13000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/monitoring?range=1", "/api/monitoring?range=900&range=3600", "/api/monitoring?query=up", "/api/monitoring?url=http://example.invalid"} {
		if get(path).Code != 400 {
			t.Fatalf("invalid query allowed: %s", path)
		}
	}
	if get("/api/monitoring?instance=unknown").Code != 404 {
		t.Fatal("unknown asset did not fail")
	}
	w := get("/api/monitoring?range=900")
	var body snapshot
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.Connected || len(body.Metrics) != 123 || body.Assets == nil {
		t.Fatal("empty registry contract invalid")
	}
}

func TestTestModeRequiresIsolatedListener(t *testing.T) {
	for _, tc := range []struct {
		address   string
		seedBuild bool
		allowed   bool
	}{
		{"127.0.0.1:13000", false, true}, {"[::1]:7080", false, true}, {"localhost:7080", false, true},
		{"0.0.0.0:7080", false, false}, {":7080", false, false}, {"10.12.15.218:7080", false, false}, {"[::]:7080", false, false}, {"not-an-address", false, false},
		{"0.0.0.0:7080", true, true},
	} {
		if err := testModeAllowed(tc.address, tc.seedBuild); (err == nil) != tc.allowed {
			t.Fatalf("%q seed=%v: allowed=%v, err %v", tc.address, tc.seedBuild, tc.allowed, err)
		}
	}
}

func TestTestModeServesOnlyConfiguredHosts(t *testing.T) {
	s := newService(testStore(t), []string{"http://localhost:13000", "http://127.0.0.1:13000"})
	h := s.webHandler("test", "", "", "")
	for path, cases := range map[string]map[string]int{
		"/api/control/assets": {"localhost:13000": 200, "LOCALHOST:13000": 200, "127.0.0.1:13000": 200, "rebind.attacker.invalid": 403, "localhost:9999": 403},
		"/":                   {"localhost:13000": 200, "rebind.attacker.invalid": 403},
		"/api/health":         {"127.0.0.1:7080": 200},
	} {
		for host, code := range cases {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != code {
				t.Fatalf("GET %s Host %q: %d, want %d", path, host, w.Code, code)
			}
		}
	}
}
