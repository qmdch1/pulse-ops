package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("CONTROL_MASTER_KEY", "")
	s, e := openStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.db.Close() })
	return s
}
func TestEncryptedDraftAndVersion(t *testing.T) {
	store := testStore(t)
	secret := "not-a-real-secret-test-only"
	draft, e := store.Save(AssetInput{Asset: Asset{Kind: "server"}, SSHPassword: &secret})
	if e != nil {
		t.Fatal(e)
	}
	if draft.Enabled || draft.Status != "draft" || draft.Address != "" || !draft.HasSSHPassword {
		t.Fatal("blank draft must not connect")
	}
	var encrypted []byte
	if e = store.db.QueryRow("SELECT payload FROM assets WHERE id=?", draft.ID).Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted, []byte(secret)) || bytes.Contains(encrypted, []byte(draft.Name)) {
		t.Fatal("plaintext stored")
	}
	encoded, _ := json.Marshal(draft)
	if bytes.Contains(encoded, []byte(secret)) {
		t.Fatal("secret in public response")
	}
	saved, e := store.Save(AssetInput{Asset: draft})
	if e != nil {
		t.Fatal(e)
	}
	record, _ := store.Get(saved.ID)
	if record.Secrets.SSHPassword != secret {
		t.Fatal("missing secret must preserve value")
	}
	if _, e = store.Save(AssetInput{Asset: draft}); e == nil {
		t.Fatal("stale version accepted")
	}
	clear := ""
	_, e = store.Save(AssetInput{Asset: saved, SSHPassword: &clear})
	if e != nil {
		t.Fatal(e)
	}
	record, _ = store.Get(saved.ID)
	if record.Secrets.SSHPassword != "" {
		t.Fatal("explicit empty must clear secret")
	}
	encrypted[0] ^= 1
	if _, e = store.aead.Open(nil, encrypted[:store.aead.NonceSize()], encrypted[store.aead.NonceSize():], []byte(draft.ID)); e == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
func TestJumpValidationAndDeletion(t *testing.T) {
	s := testStore(t)
	a, e := s.Save(AssetInput{Asset: Asset{Name: "a", Kind: "server"}})
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Save(AssetInput{Asset: Asset{Name: "b", Kind: "server", SSH: SSHConfig{JumpID: a.ID}}})
	if e != nil {
		t.Fatal(e)
	}
	a.SSH.JumpID = b.ID
	if _, e = s.Save(AssetInput{Asset: a}); e == nil {
		t.Fatal("jump cycle accepted")
	}
	if e = s.Delete(a.ID); e == nil {
		t.Fatal("referenced jump deleted")
	}
}
func TestServiceAuthenticationAndBlankConnect(t *testing.T) {
	s := newService(testStore(t), []string{"http://localhost:13000"})
	asset, e := s.store.Save(AssetInput{Asset: Asset{Kind: "server"}})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(s.webHandler("production", "operator", strings.Repeat("p", 24), ""))
	defer server.Close()
	response, e := http.Get(server.URL + "/api/control/assets")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("missing auth allowed")
	}
	request, _ := http.NewRequest("POST", server.URL+"/api/control/assets/"+asset.ID+"/connect", nil)
	request.SetBasicAuth("operator", strings.Repeat("p", 24))
	request.Header.Set("Origin", "http://localhost:13000")
	request.Host = "localhost:13000"
	response, e = http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 422 {
		t.Fatal("blank connect should fail")
	}
	record, _ := s.store.Get(asset.ID)
	if record.Asset.Enabled {
		t.Fatal("blank draft activated")
	}
}
func TestHistogramAndWindow(t *testing.T) {
	previous := previousSample{At: time.Now().Add(-5 * time.Minute), Values: map[string]float64{"h:count": 0, "h:bucket:0.1": 0, "h:bucket:1": 0, "h:bucket:+Inf": 0}}
	raw := map[string]float64{"h:count": 100, "h:bucket:0.1": 90, "h:bucket:1": 100, "h:bucket:+Inf": 100}
	value, ok := histogramQuantile(raw, previous, "h", .99)
	if !ok || value < .90 || value > .92 {
		t.Fatalf("P99 interpolation invalid: %v %v", value, ok)
	}
	raw["h:count"] = -1
	if _, ok = histogramQuantile(raw, previous, "h", .99); ok {
		t.Fatal("counter reset accepted")
	}
	s := newService(testStore(t), nil)
	s.history["x"] = []previousSample{{At: time.Now().Add(-time.Minute)}}
	if !s.windowSample("x", 5*time.Minute).At.IsZero() {
		t.Fatal("incomplete window accepted")
	}
}
func TestCollectionDoesNotFollowHTTPRedirects(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer server.Close()
	s := newService(testStore(t), nil)
	values, e := s.collectHTTP(context.Background(), StoredAsset{Asset: Asset{Address: server.URL, Kind: "http"}})
	if e != nil {
		t.Fatal(e)
	}
	if hit || values["http-status"] != 302 {
		t.Fatal("unexpected redirect behavior")
	}
}
func TestMasterKeyReopensRegistry(t *testing.T) {
	t.Setenv("CONTROL_MASTER_KEY", "")
	dir := t.TempDir()
	s, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	asset, e := s.Save(AssetInput{Asset: Asset{Kind: "postgres", Name: "durable"}})
	if e != nil {
		t.Fatal(e)
	}
	s.db.Close()
	s, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.Close()
	if _, e = s.Get(asset.ID); e != nil {
		t.Fatal(e)
	}
	info, e := os.Stat(filepath.Join(dir, "master.key"))
	if e != nil {
		t.Fatal(e)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("master key permissions")
	}
}

// Runs only against the isolated test Compose network; never targets discovered SSH hosts.
func TestIntegrationRegisteredTargetsAndTerminal(t *testing.T) {
	base := os.Getenv("PULSE_TEST_CONTROL")
	if base == "" {
		t.Skip("isolated Compose integration only")
	}
	call := func(method, path string, body any, status int) []byte {
		t.Helper()
		var reader io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			reader = bytes.NewReader(b)
		}
		r, _ := http.NewRequest(method, base+"/api/control"+path, reader)
		r.Header.Set("Origin", "http://localhost:13000")
		r.Host = "localhost:13000"
		r.Header.Set("Content-Type", "application/json")
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("%s %s returned %d: %s", method, path, response.StatusCode, data)
		}
		return data
	}
	var registry struct {
		Assets []Asset `json:"assets"`
	}
	json.Unmarshal(call("GET", "/assets", nil, 200), &registry)
	if len(registry.Assets) < 8 {
		t.Fatalf("expected 8 registered targets, got %d", len(registry.Assets))
	}
	for _, a := range registry.Assets {
		if a.Name == "SSH bastion · test" || a.Name == "SSH node · test" {
			call("POST", "/assets/"+a.ID+"/connect", nil, 200)
			var ticket struct {
				Ticket string `json:"ticket"`
			}
			json.Unmarshal(call("POST", "/terminals", map[string]string{"assetId": a.ID}, 201), &ticket)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			headers := http.Header{"Origin": []string{"http://localhost:13000"}}
			ws, _, e := websocket.Dial(ctx, strings.Replace(base, "http://", "ws://", 1)+"/terminal/ws?ticket="+ticket.Ticket, &websocket.DialOptions{HTTPHeader: headers})
			if e != nil {
				t.Fatal(e)
			}
			ready := false
			for !ready {
				kind, data, e := ws.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if kind == websocket.MessageText {
					var state map[string]string
					json.Unmarshal(data, &state)
					if state["type"] == "error" {
						t.Fatal(state["message"])
					}
					ready = state["type"] == "connected"
				}
			}
			ws.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
			ws.Write(ctx, websocket.MessageText, []byte(`{"type":"input","data":"printf 'PULSE_%s_VERIFIED\\n' 'TERMINAL'\n"}`))
			found := false
			combined := ""
			for !found {
				kind, data, e := ws.Read(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if kind == websocket.MessageBinary {
					combined += string(data)
					found = strings.Contains(combined, "PULSE_TERMINAL_VERIFIED")
				}
			}
			ws.Write(ctx, websocket.MessageText, []byte(`{"type":"disconnect"}`))
			ws.Close(websocket.StatusNormalClosure, "test complete")
			if retry, _, err := websocket.Dial(ctx, strings.Replace(base, "http://", "ws://", 1)+"/terminal/ws?ticket="+ticket.Ticket, &websocket.DialOptions{HTTPHeader: headers}); err == nil {
				retry.CloseNow()
				t.Fatal("ticket reused")
			}
		}
	}
	draft := call("POST", "/assets", map[string]string{"kind": "server", "name": "integration blank draft"}, 201)
	var a Asset
	json.Unmarshal(draft, &a)
	if a.Enabled || a.Status != "draft" {
		t.Fatal("draft connected")
	}
	call("POST", "/assets/"+a.ID+"/connect", nil, 422)
	call("DELETE", "/assets/"+a.ID, nil, 200)
	bad := call("POST", "/assets", map[string]any{"kind": "server", "name": "integration bad fingerprint", "address": "test-bastion", "sshPassword": "pulse_isolated_test_only", "ssh": map[string]string{"username": "pulse", "fingerprint": "SHA256:not-the-test-host"}}, 201)
	json.Unmarshal(bad, &a)
	call("POST", "/assets/"+a.ID+"/connect", nil, 422)
	call("DELETE", "/assets/"+a.ID, nil, 200)
	data := call("GET", "/observations?range=900&asset=all", nil, 200)
	for _, id := range []string{"db-probe", "redis-probe", "node-cpu", "memory-host", "probe-latency"} {
		if !bytes.Contains(data, []byte(`"`+id+`"`)) {
			t.Fatalf("missing real observation %s", id)
		}
	}
}
