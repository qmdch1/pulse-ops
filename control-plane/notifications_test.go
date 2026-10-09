package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func integrationForTest(t *testing.T, store *Store, endpoint string) Integration {
	t.Helper()
	i, err := store.SaveIntegration(IntegrationInput{Name: "운영 알림", Provider: "webhook", URL: &endpoint, Enabled: true, Severities: []string{"critical", "warning"}, Recovery: true})
	if err != nil {
		t.Fatal(err)
	}
	return i
}
func integrationRecord(t *testing.T, store *Store, id string) storedIntegration {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	all, err := store.integrationsLocked()
	if err != nil {
		t.Fatal(err)
	}
	return all[id]
}
func TestIntegrationAPIEncryptionAndUpdates(t *testing.T) {
	store := testStore(t)
	s := newService(store, []string{"http://localhost:13000"})
	handler := s.webHandler("test", "", "", "")
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "http://localhost:13000/api/control"+path, bytes.NewReader(raw))
		req.Header.Set("Origin", "http://localhost:13000")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	url, token := "https://receiver.example/events?secret=credential-in-url", "credential-in-header"
	raw := call("POST", "/integrations", IntegrationInput{Name: "테스트", Provider: "webhook", URL: &url, Token: &token, Severities: []string{"warning"}}, 201)
	var item Integration
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if !item.HasURL || !item.HasToken || bytes.Contains(raw, []byte("credential-in")) {
		t.Fatal("public response exposed credentials")
	}
	var encrypted []byte
	if err := store.db.QueryRow("SELECT payload FROM integrations WHERE id=?", item.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(url)) || bytes.Contains(encrypted, []byte(token)) {
		t.Fatal("integration secrets stored in plaintext")
	}
	call("PUT", "/integrations/"+item.ID, IntegrationInput{Name: "変更", Provider: "webhook", Version: item.Version, Severities: []string{"warning"}}, 200)
	record := integrationRecord(t, store, item.ID)
	if record.URL != url || record.Token != token {
		t.Fatal("omitted secrets were erased")
	}
	call("PUT", "/integrations/"+item.ID, IntegrationInput{Name: "stale", Provider: "webhook", Version: item.Version, Severities: []string{"warning"}}, 409)
	list := call("GET", "/integrations", nil, 200)
	if bytes.Contains(list, []byte("credential-in")) {
		t.Fatal("list leaked a secret")
	}
	clear := ""
	item.Version++
	call("PUT", "/integrations/"+item.ID, IntegrationInput{Name: "clear", Provider: "webhook", Version: item.Version, Token: &clear, Severities: []string{"warning"}}, 200)
	if integrationRecord(t, store, item.ID).Token != "" {
		t.Fatal("explicit token clearing ignored")
	}
	req := httptest.NewRequest("POST", "http://localhost:13000/api/control/integrations", bytes.NewReader(raw))
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	call("DELETE", "/integrations/"+item.ID, nil, 200)
	call("POST", "/integrations/"+item.ID+"/test", nil, 404)
}
func TestIntegrationValidation(t *testing.T) {
	for _, c := range []struct{ provider, url, token string }{
		{"webhook", "file:///tmp/secret", ""}, {"webhook", "https://user:password@host/path", ""}, {"webhook", "https://host/path#fragment", ""},
		{"slack", "http://hooks.slack.com/services/a/b/c", ""}, {"slack", "https://hooks.slack.com.evil.example/services/a/b/c", ""},
		{"discord", "https://example.com/api/webhooks/id/token", ""}, {"webhook", "https://example.com", "token\r\nX: injected"},
		{"teams", "https://example.com", "secret"}, {"unknown", "https://example.com", ""},
	} {
		r := storedIntegration{Integration: Integration{Name: "channel", Provider: c.provider, Severities: []string{"critical"}}, URL: c.url, Token: c.token}
		if err := validateIntegration(r); err == nil {
			t.Fatalf("accepted invalid integration %s", c.provider)
		}
	}
}

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMessengerPayloadsAndSafeErrors(t *testing.T) {
	endpoints := map[string]string{"slack": "https://hooks.slack.com/services/test/test/placeholder", "discord": "https://discord.com/api/webhooks/test/placeholder?thread_id=123", "teams": "https://workflow.example/trigger", "webhook": "https://receiver.example/events"}
	for provider, endpoint := range endpoints {
		t.Run(provider, func(t *testing.T) {
			record := storedIntegration{Integration: Integration{Name: "channel", Provider: provider, Severities: []string{"critical"}}, URL: endpoint}
			if provider == "webhook" {
				record.Token = "test-token"
			}
			client := &http.Client{Transport: notificationTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("invalid request")
				}
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				key := map[string]string{"slack": "blocks", "discord": "content", "teams": "attachments", "webhook": "event"}[provider]
				if len(payload[key]) == 0 {
					t.Fatal("missing provider payload")
				}
				if provider == "discord" && (r.URL.Query().Get("wait") != "true" || r.URL.Query().Get("thread_id") != "123" || string(payload["allowed_mentions"]) != `{"parse":[]}`) {
					t.Fatal("Discord delivery confirmation/mention protection missing")
				}
				if provider == "webhook" && r.Header.Get("Authorization") != "Bearer test-token" {
					t.Fatal("missing bearer token")
				}
				return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			})}
			event := NotificationEvent{Title: "<@everyone> 서버 오류율 증가", Severity: "critical", Status: "firing", AssetName: "API", At: nowString()}
			if err := sendNotification(context.Background(), client, record, event); err != nil {
				t.Fatal(err)
			}
			client.Transport = notificationTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("request failed: " + endpoint) })
			if err := sendNotification(context.Background(), client, record, event); err == nil || strings.Contains(err.Error(), endpoint) {
				t.Fatal("request error exposed URL or reported success")
			}
		})
	}
}
func TestNotificationRedirectFailureAndDeadline(t *testing.T) {
	var forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	record := storedIntegration{Integration: Integration{Name: "channel", Provider: "webhook", Severities: []string{"critical"}}, URL: server.URL, Token: "secret"}
	err := sendNotification(context.Background(), server.Client(), record, NotificationEvent{})
	if err == nil || forwarded.Load() != 0 || !strings.Contains(err.Error(), "307") {
		t.Fatal("redirect followed or hidden")
	}
	failure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429); _, _ = w.Write([]byte("secret")) }))
	defer failure.Close()
	record.URL = failure.URL
	err = sendNotification(context.Background(), failure.Client(), record, NotificationEvent{})
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "429") {
		t.Fatal("unsafe HTTP failure")
	}
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	record.URL = slow.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if sendNotification(ctx, slow.Client(), record, NotificationEvent{}) == nil {
		t.Fatal("ignored deadline")
	}
}
func TestNotificationTransitionsPersistAndRetry(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	i := integrationForTest(t, store, "https://receiver.example/events")
	asset := Asset{ID: "api", Name: "API", Enabled: true, Status: "connected"}
	event := NotificationEvent{ID: "errors:api", RuleID: "errors", AssetID: "api", Severity: "critical", Status: "firing", At: nowString(), Values: map[string]float64{"errors": 3}}
	active := map[string]NotificationEvent{event.ID: event}
	for n := 0; n < 2; n++ {
		if err = store.queueNotifications(asset, active, nil, 100); err != nil {
			t.Fatal(err)
		}
	}
	if len(integrationRecord(t, store, i.ID).Queue) != 1 {
		t.Fatal("duplicate active notification")
	}
	store.db.Close()
	store, err = openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	if err = store.queueNotifications(asset, active, nil, 100); err != nil {
		t.Fatal(err)
	}
	if len(integrationRecord(t, store, i.ID).Queue) != 1 {
		t.Fatal("restart duplicated notification")
	}
	store.queueNotifications(asset, nil, map[string]bool{"errors": false}, 110)
	if len(integrationRecord(t, store, i.ID).Active) != 1 {
		t.Fatal("missing evidence was treated as recovery")
	}
	store.queueNotifications(asset, nil, map[string]bool{"errors": true}, 120, map[string]float64{"errors": 0.5})
	r := integrationRecord(t, store, i.ID)
	if len(r.Queue) != 2 || r.Queue[1].Event.Status != "resolved" {
		t.Fatal("recovery not queued")
	}
	if r.Queue[1].Event.Values["errors"] != 0.5 {
		t.Fatal("recovery reused triggering values")
	}
	id := r.Queue[0].ID
	store.finishDelivery(i, id, errors.New("HTTP 429"), 121)
	r = integrationRecord(t, store, i.ID)
	if r.Queue[0].Next != 151 || r.Integration.LastStatus != "retrying" {
		t.Fatal("retry backoff incorrect")
	}
	for n := 0; n < 4; n++ {
		store.finishDelivery(i, id, errors.New("HTTP 429"), 152+int64(n))
	}
	r = integrationRecord(t, store, i.ID)
	if len(r.Queue) != 1 || r.Integration.LastStatus != "failed" {
		t.Fatal("retry budget not enforced")
	}
	store.forgetAssetNotifications("api")
	if len(integrationRecord(t, store, i.ID).Queue) != 0 {
		t.Fatal("paused target has queued notifications")
	}
}
func TestNotificationsCollectAndDeliverWithoutBrowser(t *testing.T) {
	store := testStore(t)
	s := newService(store, nil)
	var received []NotificationEvent
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Event NotificationEvent `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		received = append(received, payload.Event)
		w.WriteHeader(204)
	}))
	defer sink.Close()
	i := integrationForTest(t, store, sink.URL)
	var healthy atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
		}
	}))
	defer target.Close()
	a, err := store.Save(AssetInput{Asset: Asset{Kind: "http", Name: "API", Address: target.URL}})
	if err != nil {
		t.Fatal(err)
	}
	store.Enable(a.ID, true)
	if s.collect(context.Background(), a.ID) == nil {
		t.Fatal("failure target reported healthy")
	}
	s.deliverNotifications(context.Background())
	if len(received) != 1 || received[0].RuleID != "connection" || received[0].Status != "firing" {
		t.Fatalf("connection event not delivered: %v", received)
	}
	healthy.Store(true)
	s.mu.Lock()
	delete(s.retry, a.ID)
	s.mu.Unlock()
	if err = s.collect(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	s.deliverNotifications(context.Background())
	if len(received) != 2 || received[1].Status != "resolved" {
		t.Fatalf("recovery not delivered: %v", received)
	}
	s.collect(context.Background(), a.ID)
	s.deliverNotifications(context.Background())
	if len(received) != 2 || integrationRecord(t, store, i.ID).Integration.LastStatus != "sent" {
		t.Fatal("healthy collection duplicated notifications")
	}
}

func TestNotificationHistoryFreshnessAndIsolation(t *testing.T) {
	asset := Asset{ID: "a", Name: "A", Enabled: true, Status: "connected"}
	end := int64(10000)
	rows := []Observation{}
	for at := end - 150; at <= end; at += 15 {
		rows = append(rows, Observation{AssetID: "a", Time: at, Values: map[string]float64{"errors": 3}})
	}
	events, _ := notificationEvents(asset, rows, end)
	if _, ok := events["errors:a"]; !ok {
		t.Fatal("continuous error event missing")
	}
	rows[len(rows)-1].Values = nil
	events, recovery := notificationEvents(asset, rows, end)
	if len(events) != 0 || recovery["errors"] {
		t.Fatal("NULL treated as healthy or firing")
	}
	rows[len(rows)-1].Values = map[string]float64{"errors": 3}
	rows = append(rows[:4], rows[5:]...)
	events, _ = notificationEvents(asset, rows, end)
	if len(events) != 0 {
		t.Fatal("gap satisfied sustained condition")
	}
	for n := range rows {
		rows[n].AssetID = "b"
	}
	events, _ = notificationEvents(asset, rows, end)
	if len(events) != 0 {
		t.Fatal("values crossed infrastructure IDs")
	}
	asset.Status = "error"
	events, recovery = notificationEvents(asset, nil, end)
	if events["connection:a"].Severity != "critical" || recovery["connection"] {
		t.Fatal("connection failure semantics incorrect")
	}
}
func TestNotificationEventFixtures(t *testing.T) {
	raw, err := os.ReadFile("testweb/event-fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name                 string
		Seconds              int64
		Values               map[string]float64
		Want                 []string
		NullLast, Gap, Stale bool
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			asset := Asset{ID: "a", Name: "API", Enabled: true, Status: "connected"}
			end := int64(10000)
			rows := []Observation{}
			for age := f.Seconds; age >= 0; age -= 15 {
				if f.Gap && age == 60 {
					continue
				}
				at := end - age
				if f.Stale {
					at -= 60
				}
				values := f.Values
				if f.NullLast && age == 0 {
					values = nil
				}
				rows = append(rows, Observation{AssetID: "a", Time: at, Values: values})
			}
			events, _ := notificationEvents(asset, rows, end)
			got := []string{}
			for _, event := range events {
				got = append(got, event.RuleID)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(f.Want, ",") {
				t.Fatalf("got %v, want %v", got, f.Want)
			}
		})
	}
}
func TestNotificationSubscriptionFiltersAndReplacement(t *testing.T) {
	store := testStore(t)
	i := integrationForTest(t, store, "https://receiver.example/events")
	save := func(enabled bool, severities []string, endpoint *string) {
		t.Helper()
		var err error
		i, err = store.SaveIntegration(IntegrationInput{ID: i.ID, Name: i.Name, Provider: i.Provider, Version: i.Version, Enabled: enabled, Severities: severities, Recovery: true, URL: endpoint})
		if err != nil {
			t.Fatal(err)
		}
	}
	save(true, []string{"critical"}, nil)
	asset := Asset{ID: "api", Enabled: true, Status: "connected"}
	critical := NotificationEvent{ID: "errors:api", RuleID: "errors", AssetID: "api", Severity: "critical", Status: "firing"}
	warning := NotificationEvent{ID: "disk:api", RuleID: "disk", AssetID: "api", Severity: "warning", Status: "firing"}
	current := map[string]NotificationEvent{critical.ID: critical, warning.ID: warning}
	store.queueNotifications(asset, current, nil, 100)
	r := integrationRecord(t, store, i.ID)
	if len(r.Queue) != 1 || r.Queue[0].Event.Severity != "critical" {
		t.Fatal("severity filter ignored")
	}
	old := i
	delivery := r.Queue[0].ID
	endpoint := "https://replacement.example/events"
	save(true, []string{"critical"}, &endpoint)
	store.finishDelivery(old, delivery, nil, 101)
	r = integrationRecord(t, store, i.ID)
	if len(r.Queue) != 0 || len(r.Active) != 0 || r.Integration.LastStatus != "" {
		t.Fatal("old pending state leaked to replacement")
	}
	save(false, []string{"critical"}, nil)
	store.queueNotifications(asset, current, nil, 102)
	if len(integrationRecord(t, store, i.ID).Queue) != 0 {
		t.Fatal("disabled subscription queued events")
	}
}
