package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func aiFixture(t *testing.T) (*AIManager, AIProject) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("PULSE_AI_PROJECT_ROOT", root)
	dir := filepath.Join(root, "project")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "initial"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if raw, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, raw)
		}
	}
	m := newAIManager(testStore(t))
	p := AIProject{ID: ID(), Name: "fixture", Directory: dir, Context: "Go project", CreatedAt: time.Now().Unix()}
	m.store.mu.Lock()
	err := m.store.putOperationLocked("ai_project", p.ID, p.CreatedAt, p)
	m.store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return m, p
}
func aiConfigured(t *testing.T, m *AIManager) aiSettingsRecord {
	t.Helper()
	s := defaultAISettings()
	key := "test-key-not-a-real-credential"
	if _, err := m.saveSettings(s, &key); err != nil {
		t.Fatal(err)
	}
	cfg, err := m.settings()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
func choiceFixture(options map[string]string, choice string, confidence float64) map[string]any {
	p := map[string]float64{}
	for k := range options {
		p[k] = 0
	}
	p[choice] = 1
	return map[string]any{"type": "choice", "choice": choice, "confidence": confidence, "probabilities": p}
}
func aiJevServer(t *testing.T, m *AIManager, provider string, confidence float64, inspect func(map[string]any)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key-not-a-real-credential" {
			t.Error("invalid Jev protocol")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if inspect != nil {
			inspect(in)
		}
		qs := in["questions"].(map[string]any)
		opts := map[string]string{}
		for k := range qs["executor"].(map[string]any)["criteria"].(map[string]any) {
			opts[k] = ""
		}
		writeJSON(w, 200, map[string]any{"model": "jev-fixture", "answers": map[string]any{"executor": choiceFixture(opts, provider, confidence), "category": choiceFixture(aiCategories, "debugging", .9)}, "usage": map[string]any{"input_tokens": 220, "output_tokens": 10}})
	}))
	m.endpoint = server.URL + "/v1/systemone"
	t.Cleanup(server.Close)
	return server
}
func TestAISettingsEncryptionAndMaskedUpdates(t *testing.T) {
	m := newAIManager(testStore(t))
	cfg := aiConfigured(t, m)
	var encrypted []byte
	if err := m.store.db.QueryRow("SELECT payload FROM operations WHERE kind='ai_settings'").Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte(cfg.Key)) {
		t.Fatal("plaintext key")
	}
	s := newService(m.store, []string{"http://localhost"})
	w := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(w, httptest.NewRequest("GET", "/ai/settings", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), cfg.Key) {
		t.Fatal("settings expose key")
	}
	updated, err := m.saveSettings(cfg.Settings, nil)
	if err != nil || !updated.HasKey {
		t.Fatal(err)
	}
	again, _ := m.settings()
	if again.Key != cfg.Key {
		t.Fatal("masked edit erased key")
	}
	if _, err = m.saveSettings(cfg.Settings, nil); err == nil {
		t.Fatal("stale version accepted")
	}
	updated.Concurrency = 5
	if _, err = m.saveSettings(updated, nil); err == nil {
		t.Fatal("unbounded workers")
	}
	updated.Concurrency = 2
	clear := ""
	updated, err = m.saveSettings(updated, &clear)
	if err != nil || updated.HasKey {
		t.Fatal("clear key", err)
	}
}
func TestAIDecisionUsesPromptProfilesAndOnlyRatedProjectEvidence(t *testing.T) {
	m, p := aiFixture(t)
	cfg := aiConfigured(t, m)
	d := &AIDecision{Category: "debugging"}
	for _, job := range []AIJob{{ID: ID(), ProjectID: p.ID, Provider: "claude", Model: "configured-model", Status: "completed", Rating: "good", Decision: d}, {ID: ID(), ProjectID: "different", Provider: "codex", Status: "completed", Rating: "needs_work", Decision: d}, {ID: ID(), ProjectID: p.ID, Provider: "codex", Status: "failed", Decision: d}} {
		if err := m.putJob(job); err != nil {
			t.Fatal(err)
		}
	}
	aiJevServer(t, m, "claude", .93, func(in map[string]any) {
		state := in["state"].(map[string]any)
		if state["prompt"] != "fix the bug" || state["projectContext"] != p.Context {
			t.Error("missing context")
		}
		profiles := state["profiles"].(map[string]any)
		if profiles["codex"].(map[string]any)["strengths"] != cfg.Settings.Profiles["codex"].Strengths {
			t.Error("missing configured profile")
		}
		rows := state["ratedWork"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["provider"] != "claude" {
			t.Error("foreign or unrated results contaminated evidence")
		}
		raw, _ := json.Marshal(in)
		if bytes.Contains(raw, []byte(cfg.Key)) || bytes.Contains(raw, []byte(p.Directory)) {
			t.Error("secret/path sent as context")
		}
		options := in["questions"].(map[string]any)["executor"].(map[string]any)["criteria"].(map[string]any)
		if options["codex"] != cfg.Settings.Profiles["codex"].Strengths {
			t.Error("profile missing")
		}
	})
	result, err := m.decide(context.Background(), p, "fix the bug", cfg)
	if err != nil || result.Provider != "claude" || result.NeedsReview || result.InputTokens != 220 || !strings.Contains(result.Basis, "1건") {
		t.Fatal(result, err)
	}
}
func TestAIDecisionConfidenceAndProtocolFailureNeverFallback(t *testing.T) {
	m, p := aiFixture(t)
	cfg := aiConfigured(t, m)
	server := aiJevServer(t, m, "codex", .3, nil)
	d, err := m.decide(context.Background(), p, "fix", cfg)
	if err != nil || !d.NeedsReview {
		t.Fatal(d, err)
	}
	server.Close()
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("private raw error"))
	}))
	defer server.Close()
	m.endpoint = server.URL
	_, err = m.decide(context.Background(), p, "fix", cfg)
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	conf := .8
	for _, bad := range []AIChoice{{Type: "choice", Choice: "unknown", Confidence: &conf, Probabilities: map[string]float64{"codex": 1}}, {Type: "choice", Choice: "codex", Probabilities: map[string]float64{"codex": 1}}, {Type: "choice", Choice: "codex", Confidence: &conf, Probabilities: map[string]float64{"codex": .1, "claude": .9}}} {
		if validateAIChoice(bad, map[string]string{"codex": "", "claude": ""}) == nil {
			t.Fatal("invalid result accepted")
		}
	}
}
func TestAIProjectPathRejectsEscapesAndSubdirectories(t *testing.T) {
	m, p := aiFixture(t)
	_ = m
	if path, err := aiProjectPath(p.Directory); err != nil || path != p.Directory {
		t.Fatal(path, err)
	}
	outside := t.TempDir()
	link := filepath.Join(filepath.Dir(p.Directory), "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{outside, link, "relative", filepath.Join(p.Directory, ".git")} {
		if _, err := aiProjectPath(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
func aiHTTPAction(t *testing.T, m *AIManager, id string, input any) *httptest.ResponseRecorder {
	t.Helper()
	s := newService(m.store, []string{"http://localhost"})
	s.ai = m
	raw, _ := json.Marshal(input)
	r := httptest.NewRequest("POST", "/ai/jobs/"+id+"/action", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(w, r)
	return w
}
func TestAIReviewActionsAndOptimisticState(t *testing.T) {
	m, p := aiFixture(t)
	aiConfigured(t, m)
	j := AIJob{ID: ID(), Version: 2, ProjectID: p.ID, Prompt: "fix", Status: "review"}
	_ = m.putJob(j)
	for _, in := range []any{map[string]any{"version": 1, "action": "assign", "provider": "codex"}, map[string]any{"version": 2, "action": "assign", "provider": "invented"}, map[string]any{"version": 2, "action": "rate", "rating": "good"}} {
		if w := aiHTTPAction(t, m, j.ID, in); w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := aiHTTPAction(t, m, j.ID, map[string]any{"version": 2, "action": "assign", "provider": "claude"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, _ := m.job(j.ID)
	if saved.Status != "ready" || saved.Provider != "claude" {
		t.Fatal(saved)
	}
	w = aiHTTPAction(t, m, j.ID, map[string]any{"version": saved.Version, "action": "cancel"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	saved, _ = m.job(j.ID)
	if saved.Status != "cancelled" {
		t.Fatal(saved)
	}
	_, _ = m.updateJob(j.ID, func(j *AIJob) error {
		j.UsageReported, j.InputTokens, j.Model, j.BaseCommit = true, 99, "previous-model", "previous-base"
		return nil
	})
	saved, _ = m.job(j.ID)
	w = aiHTTPAction(t, m, j.ID, map[string]any{"version": saved.Version, "action": "retry"})
	saved, _ = m.job(j.ID)
	if w.Code != 200 || saved.Status != "queued" || saved.UsageReported || saved.InputTokens != 0 || saved.Model != "" || saved.BaseCommit != "" {
		t.Fatal("retry retained previous execution metadata", saved, w.Body.String())
	}
}
func aiFakeCLI(t *testing.T, provider, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), provider)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	if provider == "codex" {
		t.Setenv("PULSE_CODEX_BIN", path)
	} else {
		t.Setenv("PULSE_CLAUDE_BIN", path)
	}
	return path
}
func TestAIExecutionCreatesRealIsolatedGitWorktreeAndPersistsResult(t *testing.T) {
	m, p := aiFixture(t)
	cfg := aiConfigured(t, m)
	aiJevServer(t, m, "codex", .98, nil)
	aiFakeCLI(t, "codex", `cat > received-prompt.txt
printf 'changed\n' > agent-change.txt
printf '%s\n' '{"type":"thread.started","thread_id":"fixture-session"}' '{"type":"item.completed","item":{"type":"agent_message","text":"Changed file; tests not run."}}' '{"type":"turn.completed","usage":{"input_tokens":30,"output_tokens":8}}'
`)
	ctx := context.Background()
	base, _ := aiGit(ctx, p.Directory, "rev-parse", "HEAD")
	j := AIJob{ID: ID(), Version: 1, ProjectID: p.ID, Prompt: "fix $(this must not execute)", Status: "queued", CreatedAt: time.Now().Unix()}
	_ = m.putJob(j)
	m.execute(ctx, j, cfg)
	saved, err := m.job(j.ID)
	if err != nil || saved.Status != "completed" || saved.SessionID != "fixture-session" || saved.InputTokens != 30 || !saved.UsageReported || saved.Workspace == p.Directory || saved.BaseCommit != base {
		t.Fatal(saved, err)
	}
	if _, err = os.Stat(filepath.Join(p.Directory, "agent-change.txt")); !os.IsNotExist(err) {
		t.Fatal("original checkout changed")
	}
	if _, err = os.Stat(filepath.Join(saved.Workspace, "agent-change.txt")); err != nil {
		t.Fatal(err)
	}
	branch, _ := aiGit(ctx, saved.Workspace, "branch", "--show-current")
	if branch != saved.Branch || !strings.HasPrefix(branch, "codex/") {
		t.Fatal(branch)
	}
	prompt, err := os.ReadFile(filepath.Join(saved.Workspace, "received-prompt.txt"))
	if err != nil || !bytes.Contains(prompt, []byte(j.Prompt)) {
		t.Fatal("stdin prompt lost")
	}
	w := aiHTTPAction(t, m, j.ID, map[string]any{"version": saved.Version, "action": "rate", "rating": "good"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var encrypted []byte
	_ = m.store.db.QueryRow("SELECT payload FROM operations WHERE kind='ai_job' AND id=?", j.ID).Scan(&encrypted)
	if bytes.Contains(encrypted, []byte(j.Prompt)) || bytes.Contains(encrypted, []byte(saved.Output)) {
		t.Fatal("plaintext job persisted")
	}
}
func TestAIRunnerRequiresTerminalResultAndDetectsClaudeDenials(t *testing.T) {
	for _, tt := range []struct {
		provider, body string
		ok             bool
	}{{"codex", "printf '%s\\n' '{\"type\":\"thread.started\",\"thread_id\":\"x\"}'", false}, {"codex", "printf '%s\\n' '{\"type\":\"turn.failed\"}'", false}, {"claude", "printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"c\",\"result\":\"done\",\"permission_denials\":[{}]}'", false}, {"claude", "cat >/dev/null; printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"c\",\"result\":\"done\",\"usage\":{\"input_tokens\":12,\"cache_read_input_tokens\":5,\"output_tokens\":3}}'", true}} {
		t.Run(tt.provider+tt.body[:6], func(t *testing.T) {
			aiFakeCLI(t, tt.provider, tt.body)
			r, err := runAIAgent(context.Background(), tt.provider, "", t.TempDir(), "prompt")
			if (err == nil) != tt.ok {
				t.Fatal(r, err)
			}
			if tt.ok && (r.InputTokens != 17 || r.Session != "c") {
				t.Fatal(r)
			}
		})
	}
}
func TestAIQueueBoundsConcurrencyAndCancellation(t *testing.T) {
	m, p := aiFixture(t)
	cfg := aiConfigured(t, m)
	cfg.Settings.Concurrency = 1
	_, err := m.saveSettings(cfg.Settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	aiJevServer(t, m, "codex", .9, nil)
	aiFakeCLI(t, "codex", "cat >/dev/null; sleep 20")
	for i := 0; i < 2; i++ {
		_ = m.putJob(AIJob{ID: ID(), Version: 1, ProjectID: p.ID, Prompt: "fix", Status: "queued", CreatedAt: int64(i)})
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.schedule(ctx)
	defer cancel()
	deadline := time.Now().Add(3 * time.Second)
	var running AIJob
	for time.Now().Before(deadline) {
		jobs, _ := m.jobs()
		for _, j := range jobs {
			if j.Status == "running" {
				running = j
			}
		}
		if running.ID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if running.ID == "" {
		t.Fatal("worker did not start")
	}
	m.schedule(ctx)
	m.mu.Lock()
	count := len(m.active)
	m.mu.Unlock()
	if count != 1 {
		t.Fatal("concurrency limit", count)
	}
	w := aiHTTPAction(t, m, running.ID, map[string]any{"version": running.Version, "action": "cancel"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		count = len(m.active)
		m.mu.Unlock()
		if count == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count != 0 {
		t.Fatal("cancelled subprocess still occupies slot")
	}
	saved, _ := m.job(running.ID)
	if saved.Status != "cancelled" {
		t.Fatal(saved.Status)
	}
}
func TestAIRestartPreservesInterruptedWorkspace(t *testing.T) {
	m, _ := aiFixture(t)
	j := AIJob{ID: ID(), Version: 1, Status: "running", Workspace: "preserved-workspace", Output: "partial"}
	_ = m.putJob(j)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.run(ctx)
	saved, _ := m.job(j.ID)
	if saved.Status != "interrupted" || saved.Workspace != j.Workspace || saved.Output != j.Output {
		t.Fatal(saved)
	}
}

func TestAIProtocolDistinguishesMissingUsageFromReportedZero(t *testing.T) {
	for _, provider := range []string{"codex", "claude"} {
		without := `{"type":"turn.completed"}`
		with := `{"type":"turn.completed","usage":{"input_tokens":0,"output_tokens":0}}`
		if provider == "claude" {
			without = `{"type":"result","subtype":"success","result":"done"}`
			with = `{"type":"result","subtype":"success","result":"done","usage":{"input_tokens":0,"output_tokens":0}}`
		}
		var r aiRunResult
		if terminal, err := parseAIEvent(provider, []byte(without), &r); err != nil || !terminal || r.UsageReported {
			t.Fatal(provider, r, err)
		}
		if terminal, err := parseAIEvent(provider, []byte(with), &r); err != nil || !terminal || !r.UsageReported || r.InputTokens != 0 {
			t.Fatal(provider, r, err)
		}
	}
}

func TestAIShutdownWaitsForWorkersAndPersistsInterruptedState(t *testing.T) {
	m, p := aiFixture(t)
	aiConfigured(t, m)
	aiJevServer(t, m, "codex", .95, nil)
	aiFakeCLI(t, "codex", "cat >/dev/null; sleep 60")
	j := AIJob{ID: ID(), Version: 1, ProjectID: p.ID, Prompt: "fix", Status: "queued"}
	if err := m.putJob(j); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, _ := m.job(j.ID)
		if current.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not start", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	saved, _ := m.job(j.ID)
	if saved.Status != "interrupted" || saved.Workspace == "" {
		t.Fatal(saved)
	}
}
func TestAIAPIJobValidationAndAuth(t *testing.T) {
	m, p := aiFixture(t)
	s := newService(m.store, []string{"https://ops.example.invalid"})
	s.ai = m
	r := httptest.NewRequest("POST", "/api/control/ai/jobs", strings.NewReader(`{"projectId":"`+p.ID+`","prompt":"fix"}`))
	r.Header.Set("Origin", "https://evil.example.invalid")
	w := httptest.NewRecorder()
	s.webHandler("production", "operator", strings.Repeat("long", 8), "").ServeHTTP(w, r)
	if w.Code == 200 || w.Code == 202 {
		t.Fatal("unauthenticated job accepted")
	}
	for _, prompt := range []string{"", strings.Repeat("x", 32001), "fix"} {
		raw, _ := json.Marshal(map[string]string{"projectId": p.ID, "prompt": prompt})
		w = httptest.NewRecorder()
		s.apiHandler().ServeHTTP(w, httptest.NewRequest("POST", "/ai/jobs", bytes.NewReader(raw)))
		if w.Code == 202 {
			t.Fatal("unconfigured task accepted")
		}
	}
	aiConfigured(t, m)
	raw, _ := json.Marshal(map[string]string{"projectId": p.ID, "prompt": "fix"})
	w = httptest.NewRecorder()
	s.apiHandler().ServeHTTP(w, httptest.NewRequest("POST", "/ai/jobs", bytes.NewReader(raw)))
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var job AIJob
	if err := json.NewDecoder(w.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	if job.Status != "queued" {
		t.Fatal(job)
	}
	// The browser list excludes large execution output, while the detail keeps it.
	_, _ = m.updateJob(job.ID, func(j *AIJob) error { j.Output = "private execution text"; return nil })
	w = httptest.NewRecorder()
	s.apiHandler().ServeHTTP(w, httptest.NewRequest("GET", "/ai/jobs", nil))
	raw, _ = io.ReadAll(w.Body)
	if bytes.Contains(raw, []byte("private execution text")) {
		t.Fatal("list contains output")
	}
}
