package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

type RoutingOptions struct {
	AssetIDs     []string `json:"assetIds"`
	Environments []string `json:"environments"`
	Tags         []string `json:"tags"`
	RuleIDs      []string `json:"ruleIds"`
	GroupSeconds int      `json:"groupSeconds"`
	Summary      string   `json:"summary"`
	SummaryHour  int      `json:"summaryHour"`
}

type Integration struct {
	RoutingOptions
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Enabled    bool     `json:"enabled"`
	Severities []string `json:"severities"`
	Recovery   bool     `json:"recovery"`
	Version    int      `json:"version"`
	HasURL     bool     `json:"hasUrl"`
	HasToken   bool     `json:"hasToken"`
	Host       string   `json:"host"`
	LastAt     string   `json:"lastAt"`
	LastStatus string   `json:"lastStatus"`
	LastError  string   `json:"lastError"`
	Pending    int      `json:"pending"`
}
type IntegrationInput struct {
	RoutingOptions
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Enabled    bool     `json:"enabled"`
	Severities []string `json:"severities"`
	Recovery   bool     `json:"recovery"`
	Version    int      `json:"version"`
	URL        *string  `json:"url"`
	Token      *string  `json:"token"`
}
type NotificationEvent struct {
	ID          string              `json:"id"`
	RuleID      string              `json:"ruleId"`
	AssetID     string              `json:"assetId"`
	AssetName   string              `json:"assetName"`
	Environment string              `json:"environment"`
	Title       string              `json:"title"`
	Severity    string              `json:"severity"`
	Status      string              `json:"status"`
	At          string              `json:"at"`
	Values      map[string]float64  `json:"values"`
	Tags        []string            `json:"tags,omitempty"`
	Condition   string              `json:"condition,omitempty"`
	DetailURL   string              `json:"detailUrl,omitempty"`
	Members     []NotificationEvent `json:"members,omitempty"`
	SummaryText string              `json:"summaryText,omitempty"`
}
type notificationDelivery struct {
	ID       string
	Event    NotificationEvent
	Attempts int
	Next     int64
	Created  int64
}
type storedIntegration struct {
	Integration Integration
	URL         string
	Token       string
	Active      map[string]NotificationEvent
	Queue       []notificationDelivery
	LastSummary string
}

// URLs may contain webhook credentials. The complete record, including the
// durable transition state and outbox, uses the registry's authenticated cipher.
func (s *Store) integrationsLocked() (map[string]storedIntegration, error) {
	rows, err := s.db.Query("SELECT id,payload FROM integrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]storedIntegration{}
	for rows.Next() {
		var id string
		var b []byte
		if err = rows.Scan(&id, &b); err != nil {
			return nil, err
		}
		n := s.aead.NonceSize()
		if len(b) < n {
			return nil, errors.New("encrypted integration corrupt")
		}
		raw, e := s.aead.Open(nil, b[:n], b[n:], []byte("integration:"+id))
		if e != nil {
			return nil, e
		}
		var record storedIntegration
		if e = json.Unmarshal(raw, &record); e != nil {
			return nil, e
		}
		out[id] = record
	}
	return out, rows.Err()
}
func (s *Store) putIntegrationLocked(record storedIntegration) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	b := s.aead.Seal(nonce, nonce, raw, []byte("integration:"+record.Integration.ID))
	_, err = s.db.Exec("INSERT INTO integrations(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", record.Integration.ID, b)
	return err
}
func publicIntegration(record storedIntegration) Integration {
	i := record.Integration
	i.HasURL, i.HasToken, i.Pending = record.URL != "", record.Token != "", len(record.Queue)
	if u, err := url.Parse(record.URL); err == nil {
		i.Host = u.Hostname()
	}
	return i
}
func (s *Store) Integrations() ([]Integration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.integrationsLocked()
	if err != nil {
		return nil, err
	}
	out := []Integration{}
	for _, record := range all {
		out = append(out, publicIntegration(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name || out[i].Name == out[j].Name && out[i].ID < out[j].ID
	})
	return out, nil
}
func validateIntegration(record storedIntegration) error {
	i := record.Integration
	if err := validateRouting(i.RoutingOptions); err != nil {
		return err
	}
	if strings.TrimSpace(i.Name) == "" || len([]rune(i.Name)) > 120 || strings.ContainsAny(i.Name, "\x00\r\n") {
		return errors.New("연동 이름은 120자 이내로 입력하세요")
	}
	if i.Provider != "webhook" && i.Provider != "slack" && i.Provider != "discord" && i.Provider != "teams" {
		return errors.New("지원하는 연동을 선택하세요")
	}
	if len(i.Severities) == 0 || len(i.Severities) > 2 {
		return errors.New("전송할 심각도를 선택하세요")
	}
	seen := map[string]bool{}
	for _, severity := range i.Severities {
		if severity != "critical" && severity != "warning" || seen[severity] {
			return errors.New("전송할 심각도를 확인하세요")
		}
		seen[severity] = true
	}
	u, err := checkedURL(record.URL)
	if err != nil {
		return errors.New("웹훅의 http:// 또는 https:// 주소를 입력하세요")
	}
	if i.Provider != "webhook" && u.Scheme != "https" {
		return errors.New("메신저 웹훅은 https:// 주소를 사용하세요")
	}
	if i.Provider == "slack" && (u.Host != "hooks.slack.com" && u.Host != "hooks.slack-gov.com" || !strings.HasPrefix(u.Path, "/services/")) {
		return errors.New("Slack Incoming Webhook 주소를 입력하세요")
	}
	if i.Provider == "discord" && (u.Host != "discord.com" && u.Host != "discordapp.com" || !strings.HasPrefix(u.Path, "/api/webhooks/")) {
		return errors.New("Discord 채널 웹훅 주소를 입력하세요")
	}
	if len(record.Token) > 4096 || strings.ContainsAny(record.Token, "\x00\r\n") {
		return errors.New("인증 토큰 형식을 확인하세요")
	}
	if i.Provider != "webhook" && record.Token != "" {
		return errors.New("Bearer 인증은 일반 웹훅에서만 사용합니다")
	}
	return nil
}
func (s *Store) SaveIntegration(input IntegrationInput) (Integration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.integrationsLocked()
	if err != nil {
		return Integration{}, err
	}
	record := storedIntegration{Active: map[string]NotificationEvent{}, Queue: []notificationDelivery{}}
	if input.ID == "" {
		if len(all) >= 20 {
			return Integration{}, errors.New("연동은 최대 20개까지 등록할 수 있습니다")
		}
		input.ID = ID()
	} else {
		var ok bool
		record, ok = all[input.ID]
		if !ok {
			return Integration{}, errors.New("등록된 연동이 없습니다")
		}
		if record.Integration.Version != input.Version {
			return Integration{}, errors.New("다른 변경이 저장되었습니다. 목록을 다시 불러오세요")
		}
	}
	old := record
	i := record.Integration
	i.ID, i.Name, i.Provider, i.Enabled, i.Severities, i.Recovery = input.ID, strings.TrimSpace(input.Name), input.Provider, input.Enabled, input.Severities, input.Recovery
	i.RoutingOptions = input.RoutingOptions
	i.Version++
	record.Integration = i
	if input.URL != nil {
		record.URL = strings.TrimSpace(*input.URL)
	}
	if input.Token != nil {
		record.Token = *input.Token
	}
	if err = validateIntegration(record); err != nil {
		return Integration{}, err
	}
	// A changed destination/filter starts a new subscription; never send old
	// pending messages to a replacement endpoint or a disabled subscription.
	if old.URL != record.URL || old.Token != record.Token || old.Integration.Provider != i.Provider || old.Integration.Enabled != i.Enabled || strings.Join(old.Integration.Severities, ",") != strings.Join(i.Severities, ",") || old.Integration.Recovery != i.Recovery || !reflect.DeepEqual(old.Integration.RoutingOptions, i.RoutingOptions) {
		for _, d := range record.Queue {
			_ = s.historyLocked(old.Integration, d, "cancelled", "수신 설정 변경으로 취소됨", time.Now().Unix())
		}
		record.Active, record.Queue = map[string]NotificationEvent{}, []notificationDelivery{}
		record.LastSummary = ""
		record.Integration.LastAt, record.Integration.LastError, record.Integration.LastStatus = "", "", ""
	}
	if err = s.putIntegrationLocked(record); err != nil {
		return Integration{}, err
	}
	return publicIntegration(record), nil
}
func (s *Store) DeleteIntegration(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.integrationsLocked()
	if e != nil {
		return e
	}
	if record, ok := all[id]; ok {
		for _, d := range record.Queue {
			_ = s.historyLocked(record.Integration, d, "cancelled", "수신 연동 삭제로 취소됨", time.Now().Unix())
		}
	}
	result, err := s.db.Exec("DELETE FROM integrations WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return errors.New("등록된 연동이 없습니다")
	}
	return err
}
func (s *Store) forgetAssetNotifications(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, e := s.operationsLocked("event")
	if e == nil {
		for _, b := range raw {
			var v EventRecord
			if json.Unmarshal(b, &v) == nil && v.Event.AssetID == id && v.ResolvedAt == "" {
				v.ResolvedAt = nowString()
				v.Event.Status = "cancelled"
				v.UpdatedAt = v.ResolvedAt
				v.Version++
				_ = s.putOperationLocked("event", v.ID, time.Now().Unix(), v)
			}
		}
	}
	all, err := s.integrationsLocked()
	if err != nil {
		return
	}
	for _, record := range all {
		for key, event := range record.Active {
			if event.AssetID == id {
				delete(record.Active, key)
			}
		}
		queue := []notificationDelivery{}
		for _, delivery := range record.Queue {
			if delivery.Event.AssetID != id {
				queue = append(queue, delivery)
			} else {
				_ = s.historyLocked(record.Integration, delivery, "cancelled", "인프라 수집 중지·설정 변경으로 취소됨", time.Now().Unix())
			}
		}
		record.Queue = queue
		_ = s.putIntegrationLocked(record)
	}
}
func subscribed(i Integration, severity string) bool {
	for _, v := range i.Severities {
		if v == severity {
			return true
		}
	}
	return false
}
func appendDelivery(record *storedIntegration, event NotificationEvent, at int64) bool {
	if len(record.Queue) >= 1000 {
		return false
	}
	record.Queue = append(record.Queue, notificationDelivery{ID: ID(), Event: event, Next: at, Created: at})
	return true
}
func (s *Store) queueNotifications(asset Asset, current map[string]NotificationEvent, recoverable map[string]bool, at int64, latestValues ...map[string]float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, event := range current {
		event.Tags = asset.Tags
		event.DetailURL = notificationDetailURL(event.RuleID)
		current[key] = event
	}
	if err := s.journalLocked(asset, current, recoverable, at); err != nil {
		return err
	}
	all, err := s.integrationsLocked()
	if err != nil {
		return err
	}
	for _, record := range all {
		if !record.Integration.Enabled {
			continue
		}
		if record.Active == nil {
			record.Active = map[string]NotificationEvent{}
		}
		for key, event := range current {
			if _, exists := record.Active[key]; exists || !routeMatches(record.Integration, event) || s.mutedLocked(event, at) {
				continue
			}
			if appendDelivery(&record, event, at) {
				n := len(record.Queue) - 1
				record.Queue[n].Next += int64(record.Integration.GroupSeconds)
				if err = s.historyLocked(record.Integration, record.Queue[n], "queued", "", at); err != nil {
					return err
				}
				record.Active[key] = event
			}
		}
		for key, event := range record.Active {
			if event.AssetID != asset.ID {
				continue
			}
			if !asset.Enabled {
				delete(record.Active, key)
				continue
			}
			if _, active := current[key]; active || !recoverable[event.RuleID] {
				continue
			}
			if record.Integration.Recovery && !s.mutedLocked(event, at) {
				event.Status, event.At = "resolved", time.Unix(at, 0).UTC().Format(time.RFC3339)
				prior := event.Values
				event.Values = map[string]float64{}
				if len(latestValues) > 0 {
					for id := range prior {
						if value, ok := latestValues[0][id]; ok {
							event.Values[id] = value
						}
					}
				}
				if !appendDelivery(&record, event, at) {
					continue
				}
				if err = s.historyLocked(record.Integration, record.Queue[len(record.Queue)-1], "queued", "", at); err != nil {
					return err
				}
			}
			delete(record.Active, key)
		}
		if err = s.putIntegrationLocked(record); err != nil {
			return err
		}
	}
	return nil
}

func notificationPayload(provider string, event NotificationEvent) any {
	status := map[string]string{"firing": "발생", "resolved": "복구", "test": "테스트", "summary": "운영 요약"}[event.Status]
	severity := map[string]string{"critical": "긴급", "warning": "주의", "notice": "안내"}[event.Severity]
	text := fmt.Sprintf("[Pulse Ops · %s · %s] %s\n대상: %s · %s\n시각: %s", status, severity, event.Title, event.AssetName, event.Environment, event.At)
	if len(event.Members) > 0 {
		names := []string{}
		for _, e := range event.Members {
			names = append(names, e.AssetName)
		}
		text += fmt.Sprintf("\n묶음 %d건: %s", len(event.Members), strings.Join(names, " · "))
	}
	if event.Condition != "" {
		text += "\n조건: " + event.Condition
	}
	keys := []string{}
	for key := range event.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if event.Status == "summary" {
			continue
		}
		label, unit := metricLabel(key)
		text += fmt.Sprintf("\n%s: %.4g %s", label, event.Values[key], unit)
	}
	if event.SummaryText != "" {
		text += "\n" + event.SummaryText
	}
	if event.DetailURL != "" {
		text += "\n상세: " + event.DetailURL
	}
	// Bound messages for Discord's 2000-character limit and Teams card sizes.
	if r := []rune(text); len(r) > 1800 {
		text = string(r[:1800])
	}
	switch provider {
	case "slack":
		return map[string]any{"text": text, "blocks": []any{map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": text, "emoji": false}}}}
	case "discord":
		return map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{}}}
	case "teams":
		return map[string]any{"type": "message", "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": nil, "content": map[string]any{"type": "AdaptiveCard", "version": "1.2", "$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "body": []any{map[string]any{"type": "TextBlock", "text": text, "wrap": true}}}}}}
	default:
		return map[string]any{"source": "pulse-ops", "version": 1, "event": event}
	}
}
func sendNotification(ctx context.Context, client *http.Client, record storedIntegration, event NotificationEvent) error {
	if err := validateIntegration(record); err != nil {
		return err
	}
	endpoint := record.URL
	if record.Integration.Provider == "discord" {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		q.Set("wait", "true")
		u.RawQuery = q.Encode()
		endpoint = u.String()
	}
	body, err := json.Marshal(notificationPayload(record.Integration.Provider, event))
	if err != nil {
		return errors.New("전송 내용을 만들 수 없습니다")
	}
	r, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("웹훅 주소를 확인하세요")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", "Pulse-Ops/1")
	if record.Token != "" {
		r.Header.Set("Authorization", "Bearer "+record.Token)
	}
	// Never forward credentials or event contents across a redirect. Errors and
	// response bodies can echo secrets, so expose only a fixed error or HTTP code.
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safeClient.Do(r)
	if err != nil {
		return errors.New("웹훅에 연결할 수 없습니다. 주소·네트워크·인증서를 확인하세요")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("웹훅이 HTTP %d로 응답했습니다", response.StatusCode)
	}
	return nil
}
func (s *Service) deliverNotifications(ctx context.Context) {
	s.store.mu.Lock()
	all, err := s.store.integrationsLocked()
	s.store.mu.Unlock()
	if err != nil {
		return
	}
	type job struct {
		record     storedIntegration
		deliveries []notificationDelivery
		event      NotificationEvent
	}
	jobs := []job{}
	now := time.Now().Unix()
	for _, record := range all {
		if !record.Integration.Enabled {
			continue
		}
		blocked := map[string]bool{}
		eligible := []notificationDelivery{}
		s.store.mu.Lock()
		latest, e := s.store.integrationsLocked()
		live, ok := latest[record.Integration.ID]
		if e != nil || !ok || !live.Integration.Enabled {
			s.store.mu.Unlock()
			continue
		}
		record = live
		kept := []notificationDelivery{}
		for _, delivery := range record.Queue {
			muted := s.store.mutedLocked(delivery.Event, now)
			if delivery.Event.Status == "summary" {
				assets, _ := s.store.allLocked()
				list := []Asset{}
				for _, a := range assets {
					list = append(list, a.Asset)
				}
				muted = s.store.summaryMutedLocked(record.Integration, list, now)
			}
			if muted {
				_ = s.store.historyLocked(record.Integration, delivery, "suppressed", "음소거·점검 시간", now)
				if delivery.Event.Status == "firing" {
					delete(record.Active, delivery.Event.RuleID+":"+delivery.Event.AssetID)
				}
				continue
			}
			kept = append(kept, delivery)
			key := delivery.Event.ID
			if blocked[key] {
				continue
			}
			blocked[key] = true
			if delivery.Next <= now {
				eligible = append(eligible, delivery)
			}
		}
		if len(kept) != len(record.Queue) {
			record.Queue = kept
			_ = s.store.putIntegrationLocked(record)
		}
		s.store.mu.Unlock()
		if len(eligible) == 0 {
			continue
		}
		first := eligible[0]
		batch := []notificationDelivery{first}
		event := first.Event
		if record.Integration.GroupSeconds > 0 && event.Status == "firing" {
			for _, d := range eligible[1:] {
				if len(batch) >= 30 {
					break
				}
				if d.Event.Status == event.Status && d.Event.RuleID == event.RuleID && d.Event.Environment == event.Environment && d.Event.Severity == event.Severity && d.Attempts == first.Attempts {
					batch = append(batch, d)
				}
			}
			if len(batch) > 1 {
				event.ID = "group:" + first.ID
				event.Members = []NotificationEvent{}
				for _, d := range batch {
					event.Members = append(event.Members, d.Event)
				}
				event.AssetName = fmt.Sprintf("%d개 인프라", len(batch))
				event.AssetID = ""
			}
		}
		jobs = append(jobs, job{record, batch, event})
	}
	// At most four outbound requests at a time, independent of collection slots.
	for start := 0; start < len(jobs); start += 4 {
		done := make(chan struct{}, 4)
		for _, j := range jobs[start:min(start+4, len(jobs))] {
			go func(j job) {
				defer func() { done <- struct{}{} }()
				requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				// Recheck version and mute state immediately before starting the request.
				s.store.mu.Lock()
				latest, e := s.store.integrationsLocked()
				live, ok := latest[j.record.Integration.ID]
				valid := e == nil && ok && live.Integration.Enabled && live.Integration.Version == j.record.Integration.Version
				for _, d := range j.deliveries {
					if s.store.mutedLocked(d.Event, time.Now().Unix()) {
						valid = false
					}
				}
				s.store.mu.Unlock()
				if !valid {
					cancel()
					return
				}
				err := sendNotification(requestCtx, http.DefaultClient, j.record, j.event)
				cancel()
				for _, d := range j.deliveries {
					s.store.finishDelivery(j.record.Integration, d.ID, err, time.Now().Unix())
				}
			}(j)
		}
		for count := 0; count < min(4, len(jobs)-start); count++ {
			<-done
		}
	}
}
func (s *Store) finishDelivery(integration Integration, id string, result error, at int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.integrationsLocked()
	if err != nil {
		return
	}
	record, ok := all[integration.ID]
	if !ok || record.Integration.Version != integration.Version {
		return
	}
	for index, delivery := range record.Queue {
		if delivery.ID != id {
			continue
		}
		delivery.Attempts++
		i := &record.Integration
		i.LastAt, i.LastError, i.LastStatus = time.Unix(at, 0).UTC().Format(time.RFC3339), "", "sent"
		if result != nil {
			i.LastError, i.LastStatus = result.Error(), "retrying"
		}
		if result == nil || delivery.Attempts >= 5 || at-delivery.Created >= 86400 {
			record.Queue = append(record.Queue[:index], record.Queue[index+1:]...)
			if result != nil {
				i.LastStatus = "failed"
			}
		} else {
			delivery.Next = at + int64(15*(1<<delivery.Attempts))
			record.Queue[index] = delivery
		}
		if s.putIntegrationLocked(record) == nil {
			_ = s.historyLocked(integration, delivery, i.LastStatus, i.LastError, at)
			s.Record(integration.ID, "notification."+delivery.Event.Status, i.LastStatus)
		}
		return
	}
}
func (s *Service) runNotifications(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.queueSummaries(time.Now())
			s.deliverNotifications(ctx)
		}
	}
}
func (s *Store) recordNotificationTest(i Integration, result error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.integrationsLocked()
	if err != nil {
		return
	}
	record, ok := all[i.ID]
	if !ok || record.Integration.Version != i.Version {
		return
	}
	record.Integration.LastAt, record.Integration.LastStatus, record.Integration.LastError = nowString(), "sent", ""
	if result != nil {
		record.Integration.LastStatus, record.Integration.LastError = "failed", result.Error()
	}
	_ = s.putIntegrationLocked(record)
	event := NotificationEvent{ID: ID(), RuleID: "test", Title: "이벤트 알림 연동 테스트", Severity: "notice", Status: "test", AssetName: "Pulse Ops", Environment: "테스트", At: nowString(), Values: map[string]float64{}}
	_ = s.historyLocked(i, notificationDelivery{ID: ID(), Event: event, Attempts: 1, Created: time.Now().Unix()}, record.Integration.LastStatus, record.Integration.LastError, time.Now().Unix())
}
func (s *Service) installIntegrationAPI(api *http.ServeMux) {
	api.HandleFunc("GET /integrations/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Integrations()
		var i Integration
		found := false
		for _, item := range items {
			if item.ID == r.PathValue("id") {
				i = item
				found = true
				break
			}
		}
		if err != nil || !found {
			fail(w, 404, "등록된 연동이 없습니다")
			return
		}
		event := NotificationEvent{ID: "preview", RuleID: "node-cpu", Title: "메시지 예시 · CPU 사용률", Severity: "warning", Status: "firing", AssetName: "예시 대상", Environment: "production", At: nowString(), Condition: "CPU 사용률 > 80% · 5분", Values: map[string]float64{"node-cpu": 85}, DetailURL: notificationDetailURL("node-cpu")}
		records, _ := operationList[EventRecord](s.store, "event")
		sort.Slice(records, func(a, b int) bool { return records[a].UpdatedAt > records[b].UpdatedAt })
		sample := true
		for _, v := range records {
			if routeMatches(i, v.Event) {
				event = v.Event
				sample = false
				break
			}
		}
		writeJSON(w, 200, map[string]any{"sample": sample, "event": event, "payload": notificationPayload(i.Provider, event)})
	})
	api.HandleFunc("POST /integrations/{id}/clone", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Integrations()
		if err != nil {
			fail(w, 503, "목록을 읽을 수 없습니다")
			return
		}
		for _, i := range items {
			if i.ID == r.PathValue("id") {
				writeJSON(w, 200, IntegrationInput{Name: i.Name + " 복사", Provider: i.Provider, Severities: i.Severities, Recovery: i.Recovery, RoutingOptions: i.RoutingOptions})
				return
			}
		}
		fail(w, 404, "연동이 없습니다")
	})
	api.HandleFunc("GET /integrations", func(w http.ResponseWriter, r *http.Request) {
		items, err := s.store.Integrations()
		if err != nil {
			fail(w, 503, "연동 목록을 읽을 수 없습니다")
			return
		}
		writeJSON(w, 200, items)
	})
	save := func(w http.ResponseWriter, r *http.Request) {
		var input IntegrationInput
		if !decode(w, r, &input) {
			return
		}
		id := r.PathValue("id")
		if input.ID != "" && input.ID != id {
			fail(w, 400, "ID 불일치")
			return
		}
		input.ID = id
		item, err := s.store.SaveIntegration(input)
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		s.store.Record(item.ID, "integration.save", "ok")
		status := 200
		if id == "" {
			status = 201
		}
		writeJSON(w, status, item)
	}
	api.HandleFunc("POST /integrations", save)
	api.HandleFunc("PUT /integrations/{id}", save)
	api.HandleFunc("DELETE /integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.store.DeleteIntegration(r.PathValue("id")); err != nil {
			fail(w, 404, "등록된 연동이 없습니다")
			return
		}
		s.store.Record(r.PathValue("id"), "integration.delete", "ok")
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	api.HandleFunc("POST /integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		s.store.mu.Lock()
		all, err := s.store.integrationsLocked()
		s.store.mu.Unlock()
		record, ok := all[r.PathValue("id")]
		if err != nil || !ok {
			fail(w, 404, "등록된 연동이 없습니다")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		event := NotificationEvent{ID: ID(), RuleID: "test", Title: "이벤트 알림 연동 테스트", Severity: "notice", Status: "test", AssetName: "Pulse Ops", Environment: "테스트", At: nowString(), Values: map[string]float64{}}
		err = sendNotification(ctx, http.DefaultClient, record, event)
		s.store.recordNotificationTest(record.Integration, err)
		status := "sent"
		if err != nil {
			status = "failed"
		}
		s.store.Record(record.Integration.ID, "notification.test", status)
		if err != nil {
			fail(w, 422, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"message": "테스트 알림을 전송했습니다"})
	})
}
