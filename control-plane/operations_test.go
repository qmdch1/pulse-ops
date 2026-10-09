package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func opsCall(t *testing.T, s *Service, method, path string, body any, want int) []byte {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "http://localhost:13000/api/control"+path, bytes.NewReader(raw))
	r.Header.Set("Origin", "http://localhost:13000")
	w := httptest.NewRecorder()
	s.webHandler("test", "", "", "").ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}
func opsAsset(t *testing.T, store *Store, name string) Asset {
	t.Helper()
	password := "private-test-password"
	a, e := store.Save(AssetInput{Asset: Asset{Name: name, Kind: "http", Address: "http://localhost:8080", Environment: "prod", Tags: []string{"team-api"}}, Password: &password})
	if e != nil {
		t.Fatal(e)
	}
	a, e = store.Enable(a.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func firingFor(a Asset) NotificationEvent {
	return NotificationEvent{ID: "connection:" + a.ID, RuleID: "connection", AssetID: a.ID, AssetName: a.Name, Environment: a.Environment, Tags: a.Tags, Severity: "critical", Status: "firing", Title: "연결 실패", At: nowString(), Values: map[string]float64{"targets-down": 1}}
}
func queueFor(t *testing.T, store *Store, a Asset, event NotificationEvent, at int64) {
	t.Helper()
	if e := store.queueNotifications(a, map[string]NotificationEvent{event.ID: event}, map[string]bool{}, at); e != nil {
		t.Fatal(e)
	}
}

func TestOperationsEventWorkflowWithoutIntegration(t *testing.T) {
	store := testStore(t)
	s := newService(store, []string{"http://localhost:13000"})
	a := opsAsset(t, store, "API")
	event := firingFor(a)
	queueFor(t, store, a, event, time.Now().Unix())
	queueFor(t, store, a, event, time.Now().Unix()+15)
	records, e := operationList[EventRecord](store, "event")
	if e != nil || len(records) != 1 {
		t.Fatalf("journal missing without a channel: %+v %v", records, e)
	}
	v := records[0]
	raw := opsCall(t, s, "PUT", "/events/"+v.ID, map[string]any{"version": v.Version, "workflow": "in_progress", "note": "<script>確認 & 조치</script>"}, 200)
	if !bytes.Contains(raw, []byte("in_progress")) {
		t.Fatal("workflow not saved")
	}
	opsCall(t, s, "PUT", "/events/"+v.ID, map[string]any{"version": v.Version, "workflow": "resolved"}, 409)
	store.mu.Lock()
	var ciphertext []byte
	e = store.db.QueryRow("SELECT payload FROM operations WHERE kind='event' AND id=?", v.ID).Scan(&ciphertext)
	store.mu.Unlock()
	if e != nil || bytes.Contains(ciphertext, []byte("조치")) {
		t.Fatal("notes not encrypted")
	}
	if err := store.queueNotifications(a, map[string]NotificationEvent{}, map[string]bool{"connection": false}, time.Now().Unix()+30); err != nil {
		t.Fatal(err)
	}
	records, _ = operationList[EventRecord](store, "event")
	if records[0].ResolvedAt != "" {
		t.Fatal("missing evidence recovered an incident")
	}
	if err := store.queueNotifications(a, map[string]NotificationEvent{}, map[string]bool{"connection": true}, time.Now().Unix()+45); err != nil {
		t.Fatal(err)
	}
	records, _ = operationList[EventRecord](store, "event")
	if records[0].ResolvedAt == "" || records[0].Workflow != "in_progress" || len(records[0].Notes) != 1 {
		t.Fatal("automatic recovery erased operator workflow")
	}
	queueFor(t, store, a, event, time.Now().Unix()+60)
	records, _ = operationList[EventRecord](store, "event")
	if len(records) != 2 {
		t.Fatal("new episode not created")
	}
	store.forgetAssetNotifications(a.ID)
	records, _ = operationList[EventRecord](store, "event")
	for _, v := range records {
		if v.ResolvedAt == "" {
			t.Fatal("paused incident remained live")
		}
	}
}
func TestOperationsRoutingMuteExpiryAndGrouping(t *testing.T) {
	store := testStore(t)
	s := newService(store, nil)
	var mu sync.Mutex
	received := []NotificationEvent{}
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p struct{ Event NotificationEvent }
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, p.Event)
		mu.Unlock()
	}))
	defer sink.Close()
	i := integrationForTest(t, store, sink.URL)
	i, e := store.SaveIntegration(IntegrationInput{ID: i.ID, Version: i.Version, Name: i.Name, Provider: i.Provider, Enabled: true, Recovery: true, Severities: i.Severities, RoutingOptions: RoutingOptions{Environments: []string{"prod"}, Tags: []string{"team-api"}, RuleIDs: []string{"connection"}, GroupSeconds: 30}})
	if e != nil {
		t.Fatal(e)
	}
	a, b := opsAsset(t, store, "A"), opsAsset(t, store, "B")
	now := time.Now().Unix()
	mute := MuteWindow{ID: ID(), Name: "점검", AssetIDs: []string{a.ID}, StartsAt: now - 10, EndsAt: now + 10, Version: 1}
	store.mu.Lock()
	e = store.putOperationLocked("mute", mute.ID, now, mute)
	store.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	queueFor(t, store, a, firingFor(a), now)
	if len(integrationRecord(t, store, i.ID).Queue) != 0 {
		t.Fatal("muted firing was queued")
	}
	queueFor(t, store, a, firingFor(a), now+11)
	queueFor(t, store, b, firingFor(b), now+11)
	c := b
	c.ID = "outside"
	c.Environment = "staging"
	queueFor(t, store, c, firingFor(c), now+11)
	record := integrationRecord(t, store, i.ID)
	if len(record.Queue) != 2 {
		t.Fatal("scope filters or mute expiry failed")
	}
	s.deliverNotifications(context.Background())
	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 0 {
		t.Fatal("group sent before its window")
	}
	store.mu.Lock()
	record.Queue[0].Next = now - 1
	record.Queue[1].Next = now - 1
	_, _ = store.db.Exec("DELETE FROM operations WHERE kind='mute'")
	e = store.putIntegrationLocked(record)
	store.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	s.deliverNotifications(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || len(received[0].Members) != 2 {
		t.Fatalf("expected one 2-target grouped payload: %+v", received)
	}
	history, _ := operationList[DeliveryRecord](store, "delivery")
	if len(history) != 2 {
		t.Fatal("group discarded per-target delivery evidence")
	}
	for _, v := range history {
		if v.Status != "sent" || v.Attempts != 1 {
			t.Fatal("group status not saved")
		}
	}
}
func TestOperationsSuppressQueuedAndManualRetry(t *testing.T) {
	store := testStore(t)
	s := newService(store, []string{"http://localhost:13000"})
	i := integrationForTest(t, store, "http://localhost:1/secret-endpoint")
	a := opsAsset(t, store, "A")
	now := time.Now().Unix()
	queueFor(t, store, a, firingFor(a), now)
	record := integrationRecord(t, store, i.ID)
	id := record.Queue[0].ID
	for attempt := 0; attempt < 5; attempt++ {
		store.finishDelivery(i, id, errors.New("HTTP 429"), now+int64(attempt))
	}
	history, _ := operationList[DeliveryRecord](store, "delivery")
	if len(history) != 1 || history[0].Status != "failed" {
		t.Fatal("failed evidence missing")
	}
	raw := opsCall(t, s, "POST", "/deliveries/"+id+"/retry", map[string]int{"version": history[0].Version}, 200)
	if !bytes.Contains(raw, []byte("재전송")) {
		t.Fatal("retry not acknowledged")
	}
	opsCall(t, s, "POST", "/deliveries/"+id+"/retry", map[string]int{"version": history[0].Version}, 409)
	if len(integrationRecord(t, store, i.ID).Queue) != 1 {
		t.Fatal("manual retry duplicated")
	}
	mute := MuteWindow{ID: ID(), Name: "점검", StartsAt: now - 10, EndsAt: now + 100, Version: 1}
	store.mu.Lock()
	_ = store.putOperationLocked("mute", mute.ID, now, mute)
	store.mu.Unlock()
	s.deliverNotifications(context.Background())
	record = integrationRecord(t, store, i.ID)
	if len(record.Queue) != 0 || len(record.Active) != 0 {
		t.Fatal("muted pending firing retained a subscription")
	}
	history, _ = operationList[DeliveryRecord](store, "delivery")
	suppressed := false
	for _, v := range history {
		suppressed = suppressed || v.Status == "suppressed"
	}
	if !suppressed {
		t.Fatal("suppression missing from history")
	}
}
func TestOperationsSavedViewsMutesCloneImportBulk(t *testing.T) {
	store := testStore(t)
	s := newService(store, []string{"http://localhost:13000"})
	a := opsAsset(t, store, "원본")
	raw := opsCall(t, s, "POST", "/assets/"+a.ID+"/clone", nil, 201)
	var clone Asset
	_ = json.Unmarshal(raw, &clone)
	if clone.HasPassword || clone.Enabled || clone.ID == a.ID || len(clone.Tags) != 1 {
		t.Fatal("clone copied credentials or lost config")
	}
	saved, _ := store.Get(clone.ID)
	if saved.Secrets.Password != "" {
		t.Fatal("clone secretly retained password")
	}
	view := SavedView{Name: "운영 DB", AssetIDs: []string{a.ID}, Range: 3600, RefreshSeconds: 30}
	raw = opsCall(t, s, "POST", "/views", view, 200)
	_ = json.Unmarshal(raw, &view)
	view.Name = "수정"
	opsCall(t, s, "PUT", "/views/"+view.ID, view, 200)
	opsCall(t, s, "PUT", "/views/"+view.ID, view, 409)
	opsCall(t, s, "POST", "/views", SavedView{Name: "잘못된 대상", AssetIDs: []string{"absent"}, Range: 3600}, 400)
	now := time.Now().Unix()
	mute := MuteWindow{Name: "미리 점검", AssetIDs: []string{a.ID}, Tags: []string{"team-api"}, StartsAt: now + 60, EndsAt: now + 120}
	raw = opsCall(t, s, "POST", "/mutes", mute, 200)
	_ = json.Unmarshal(raw, &mute)
	mute.EndsAt = mute.StartsAt
	opsCall(t, s, "PUT", "/mutes/"+mute.ID, mute, 400)
	opsCall(t, s, "DELETE", "/mutes/"+mute.ID, nil, 200)
	opsCall(t, s, "POST", "/assets/import", map[string]any{"assets": []AssetInput{{Asset: Asset{Name: "CSV", Kind: "redis", Address: "redis", Tags: []string{"team-db"}}}}}, 200)
	before, _ := store.List()
	opsCall(t, s, "POST", "/assets/import", map[string]any{"assets": []AssetInput{{Asset: Asset{Kind: "redis"}}, {Asset: Asset{Kind: "invalid"}}}}, 400)
	after, _ := store.List()
	if len(before) != len(after) {
		t.Fatal("validation partially imported invalid CSV")
	}
	raw = opsCall(t, s, "POST", "/assets/bulk", map[string]any{"action": "pause", "items": []map[string]any{{"id": a.ID, "version": a.Version - 1}}}, 200)
	if !bytes.Contains(raw, []byte("error")) {
		t.Fatal("stale bulk version accepted")
	}
	fresh, _ := store.Get(a.ID)
	if !fresh.Asset.Enabled {
		t.Fatal("stale pause changed asset")
	}
	raw = opsCall(t, s, "POST", "/assets/bulk", map[string]any{"action": "update", "items": []map[string]any{{"id": a.ID, "version": a.Version}}, "environment": "staging", "tags": []string{"team-new"}}, 200)
	if bytes.Contains(raw, []byte("error")) {
		t.Fatal(string(raw))
	}
	fresh, _ = store.Get(a.ID)
	if fresh.Asset.Enabled || fresh.Secrets.Password == "" || fresh.Asset.Tags[0] != "team-new" {
		t.Fatal("bulk update lost credential or failed to pause")
	}
	i := integrationForTest(t, store, "https://receiver.example/secret")
	raw = opsCall(t, s, "POST", "/integrations/"+i.ID+"/clone", nil, 200)
	var copy IntegrationInput
	_ = json.Unmarshal(raw, &copy)
	if bytes.Contains(raw, []byte("secret")) || copy.URL != nil || copy.Token != nil || copy.Enabled || copy.ID != "" {
		t.Fatal("integration clone leaked destination")
	}
	opsCall(t, s, "GET", "/integrations/"+i.ID+"/preview", nil, 200)
}
func TestOperationsSummarySchedulePersistenceAndChanges(t *testing.T) {
	dir := t.TempDir()
	store, e := openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { store.db.Close() }()
	s := newService(store, nil)
	a := opsAsset(t, store, "API")
	i := integrationForTest(t, store, "https://receiver.example/events")
	i, e = store.SaveIntegration(IntegrationInput{ID: i.ID, Version: i.Version, Name: i.Name, Provider: i.Provider, Enabled: true, Severities: i.Severities, RoutingOptions: RoutingOptions{Summary: "daily", SummaryHour: 9}})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 12, 9, 1, 0, 0, seoul)
	end := now.Unix()
	for _, o := range []Observation{{AssetID: a.ID, Time: end - 86400 - 900, Values: map[string]float64{"node-cpu": 10}}, {AssetID: a.ID, Time: end - 900, Values: map[string]float64{"node-cpu": 40}}} {
		if e = store.Observe(o); e != nil {
			t.Fatal(e)
		}
	}
	s.queueSummaries(now.Add(-2 * time.Minute))
	if len(integrationRecord(t, store, i.ID).Queue) != 0 {
		t.Fatal("summary before scheduled hour")
	}
	s.queueSummaries(now)
	s.queueSummaries(now.Add(time.Minute))
	record := integrationRecord(t, store, i.ID)
	if len(record.Queue) != 1 || !strings.Contains(record.Queue[0].Event.SummaryText, "10 → 40") {
		t.Fatalf("summary duplicate or missing comparison: %+v", record.Queue)
	}
	if e = store.db.Close(); e != nil {
		t.Fatal(e)
	}
	store, e = openStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	s = newService(store, nil)
	s.queueSummaries(now.Add(2 * time.Minute))
	if len(integrationRecord(t, store, i.ID).Queue) != 1 {
		t.Fatal("restart duplicated summary")
	}
	weekly := i
	weekly.Summary = "weekly"
	if _, _, due := summaryPeriod(weekly, now.Add(24*time.Hour)); due {
		t.Fatal("weekly summary on Tuesday")
	}
	if _, seconds, due := summaryPeriod(weekly, now); !due || seconds != 7*86400 {
		t.Fatal("weekly schedule wrong")
	}
}
func TestOperationsDiagnosticUsesActualHTTPAndDraftGuidance(t *testing.T) {
	store := testStore(t)
	s := newService(store, []string{"http://localhost:13000"})
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer sink.Close()
	a, e := store.Save(AssetInput{Asset: Asset{Name: "診断", Kind: "http", Address: sink.URL}})
	if e != nil {
		t.Fatal(e)
	}
	raw := opsCall(t, s, "POST", "/assets/"+a.ID+"/diagnose", nil, 200)
	if !bytes.Contains(raw, []byte("실제 읽기 요청 성공")) || !bytes.Contains(raw, []byte("DNS")) {
		t.Fatal(string(raw))
	}
	record, _ := store.Get(a.ID)
	if record.Asset.Enabled || record.Asset.LastSeen != "" {
		t.Fatal("diagnostic enabled collection")
	}
	a, e = store.Save(AssetInput{Asset: Asset{Name: "초안", Kind: "server"}})
	if e != nil {
		t.Fatal(e)
	}
	raw = opsCall(t, s, "POST", "/assets/"+a.ID+"/diagnose", nil, 200)
	if !bytes.Contains(raw, []byte("연결 주소")) {
		t.Fatal("missing actionable draft guidance")
	}
}
