package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

func validateRouting(v RoutingOptions) error {
	for _, values := range [][]string{v.AssetIDs, v.Environments, v.Tags, v.RuleIDs} {
		if err := validateLabels(values, 200); err != nil {
			return err
		}
	}
	if v.GroupSeconds != 0 && v.GroupSeconds != 30 && v.GroupSeconds != 60 && v.GroupSeconds != 120 {
		return fmt.Errorf("알림 묶음은 끄기·30·60·120초를 선택하세요")
	}
	if v.Summary != "" && v.Summary != "off" && v.Summary != "daily" && v.Summary != "weekly" || v.SummaryHour < 0 || v.SummaryHour > 23 {
		return fmt.Errorf("운영 요약 주기와 시각을 확인하세요")
	}
	for _, id := range v.RuleIDs {
		found := false
		for _, r := range notificationRules {
			if r.ID == id && r.ServerSupported {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("서버에서 판정하는 이벤트 규칙을 선택하세요")
		}
	}
	return nil
}
func routeMatches(i Integration, event NotificationEvent) bool {
	if event.Status == "summary" {
		return true
	}
	return subscribed(i, event.Severity) && matchesScope(i.AssetIDs, i.Environments, i.Tags, event) && (len(i.RuleIDs) == 0 || contains(i.RuleIDs, event.RuleID))
}
func notificationDetailURL(rule string) string {
	raw := strings.TrimRight(os.Getenv("CONTROL_PUBLIC_URL"), "/")
	if raw == "" {
		return ""
	}
	if _, err := checkedURL(raw); err != nil {
		return ""
	}
	return raw + "/#events?rule=" + rule
}
func metricLabel(id string) (string, string) {
	for _, m := range metricDefinitions {
		if m.ID == id {
			return m.Title, m.Unit
		}
	}
	return id, ""
}

var seoul = time.FixedZone("Asia/Seoul", 9*60*60)

func summaryPeriod(i Integration, now time.Time) (string, int64, bool) {
	t := now.In(seoul)
	if i.Summary != "daily" && i.Summary != "weekly" || t.Hour() < i.SummaryHour {
		return "", 0, false
	}
	days := 1
	if i.Summary == "weekly" {
		if t.Weekday() != time.Monday {
			return "", 0, false
		}
		days = 7
	}
	boundary := time.Date(t.Year(), t.Month(), t.Day(), i.SummaryHour, 0, 0, 0, seoul)
	return i.Summary + ":" + boundary.Format("2006-01-02"), int64(days * 86400), true
}
func (s *Service) queueSummaries(now time.Time) {
	s.store.mu.Lock()
	all, err := s.store.integrationsLocked()
	s.store.mu.Unlock()
	if err != nil {
		return
	}
	needed := false
	for _, record := range all {
		period, _, due := summaryPeriod(record.Integration, now)
		if record.Integration.Enabled && due && record.LastSummary != period {
			needed = true
			break
		}
	}
	if !needed {
		return
	}
	records, err := operationList[EventRecord](s.store, "event")
	if err != nil {
		return
	}
	assets, err := s.store.List()
	if err != nil {
		return
	}
	for _, record := range all {
		i := record.Integration
		period, seconds, due := summaryPeriod(i, now)
		if !i.Enabled || !due || record.LastSummary == period {
			continue
		}
		event := s.summaryEvent(i, assets, records, now.Unix(), seconds)
		s.store.mu.Lock()
		latest, e := s.store.integrationsLocked()
		live, ok := latest[i.ID]
		if e == nil && ok && live.Integration.Version == i.Version && live.Integration.Enabled && live.LastSummary != period && !s.store.summaryMutedLocked(i, assets, now.Unix()) {
			if appendDelivery(&live, event, now.Unix()) {
				live.LastSummary = period
				if s.store.putIntegrationLocked(live) == nil {
					_ = s.store.historyLocked(i, live.Queue[len(live.Queue)-1], "queued", "", now.Unix())
				}
			}
		}
		s.store.mu.Unlock()
	}
}
func (s *Store) summaryMutedLocked(i Integration, assets []Asset, at int64) bool {
	// A global window always suppresses summaries. Scoped windows suppress a
	// summary when any asset in its route is under maintenance.
	if s.mutedLocked(NotificationEvent{}, at) {
		return true
	}
	for _, a := range assets {
		e := NotificationEvent{AssetID: a.ID, Environment: a.Environment, Tags: a.Tags}
		if matchesScope(i.AssetIDs, i.Environments, i.Tags, e) && s.mutedLocked(e, at) {
			return true
		}
	}
	return false
}
func (s *Service) summaryEvent(i Integration, assets []Asset, records []EventRecord, end, seconds int64) NotificationEvent {
	start := end - seconds
	firing, recovered, active := 0, 0, 0
	for _, v := range records {
		if !routeMatches(i, v.Event) {
			continue
		}
		at, _ := time.Parse(time.RFC3339, v.Event.At)
		if at.Unix() >= start && at.Unix() <= end {
			firing++
		}
		resolved, _ := time.Parse(time.RFC3339, v.ResolvedAt)
		if v.Event.Status == "resolved" && resolved.Unix() >= start && resolved.Unix() <= end {
			recovered++
		}
		if v.ResolvedAt == "" {
			active++
		}
	}
	label := "일간"
	if seconds > 86400 {
		label = "주간"
	}
	lines := []string{fmt.Sprintf("기간: %s – %s (한국시간)", time.Unix(start, 0).In(seoul).Format("01/02 15:04"), time.Unix(end, 0).In(seoul).Format("01/02 15:04")), fmt.Sprintf("발생 %d건 · 복구 %d건 · 미복구 %d건", firing, recovered, active)}
	type change struct {
		name, metric  string
		before, after float64
		n             int
	}
	changes := []change{}
	observed := 0
	for _, a := range assets {
		if !matchesScope(i.AssetIDs, i.Environments, i.Tags, NotificationEvent{AssetID: a.ID, Environment: a.Environment, Tags: a.Tags}) {
			continue
		}
		points, e := s.store.Observations([]string{a.ID}, end-2*seconds, max(900, seconds/48))
		if e != nil {
			continue
		}
		before, after := map[string]float64{}, map[string]float64{}
		nb, na := map[string]int{}, map[string]int{}
		for _, p := range points {
			if p.Time > end || p.Error != "" {
				continue
			}
			for _, key := range []string{"node-cpu", "memory-host", "disk", "db-probe", "redis-probe", "probe-latency"} {
				if v, ok := p.Values[key]; ok {
					if p.Time < start {
						before[key] += v
						nb[key]++
					} else {
						after[key] += v
						na[key]++
					}
				}
			}
		}
		if len(na) > 0 {
			observed++
		}
		for key, sum := range after {
			if nb[key] > 0 {
				changes = append(changes, change{a.Name, key, before[key] / float64(nb[key]), sum / float64(na[key]), na[key]})
			}
		}
	}
	sort.Slice(changes, func(a, b int) bool {
		return math.Abs(changes[a].after-changes[a].before) > math.Abs(changes[b].after-changes[b].before)
	})
	lines = append(lines, fmt.Sprintf("리소스 관측 대상 %d개 · 이전 동일 길이 기간 대비 평균 변화", observed))
	for _, c := range changes[:min(8, len(changes))] {
		label, unit := metricLabel(c.metric)
		lines = append(lines, fmt.Sprintf("%s · %s: %.3g → %.3g %s (%+.3g %s, 표본 %d개)", c.name, label, c.before, c.after, unit, c.after-c.before, unit, c.n))
	}
	if len(changes) == 0 {
		lines = append(lines, "비교 가능한 이전 지표가 없습니다")
	}
	return NotificationEvent{ID: ID(), RuleID: "summary", Title: label + " 운영 요약", AssetName: "수신 대상 인프라", Environment: "선택한 환경", Severity: "notice", Status: "summary", At: time.Unix(end, 0).UTC().Format(time.RFC3339), Values: map[string]float64{"events-firing": float64(firing), "events-recovered": float64(recovered), "events-active": float64(active)}, SummaryText: strings.Join(lines, "\n"), DetailURL: notificationDetailURL("")}
}
