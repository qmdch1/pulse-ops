package main

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

var buildTime time.Time // ETags carry deployment identity; no invented build timestamp.
type metricDefinition struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Unit  string `json:"unit"`
}

var metricDefinitions = []metricDefinition{}

func init() {
	b, e := webFiles.ReadFile("web/data/metrics.json")
	if e != nil || json.Unmarshal(b, &metricDefinitions) != nil {
		panic("invalid embedded metric catalog")
	}
}

type sample struct {
	Time  int64    `json:"time"`
	Value *float64 `json:"value"`
}
type metricSeries struct {
	Labels map[string]string `json:"labels"`
	Points []sample          `json:"points"`
}
type metricResult struct {
	ID     string         `json:"id"`
	State  string         `json:"state"`
	Latest *float64       `json:"latest"`
	Series []metricSeries `json:"series"`
}
type target struct {
	Instance   string `json:"instance"`
	Job        string `json:"job"`
	Health     string `json:"health"`
	LastScrape string `json:"lastScrape"`
	LastError  string `json:"lastError"`
}
type snapshot struct {
	Mode              string         `json:"mode"`
	Connected         bool           `json:"connected"`
	CollectedAt       string         `json:"collectedAt"`
	Start             int64          `json:"start"`
	End               int64          `json:"end"`
	Step              int64          `json:"step"`
	Assets            []Asset        `json:"assets"`
	Metrics           []metricResult `json:"metrics"`
	EvaluationMetrics []metricResult `json:"evaluationMetrics"`
	Targets           []target       `json:"targets"`
	Alerts            []any          `json:"alerts"`
	History           []any          `json:"history"`
	GrafanaURL        *string        `json:"grafanaUrl"`
}
type cachedSnapshot struct {
	at    time.Time
	value snapshot
}

// One bounded cache per service; coalesces simultaneous tabs without scanning
// SQLite separately for every browser. Mutations invalidate it immediately.
type snapshotCache struct {
	sync.Mutex
	entries map[string]cachedSnapshot
}

func (s *Service) invalidateSnapshot() {
	s.snapshots.Lock()
	s.snapshots.entries = nil
	s.snapshots.Unlock()
}
func metricResults(assets []Asset, observations []Observation, end, step int64) []metricResult {
	byAsset := map[string][]Observation{}
	for _, o := range observations {
		if o.Time <= end {
			byAsset[o.AssetID] = append(byAsset[o.AssetID], o)
		}
	}
	results := make([]metricResult, 0, len(metricDefinitions))
	for _, definition := range metricDefinitions {
		result := metricResult{ID: definition.ID, State: "missing", Series: []metricSeries{}}
		live := false
		for _, a := range assets {
			rows := byAsset[a.ID]
			points := make([]sample, 0, len(rows)+1)
			observed := false
			for _, o := range rows {
				p := sample{Time: o.Time}
				if v, ok := o.Values[definition.ID]; ok && !math.IsNaN(v) && !math.IsInf(v, 0) {
					value := v
					p.Value = &value
					observed = true
				}
				points = append(points, p)
			}
			if !observed {
				continue
			}
			last := points[len(points)-1]
			if end-last.Time > step*3/2 {
				points = append(points, sample{Time: end})
			}
			if a.Enabled && last.Value != nil && end-last.Time <= 45 {
				live = true
			}
			result.Series = append(result.Series, metricSeries{Labels: map[string]string{"assetId": a.ID, "instance": a.ID, "name": a.Name, "kind": a.Kind}, Points: points})
		}
		if len(result.Series) > 0 {
			result.State = "stale"
			if live {
				result.State = "ok"
			}
			if len(result.Series) == 1 {
				series := result.Series[0]
				for _, a := range assets {
					if a.ID == series.Labels["assetId"] && a.Enabled {
						p := series.Points[len(series.Points)-1]
						if end-p.Time <= 45 {
							result.Latest = p.Value
						}
					}
				}
			}
		}
		results = append(results, result)
	}
	return results
}
func (s *Service) monitoring(w http.ResponseWriter, r *http.Request, mode string) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "range" && key != "instance") || len(values) != 1 {
			fail(w, 400, "허용되지 않은 조회 범위입니다")
			return
		}
	}
	duration := 3600
	var e error
	if raw := query.Get("range"); raw != "" {
		duration, e = strconv.Atoi(raw)
	}
	selected := query.Get("instance")
	if selected == "" {
		selected = "all"
	}
	if e != nil || (duration != 900 && duration != 3600 && duration != 21600 && duration != 86400) || len(selected) > 200 {
		fail(w, 400, "허용되지 않은 조회 범위입니다")
		return
	}
	key := strconv.Itoa(duration) + ":" + selected
	s.snapshots.Lock()
	defer s.snapshots.Unlock()
	if cached, ok := s.snapshots.entries[key]; ok && time.Since(cached.at) < 3*time.Second {
		writeJSON(w, 200, cached.value)
		return
	}
	assets, e := s.store.List()
	if e != nil {
		fail(w, 503, "등록 목록을 읽을 수 없습니다")
		return
	}
	scoped := []Asset{}
	ids := []string{}
	targets := []target{}
	for _, a := range assets {
		if selected == "all" || selected == a.ID {
			scoped = append(scoped, a)
			ids = append(ids, a.ID)
			health := a.Status
			if health == "connected" {
				health = "up"
			}
			targets = append(targets, target{a.ID, a.Kind, health, a.LastSeen, a.Message})
		}
	}
	for _, host := range s.dockerHosts(assets) {
		assets = append(assets, host)
		if selected == "all" || selected == host.ID {
			scoped = append(scoped, host)
			ids = append(ids, host.ID)
		}
	}
	if selected != "all" && len(scoped) == 0 {
		fail(w, 404, "등록되지 않은 인프라입니다")
		return
	}
	end := time.Now().Unix()
	step := int64(max(15, duration/120))
	observations, e := s.store.Observations(ids, end-int64(duration), step)
	if e != nil {
		fail(w, 503, "관측 이력을 읽을 수 없습니다")
		return
	}
	evaluation, e := s.store.Observations(ids, end-3600, 15)
	if e != nil {
		fail(w, 503, "이벤트 평가 이력을 읽을 수 없습니다")
		return
	}
	result := snapshot{Mode: mode, Connected: true, CollectedAt: time.Unix(end, 0).UTC().Format(time.RFC3339), Start: end - int64(duration), End: end, Step: step, Assets: assets, Metrics: metricResults(scoped, observations, end, step), EvaluationMetrics: metricResults(scoped, evaluation, end, 15), Targets: targets, Alerts: []any{}, History: []any{}}
	if len(s.snapshots.entries) >= 16 {
		s.snapshots.entries = nil
	}
	if s.snapshots.entries == nil {
		s.snapshots.entries = map[string]cachedSnapshot{}
	}
	s.snapshots.entries[key] = cachedSnapshot{time.Now(), result}
	writeJSON(w, 200, result)
}
