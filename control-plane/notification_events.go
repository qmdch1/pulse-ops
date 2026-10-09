package main

import (
	"encoding/json"
	"time"
)

type eventCheck struct {
	Metric  string  `json:"metric"`
	Op      string  `json:"op"`
	Value   float64 `json:"value"`
	Seconds int64   `json:"seconds"`
	Other   string  `json:"other"`
	Factor  float64 `json:"factor"`
}
type eventRule struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Condition       string       `json:"condition"`
	Duration        string       `json:"duration"`
	Severity        string       `json:"severity"`
	RequiredIDs     []string     `json:"requiredIds"`
	Checks          []eventCheck `json:"checks"`
	ServerSupported bool         `json:"serverSupported"`
}

var notificationRules []eventRule

func init() {
	raw, err := webFiles.ReadFile("web/data/event-rules.json")
	if err != nil || json.Unmarshal(raw, &notificationRules) != nil {
		panic("invalid embedded event rules")
	}
}
func compareEventValue(value, boundary float64, op string) bool {
	switch op {
	case ">":
		return value > boundary
	case "<":
		return value < boundary
	case ">=":
		return value >= boundary
	}
	return false
}
func eventCheckMatches(check eventCheck, metrics map[string]metricResult, end int64) bool {
	m := metrics[check.Metric]
	if m.Latest == nil || m.State != "ok" || len(m.Series) == 0 {
		return false
	}
	boundary := check.Value
	if check.Other != "" {
		other := metrics[check.Other].Latest
		if other == nil {
			return false
		}
		boundary = *other * check.Factor
	}
	points := m.Series[0].Points
	if check.Op == "slope" || check.Op == "memory-step" {
		valid := []sample{}
		for _, p := range points {
			if p.Value != nil {
				valid = append(valid, p)
			}
		}
		if len(valid) < 10 || valid[len(valid)-1].Time-valid[0].Time < check.Seconds {
			return false
		}
		if check.Op == "memory-step" {
			average := 0.0
			for _, p := range valid[:len(valid)/2] {
				average += *p.Value
			}
			average /= float64(len(valid) / 2)
			return *m.Latest > average+50 && *m.Latest > average*1.5
		}
		sx, sy, sxx, sxy := 0.0, 0.0, 0.0, 0.0
		for _, p := range valid {
			x := float64(p.Time - valid[0].Time)
			sx += x
			sy += *p.Value
			sxx += x * x
			sxy += x * *p.Value
		}
		n := float64(len(valid))
		denominator := n*sxx - sx*sx
		return denominator > 0 && (n*sxy-sx*sy)/denominator > boundary
	}
	if check.Seconds == 0 {
		return compareEventValue(*m.Latest, boundary, check.Op)
	}
	window := []sample{}
	for _, p := range points {
		if p.Time >= end-check.Seconds-15 && p.Time <= end {
			window = append(window, p)
		}
	}
	if len(window) < 2 || window[0].Time > end-check.Seconds || window[len(window)-1].Time < end-15 {
		return false
	}
	for i, p := range window {
		if p.Value == nil || !compareEventValue(*p.Value, boundary, check.Op) || i > 0 && p.Time-window[i-1].Time > 22 {
			return false
		}
	}
	return true
}
func notificationEvents(asset Asset, observations []Observation, end int64) (map[string]NotificationEvent, map[string]bool) {
	current, recoverable := map[string]NotificationEvent{}, map[string]bool{}
	if !asset.Enabled {
		return current, recoverable
	}
	metrics := map[string]metricResult{}
	for _, m := range metricResults([]Asset{asset}, observations, end, 15) {
		metrics[m.ID] = m
	}
	for _, rule := range notificationRules {
		if !rule.ServerSupported {
			continue
		}
		match, healthy := true, asset.Status == "connected"
		if rule.ID == "connection" {
			match = asset.Status == "error"
		} else {
			for _, id := range rule.RequiredIDs {
				if metrics[id].Latest == nil {
					healthy = false
				}
			}
			for _, check := range rule.Checks {
				if !eventCheckMatches(check, metrics, end) {
					match = false
					break
				}
			}
			if len(rule.Checks) == 0 {
				match = false
			}
		}
		recoverable[rule.ID] = healthy
		if !match {
			continue
		}
		values := map[string]float64{}
		for _, id := range rule.RequiredIDs {
			if value := metrics[id].Latest; value != nil {
				values[id] = *value
			}
		}
		key := rule.ID + ":" + asset.ID
		current[key] = NotificationEvent{ID: key, RuleID: rule.ID, AssetID: asset.ID, AssetName: asset.Name, Environment: asset.Environment, Title: rule.Title, Severity: rule.Severity, Status: "firing", At: time.Unix(end, 0).UTC().Format(time.RFC3339), Values: values, Tags: asset.Tags, Condition: rule.Condition + " · " + rule.Duration}
	}
	return current, recoverable
}
func (s *Service) enqueueAssetNotifications(id string) error {
	asset, err := s.store.Get(id)
	if err != nil {
		return err
	}
	end := time.Now().Unix()
	observations, err := s.store.Observations([]string{id}, end-3600, 15)
	if err != nil {
		return err
	}
	current, recoverable := notificationEvents(asset.Asset, observations, end)
	latest := map[string]float64{}
	if len(observations) > 0 {
		latest = observations[len(observations)-1].Values
	}
	return s.store.queueNotifications(asset.Asset, current, recoverable, end, latest)
}
