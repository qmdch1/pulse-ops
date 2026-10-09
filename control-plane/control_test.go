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
func TestBusiestProcessUsesTickDeltasOfTheSameProcess(t *testing.T) {
	start := time.Unix(1000, 0)
	_, ok, first := busiestProcess("100\n1 500 10\n42 1000 77\n43 50 80\n", processSample{}, start)
	if ok || len(first.Ticks) != 3 {
		t.Fatalf("first sample must only seed the baseline: %v %d", ok, len(first.Ticks))
	}
	// 15 s later: PID 42 used 3 s of CPU (20%), PID 43 restarted with a new start
	// time and huge ticks, PID 44 is new, and PID 1 is unchanged.
	value, ok, second := busiestProcess("100\n1 500 10\n42 1300 77\n43 9000 999\n44 7000 1200\n", first, start.Add(15*time.Second))
	if !ok || value < 19.99 || value > 20.01 {
		t.Fatalf("busiest process %v %v", value, ok)
	}
	// Multithreaded work above one core is kept, ticks per second come from the host.
	value, ok, _ = busiestProcess("250\n42 1300 77\n", processSample{At: start, Ticks: map[string]float64{"42:77": 300}}, start.Add(2*time.Second))
	if !ok || value != 200 {
		t.Fatalf("multi-core process %v %v", value, ok)
	}
	if _, ok, _ = busiestProcess("100\n42 100 77\n", second, start.Add(30*time.Second)); ok {
		t.Fatal("a counter that went backwards was reported")
	}
	if _, ok, _ = busiestProcess("100\n42 1400 77\n", first, start.Add(2*time.Hour)); ok {
		t.Fatal("a stale baseline was reported")
	}
	if _, ok, _ = busiestProcess("", first, start.Add(15*time.Second)); ok {
		t.Fatal("empty process output was reported")
	}
}
func TestParseDiskCapacity(t *testing.T) {
	const gib = 1024 * 1024
	for name, sample := range map[string]struct {
		line                       string
		percent, total, used, free float64
	}{
		"linux": {"/dev/sda1         103081248  41232500  56588112      43% /", 43, 103081248.0 / gib, 41232500.0 / gib, 56588112.0 / gib},
		"macos": {"/dev/disk3s1s1    482797652  10508552 284262880      4%    /", 4, 482797652.0 / gib, 10508552.0 / gib, 284262880.0 / gib},
	} {
		values := map[string]float64{}
		parseDisk(sample.line, values)
		if values["disk"] != sample.percent || values["disk-total"] != sample.total || values["disk-used"] != sample.used || values["disk-free"] != sample.free {
			t.Fatalf("%s disk values %v", name, values)
		}
	}
	values := map[string]float64{}
	parseDisk("Filesystem 1024-blocks Used Available Capacity Mounted on", values)
	if len(values) != 0 {
		t.Fatal("df header parsed as a sample")
	}
}
func TestContainerShareUsesCgroupAllocation(t *testing.T) {
	const totalKB = 16 * 1024 * 1024 // 16 GiB VM
	previous := previousSample{At: time.Now().Add(-10 * time.Second), Values: map[string]float64{"cgroup_cpu_usec": 1_000_000}}
	near := func(a, b float64) bool { return a > b-0.01 && a < b+0.01 }
	// cgroup v2 container: 64 MiB and half a core. 1 s of CPU in 10 s is 0.1 core = 20 % of 0.5.
	raw := map[string]float64{}
	own, ok := containerShare(map[string][]string{"mem.max": {"67108864"}, "mem.current": {"12582912"}, "mem.inactive": {"2097152"}, "session.anon_kb": {"2048"}, "cpu.max": {"50000", "100000"}, "cpuset": {"0-11"}, "cpu.usage_usec": {"2000000"}, "container": {"1"}, "nproc": {"12"}}, raw, previous, totalKB)
	if !ok || !near(own["memory-host"], 12.5) || own["memory-limit"] != 64 || own["cpu-cores"] != 0.5 || !near(own["node-cpu"], 20) {
		t.Fatalf("v2 limited container %v %v", own, ok)
	}
	// No limits: RAM is measured against the VM and CPU against every allowed core.
	own, ok = containerShare(map[string][]string{"mem.max": {"max"}, "mem.current": {"1073741824"}, "cpu.max": {"max", "100000"}, "cpuset": {"0-3,6"}, "cpu.usage_usec": {"6000000"}, "container": {"1"}}, map[string]float64{}, previous, totalKB)
	if !ok || !near(own["memory-host"], 6.25) || own["memory-limit"] != 16384 || own["cpu-cores"] != 5 || !near(own["node-cpu"], 10) {
		t.Fatalf("unlimited container %v %v", own, ok)
	}
	// cgroup v1: the quota caps the usable cores and cpuacct counts nanoseconds.
	own, ok = containerShare(map[string][]string{"v1.mem.limit": {"134217728"}, "mem.current": {"67108864"}, "mem.inactive": {"0"}, "v1.cpu.quota": {"200000"}, "v1.cpu.period": {"100000"}, "v1.cpu.usage": {"3000000000"}, "nproc": {"4"}}, map[string]float64{}, previous, totalKB)
	if !ok || own["memory-host"] != 50 || own["memory-limit"] != 128 || own["cpu-cores"] != 2 || !near(own["node-cpu"], 10) {
		t.Fatalf("v1 container %v %v", own, ok)
	}
	// A real host (v2 root without limit files, or v1 with the unlimited sentinel) keeps /proc values.
	for _, host := range []map[string][]string{{"cpuset": {"0-11"}, "cpu.usage_usec": {"9"}, "nproc": {"12"}}, {"v1.mem.limit": {"9223372036854771712"}, "v1.cpu.quota": {"-1"}, "nproc": {"8"}}} {
		if _, ok := containerShare(host, map[string]float64{}, previous, totalKB); ok {
			t.Fatalf("host treated as a container: %v", host)
		}
	}
	for set, want := range map[string]float64{"0-11": 12, "0-3,6": 5, "": 0, "7": 1} {
		if got := cpusetCount(set); got != want {
			t.Fatalf("cpuset %q = %v", set, got)
		}
	}
}
func TestDockerHostGroupsContainersOfOneKernel(t *testing.T) {
	if dockerHostID("4f0c1a2b-9d8e-4c7f-a1b2-c3d4e5f60718") != "dockerhost-4f0c1a2b9d8e" || dockerHostID("not-a-boot-id") != "" || dockerHostID("") != "" {
		t.Fatal("host id must come from a hex boot id only")
	}
	s := newService(testStore(t), nil)
	boot := "4f0c1a2b-9d8e-4c7f-a1b2-c3d4e5f60718"
	s.observeHost("node", boot, map[string]float64{"node-cpu": 3, "disk-used": 20})
	s.observeHost("bastion", boot, map[string]float64{"node-cpu": 3, "disk-used": 20})
	s.observeHost("deleted", boot, map[string]float64{"node-cpu": 3})
	observations, err := s.store.Observations([]string{"dockerhost-4f0c1a2b9d8e"}, 0, 15)
	if err != nil || len(observations) != 1 || observations[0].Values["disk-used"] != 20 {
		t.Fatalf("host values must be stored once per interval: %v %v", observations, err)
	}
	hosts := s.dockerHosts([]Asset{{ID: "bastion", Name: "bastion", Enabled: true, Status: "connected", LastSeen: "2026-10-09T08:00:00Z"}, {ID: "node", Name: "node", Enabled: true, Status: "paused", LastSeen: "2026-10-09T08:00:15Z"}})
	if len(hosts) != 1 || !hosts[0].Virtual || hosts[0].Kind != "server" || hosts[0].Status != "connected" || hosts[0].LastSeen != "2026-10-09T08:00:15Z" || strings.Join(hosts[0].Members, ",") != "bastion,node" {
		t.Fatalf("virtual host %+v", hosts)
	}
	if len(s.dockerHosts(nil)) != 0 {
		t.Fatal("a host without registered containers must not appear")
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
