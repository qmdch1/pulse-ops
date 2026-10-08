package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/redis/go-redis/v9"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) collect(ctx context.Context, id string) error {
	s.mu.Lock()
	if s.busy[id] || time.Now().Before(s.retry[id]) {
		s.mu.Unlock()
		return errors.New("이 인프라의 수집이 진행 중입니다")
	}
	s.busy[id] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.busy, id); s.mu.Unlock() }()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	record, e := s.store.Get(id)
	if e != nil {
		return e
	}
	if !record.Asset.Enabled {
		return errors.New("수집이 중지되어 있습니다")
	}
	started := time.Now()
	values := map[string]float64{}
	raw := map[string]float64{}
	switch record.Asset.Kind {
	case "server":
		values, raw, e = s.collectSSH(ctx, record)
	case "postgres":
		values, raw, e = s.collectPostgres(ctx, record)
	case "mysql", "mariadb":
		values, raw, e = s.collectMySQL(ctx, record)
	case "oracle":
		values, raw, e = s.collectOracle(ctx, record)
	case "redis":
		values, raw, e = s.collectRedis(ctx, record)
	case "http":
		values, e = s.collectHTTP(ctx, record)
	case "application":
		values, raw, e = s.collectApplication(ctx, record)
	default:
		e = errors.New("수집 유형을 확인하세요")
	}
	if values == nil {
		values = map[string]float64{}
	}
	values["scrape-duration"] = time.Since(started).Seconds()
	values["scrape-age"] = 0
	state, message := "connected", "직접 수집 중"
	if e != nil {
		state = "error"
		message = e.Error()
		values["targets"] = 0
		values["targets-down"] = 1
		switch record.Asset.Kind {
		case "postgres", "mysql", "mariadb", "oracle":
			values["db-up"] = 0
		case "redis":
			values["redis-up"] = 0
		case "http":
			values["probe"] = 0
		}
	}
	if e == nil {
		values["targets"] = 1
		values["targets-down"] = 0
		if ready, ok := values["db-monitoring-ready"]; ok && ready == 0 {
			message = "접속 정상 · 일부 DB 통계 미수집. 모니터링 읽기 권한·DB 버전·쿼리 제한을 확인하세요"
		}
	}
	for key, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			delete(values, key)
		}
	}
	if _, err := s.store.SetState(id, record.Asset.Version, true, state, message, values); err != nil {
		return err
	}
	s.mu.Lock()
	if e != nil {
		s.failures[id]++
		s.retry[id] = time.Now().Add(time.Duration(min(300, 15*(1<<min(s.failures[id], 4)))) * time.Second)
	} else {
		delete(s.retry, id)
		delete(s.failures, id)
	}
	s.mu.Unlock()
	if e == nil {
		if raw != nil {
			s.mu.Lock()
			s.previous[id] = previousSample{At: started, Values: raw}
			history := append(s.history[id], s.previous[id])
			for len(history) > 1 && history[0].At.Before(started.Add(-62*time.Minute)) {
				history = history[1:]
			}
			if len(history) > 260 {
				history = history[len(history)-260:]
			}
			s.history[id] = history
			s.mu.Unlock()
		}
	}

	if err := s.store.Observe(Observation{AssetID: id, Time: time.Now().Unix(), Values: values, Error: func() string {
		if e != nil {
			return message
		}
		return ""
	}()}); err != nil {
		return errors.New("관측 이력을 저장할 수 없습니다")
	}
	if record.Asset.Status != state {
		s.store.Record(id, "collection."+state, state)
	}
	return e
}
func (s *Service) previousFor(id string) previousSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.previous[id]
}
func deltaRate(current map[string]float64, previous previousSample, key string) (float64, bool) {
	v, ok := current[key]
	old, exists := previous.Values[key]
	elapsed := time.Since(previous.At).Seconds()
	if !ok || !exists || elapsed <= 0 || elapsed > 3900 {
		return 0, false
	}
	if v < old {
		return 0, false
	}
	return (v - old) / elapsed, true
}
func ratio(values map[string]float64, id string, numerator, denominator float64) {
	if denominator > 0 {
		values[id] = 100 * numerator / denominator
	}
}
func parseNumber(s string) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64); return v }

const linuxMetrics = `set -e; LC_ALL=C; echo '__CPU__'; head -n 1 /proc/stat; echo '__MEM__'; cat /proc/meminfo; echo '__LOAD__'; cat /proc/loadavg; echo '__DISK__'; df -Pk /; echo '__NET__'; cat /proc/net/dev; echo '__UPTIME__'; cat /proc/uptime`

func (s *Service) collectSSH(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	connection, _, e := s.sshConnect(ctx, record.Asset.ID, false)
	if e != nil {
		return nil, nil, e
	}
	defer connection.Close()
	values := map[string]float64{}
	raw := map[string]float64{}
	osName := record.Asset.OS
	if osName == "" {
		output, err := readCommand(ctx, connection, "uname -s")
		if err == nil && strings.Contains(output, "Darwin") {
			osName = "macos"
		} else if err == nil && strings.Contains(output, "Linux") {
			osName = "linux"
		} else {
			osName = "windows"
		}
	}
	if osName == "windows" {
		command := `powershell -NoProfile -NonInteractive -Command "$o=Get-CimInstance Win32_OperatingSystem;$c=Get-CimInstance Win32_Processor;$d=Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3';@{cpu=($c|Measure-Object LoadPercentage -Average).Average;memory=(100*(1-$o.FreePhysicalMemory/$o.TotalVisibleMemorySize));disk=($d|ForEach-Object {100*(1-$_.FreeSpace/$_.Size)}|Measure-Object -Maximum).Maximum}|ConvertTo-Json -Compress"`
		output, err := readCommand(ctx, connection, command)
		if err != nil {
			return nil, nil, errors.New("Windows 리소스 조회 권한 또는 PowerShell SSH 설정을 확인하세요")
		}
		var result map[string]*float64
		if json.Unmarshal([]byte(output), &result) != nil {
			return nil, nil, errors.New("Windows 수집 결과를 해석할 수 없습니다")
		}
		for id, key := range map[string]string{"node-cpu": "cpu", "memory-host": "memory", "disk": "disk"} {
			if value := result[key]; value != nil {
				values[id] = *value
			}
		}
		return values, raw, nil
	}
	if osName == "macos" {
		output, err := readCommand(ctx, connection, `LC_ALL=C; echo '__TOP__'; top -l 1 -n 0; echo '__VM__'; vm_stat; echo '__TOTAL__'; sysctl -n hw.memsize; echo '__DISK__'; df -Pk /`)
		if err != nil {
			return nil, nil, errors.New("macOS 리소스 조회 실패")
		}
		part := ""
		var free, inactive, pageSize, total float64
		pageSize = 4096
		for _, line := range strings.Split(output, "\n") {
			if strings.HasPrefix(line, "__") {
				part = line
				continue
			}
			if strings.Contains(line, "CPU usage:") {
				fields := strings.Fields(line)
				for i, f := range fields {
					if f == "idle" && i > 0 {
						values["node-cpu"] = 100 - parseNumber(strings.TrimSuffix(fields[i-1], "%"))
					}
				}
			}
			if part == "__VM__" {
				if strings.Contains(line, "page size of") {
					fields := strings.Fields(line)
					for i, f := range fields {
						if f == "of" && i+1 < len(fields) {
							pageSize = parseNumber(fields[i+1])
						}
					}
				}
				if strings.HasPrefix(line, "Pages free:") {
					free = parseNumber(strings.TrimSuffix(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), "."))
				}
				if strings.HasPrefix(line, "Pages inactive:") {
					inactive = parseNumber(strings.TrimSuffix(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), "."))
				}
			}
			if part == "__TOTAL__" {
				total = parseNumber(line)
			}
			if part == "__DISK__" {
				parseDisk(line, values)
			}
		}
		if total > 0 {
			values["memory-host"] = 100 * (1 - (free+inactive)*pageSize/total)
		}
		return values, raw, nil
	}
	output, e := readCommand(ctx, connection, linuxMetrics)
	if e != nil {
		return nil, nil, errors.New("Linux 리소스 조회 실패. /proc 접근 권한을 확인하세요")
	}
	part := ""
	var totalMem, availableMem float64
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "__") {
			part = line
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch part {
		case "__CPU__":
			if fields[0] == "cpu" && len(fields) > 5 {
				var total float64
				for _, field := range fields[1:min(9, len(fields))] {
					total += parseNumber(field)
				}
				raw["cpu_total"] = total
				raw["cpu_idle"] = parseNumber(fields[4]) + parseNumber(fields[5])
			}
		case "__MEM__":
			if len(fields) >= 2 {
				if fields[0] == "MemTotal:" {
					totalMem = parseNumber(fields[1])
				}
				if fields[0] == "MemAvailable:" {
					availableMem = parseNumber(fields[1])
				}
				if fields[0] == "SwapTotal:" {
					raw["swap_total"] = parseNumber(fields[1])
				}
				if fields[0] == "SwapFree:" {
					raw["swap_free"] = parseNumber(fields[1])
				}
			}
		case "__LOAD__":
			if len(fields) >= 2 {
				values["load"] = parseNumber(fields[1])
			}
		case "__DISK__":
			parseDisk(line, values)
		case "__NET__":
			if strings.Contains(line, ":") && !strings.HasPrefix(strings.TrimSpace(line), "lo:") {
				stats := strings.Fields(strings.SplitN(line, ":", 2)[1])
				if len(stats) >= 16 {
					raw["network_in"] += parseNumber(stats[0])
					raw["network_out"] += parseNumber(stats[8])
					raw["network_drop"] += parseNumber(stats[3]) + parseNumber(stats[11])
				}
			}
		case "__UPTIME__":
			values["uptime"] = parseNumber(fields[0])
		}
	}
	if totalMem > 0 {
		values["memory-host"] = 100 * (1 - availableMem/totalMem)
	}
	values["swap"] = (raw["swap_total"] - raw["swap_free"]) / 1024
	previous := s.previousFor(record.Asset.ID)
	if total, ok := deltaRate(raw, previous, "cpu_total"); ok && total > 0 {
		idle, _ := deltaRate(raw, previous, "cpu_idle")
		values["node-cpu"] = 100 * (1 - idle/total)
	}
	for id, key := range map[string]string{"network-in": "network_in", "network-out": "network_out", "network-drop": "network_drop"} {
		if value, ok := deltaRate(raw, previous, key); ok {
			if id != "network-drop" {
				value /= 1024 * 1024
			}
			values[id] = value
		}
	}
	return values, raw, nil
}
func parseDisk(line string, values map[string]float64) {
	fields := strings.Fields(line)
	if len(fields) >= 6 && strings.HasSuffix(fields[4], "%") {
		values["disk"] = parseNumber(strings.TrimSuffix(fields[4], "%"))
		values["disk-free"] = parseNumber(fields[3]) / 1024 / 1024
	}
}

func (s *Service) collectPostgres(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	a := record.Asset
	port := a.Port
	if port == 0 {
		port = 5432
	}
	database := a.Database
	if database == "" {
		database = "postgres"
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(a.Address, strconv.Itoa(port)), Path: "/" + database, User: url.UserPassword(a.Username, record.Secrets.Password)}
	query := u.Query()
	mode := a.TLSMode
	if mode == "" {
		mode = "verify-full"
	}
	query.Set("sslmode", mode)
	u.RawQuery = query.Encode()
	config, e := pgx.ParseConfig(u.String())
	if e != nil {
		return nil, nil, errors.New("PostgreSQL 연결 정보를 확인하세요")
	}
	dial, closeDial, e := s.dialer(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	defer closeDial()
	// Close the socket when the collection budget ends, even inside the driver.
	config.DialFunc = collectionDial(ctx, dial).DialContext
	config.ConnectTimeout = 8 * time.Second
	config.RuntimeParams["default_transaction_read_only"] = "on"
	config.RuntimeParams["statement_timeout"] = "3000"
	config.RuntimeParams["application_name"] = "pulse-observer"
	connection, e := pgx.ConnectConfig(ctx, config)
	if e != nil {
		return nil, nil, errors.New("PostgreSQL 접속 실패. 주소·계정·TLS·경유 서버를 확인하세요")
	}
	defer connection.Close(context.Background())
	values := map[string]float64{"db-up": 1}
	raw := map[string]float64{}
	start := time.Now()
	var one int
	if e = connection.QueryRow(ctx, "SELECT 1").Scan(&one); e != nil {
		return nil, nil, errors.New("PostgreSQL 읽기 확인 실패")
	}
	values["db-probe"] = float64(time.Since(start).Microseconds()) / 1000
	var connections, commits, rollbacks, hits, reads, deadlocks float64
	e = connection.QueryRow(ctx, `SELECT COALESCE(sum(numbackends),0),COALESCE(sum(xact_commit),0),COALESCE(sum(xact_rollback),0),COALESCE(sum(blks_hit),0),COALESCE(sum(blks_read),0),COALESCE(sum(deadlocks),0) FROM pg_stat_database WHERE datname=$1`, database).Scan(&connections, &commits, &rollbacks, &hits, &reads, &deadlocks)
	if e != nil {
		return values, raw, nil
	}
	values["db-connections"] = connections
	raw["commits"] = commits
	raw["rollbacks"] = rollbacks
	raw["hits"] = hits
	raw["reads"] = reads
	raw["deadlocks"] = deadlocks
	previous := s.previousFor(a.ID)
	cr, ok := deltaRate(raw, previous, "commits")
	rr, ok2 := deltaRate(raw, previous, "rollbacks")
	if ok && ok2 {
		ratio(values, "db-rollback", rr, cr+rr)
		values["db-transactions"] = cr + rr
	}
	hr, hOK := deltaRate(raw, previous, "hits")
	br, bOK := deltaRate(raw, previous, "reads")
	if hOK && bOK {
		ratio(values, "db-buffer", hr, hr+br)
	}
	if dr, ok := deltaRate(raw, previous, "deadlocks"); ok {
		values["db-deadlocks"] = dr
	}
	var locks float64
	if connection.QueryRow(ctx, "SELECT count(*)::float8 FROM pg_locks WHERE mode='AccessExclusiveLock' AND database=(SELECT oid FROM pg_database WHERE datname=$1)", database).Scan(&locks) == nil {
		values["db-locks"] = locks
	}
	var lag *float64
	if connection.QueryRow(ctx, "SELECT CASE WHEN pg_is_in_recovery() THEN EXTRACT(EPOCH FROM now()-pg_last_xact_replay_timestamp())::float8 END").Scan(&lag) == nil && lag != nil {
		values["db-replication"] = *lag
	}
	return values, raw, nil
}
func (s *Service) collectRedis(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	a := record.Asset
	port := a.Port
	if port == 0 {
		port = 6379
	}
	dial, closeDial, e := s.dialer(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	defer closeDial()
	dial = collectionDial(ctx, dial)
	options := &redis.Options{Addr: net.JoinHostPort(a.Address, strconv.Itoa(port)), Username: a.Username, Password: record.Secrets.Password, Protocol: 2, MaxRetries: 0, PoolSize: 1, DialTimeout: 8 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, Dialer: dial}
	if a.TLSMode == "verify-full" {
		options.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			secure := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: a.Address})
			if err = secure.HandshakeContext(ctx); err != nil {
				conn.Close()
				return nil, err
			}
			return secure, nil
		}
	}
	client := redis.NewClient(options)
	defer client.Close()
	started := time.Now()
	if client.Ping(ctx).Err() != nil {
		return nil, nil, errors.New("Redis 접속 실패. 주소·인증·TLS·경유 서버를 확인하세요")
	}
	values := map[string]float64{"redis-up": 1, "redis-probe": float64(time.Since(started).Microseconds()) / 1000}
	info, e := client.Info(ctx).Result()
	if e != nil {
		return nil, nil, errors.New("Redis INFO 읽기 권한이 필요합니다")
	}
	raw := map[string]float64{}
	for _, line := range strings.Split(info, "\n") {
		pair := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(pair) == 2 {
			if v, e := strconv.ParseFloat(pair[1], 64); e == nil {
				raw[pair[0]] = v
			}
		}
	}
	if raw["maxmemory"] > 0 {
		ratio(values, "redis-memory", raw["used_memory"], raw["maxmemory"])
	}
	values["redis-used"] = raw["used_memory"] / 1024 / 1024
	values["redis-blocked"] = raw["blocked_clients"]
	values["redis-clients"] = raw["connected_clients"]
	previous := s.previousFor(a.ID)
	hits, hOK := deltaRate(raw, previous, "keyspace_hits")
	miss, mOK := deltaRate(raw, previous, "keyspace_misses")
	if hOK && mOK {
		ratio(values, "redis-hit", hits, hits+miss)
	}
	for id, key := range map[string]string{"redis-evictions": "evicted_keys", "redis-expired": "expired_keys", "redis-commands": "total_commands_processed"} {
		if v, ok := deltaRate(raw, previous, key); ok {
			values[id] = v
		}
	}
	return values, raw, nil
}
func (s *Service) httpClient(ctx context.Context, a Asset) (*http.Client, func(), error) {
	dial, closeDial, e := s.dialer(ctx, a)
	if e != nil {
		return nil, func() {}, e
	}
	transport := &http.Transport{DialContext: dial, ResponseHeaderTimeout: 5 * time.Second, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxIdleConnsPerHost: 1}
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, func() { transport.CloseIdleConnections(); closeDial() }, nil
}
func (s *Service) collectHTTP(ctx context.Context, record StoredAsset) (map[string]float64, error) {
	client, closeClient, e := s.httpClient(ctx, record.Asset)
	if e != nil {
		return nil, e
	}
	defer closeClient()
	u, e := checkedURL(record.Asset.Address)
	if e != nil {
		return nil, e
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if record.Asset.Username != "" {
		request.SetBasicAuth(record.Asset.Username, record.Secrets.Password)
	}
	start := time.Now()
	response, e := client.Do(request)
	if e != nil {
		return nil, errors.New("HTTP 경로에 연결할 수 없습니다. DNS·TLS·경유 경로를 확인하세요")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	values := map[string]float64{"probe": 0, "probe-latency": float64(time.Since(start).Microseconds()) / 1000, "http-status": float64(response.StatusCode)}
	if response.StatusCode >= 200 && response.StatusCode < 400 {
		values["probe"] = 100
	}
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		expiry := response.TLS.PeerCertificates[0].NotAfter
		for _, cert := range response.TLS.PeerCertificates {
			if cert.NotAfter.Before(expiry) {
				expiry = cert.NotAfter
			}
		}
		values["tls-expiry"] = time.Until(expiry).Hours() / 24
	}
	return values, nil
}

func label(m *dto.Metric, key string) string {
	for _, l := range m.Label {
		if l.GetName() == key {
			return l.GetValue()
		}
	}
	return ""
}
func scalar(m *dto.Metric) float64 {
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	if m.Untyped != nil {
		return m.Untyped.GetValue()
	}
	return 0
}
func (s *Service) collectApplication(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	client, closeClient, e := s.httpClient(ctx, record.Asset)
	if e != nil {
		return nil, nil, e
	}
	defer closeClient()
	endpoint := record.Asset.MetricsURL
	if endpoint == "" {
		u, e := checkedURL(record.Asset.Address)
		if e != nil {
			return nil, nil, e
		}
		u.Path = strings.TrimSuffix(u.Path, "/") + "/metrics"
		endpoint = u.String()
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if record.Asset.Username != "" {
		request.SetBasicAuth(record.Asset.Username, record.Secrets.Password)
	}
	response, e := client.Do(request)
	if e != nil {
		return nil, nil, errors.New("애플리케이션 계측 주소에 연결할 수 없습니다")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, nil, fmt.Errorf("애플리케이션 계측 응답 HTTP %d", response.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(response.Body, 1500001))
	if e != nil || len(data) > 1500000 {
		return nil, nil, errors.New("계측 응답 크기 초과 또는 읽기 실패")
	}
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, e := parser.TextToMetricFamilies(strings.NewReader(string(data)))
	if e != nil {
		return nil, nil, errors.New("계측 응답이 유효한 OpenMetrics/Prometheus 텍스트가 아닙니다")
	}
	values := map[string]float64{}
	raw := map[string]float64{}
	gauge := func(name string) (float64, bool) {
		family, ok := families[name]
		if !ok || len(family.Metric) == 0 {
			return 0, false
		}
		v := scalar(family.Metric[0])
		for _, m := range family.Metric[1:] {
			v = math.Max(v, scalar(m))
		}
		return v, true
	}
	for id, definition := range map[string]struct {
		Name  string
		Scale float64
	}{"cpu": {"app_process_cpu_percent", 1}, "process-cpu": {"app_process_cpu_percent", 1}, "memory": {"process_resident_memory_bytes", 1.0 / (1024 * 1024)}, "gc-floor": {"app_memory_after_gc_bytes", 1.0 / (1024 * 1024)}, "threads": {"app_threads_active", 1}, "inflight": {"app_active_requests", 1}, "disk": {"app_disk_used_ratio", 100}, "disk-free": {"app_disk_free_bytes", 1.0 / (1024 * 1024 * 1024)}, "pool-pending": {"app_db_pool_pending", 1}, "queue": {"app_queue_depth", 1}, "queue-age": {"app_queue_oldest_age_seconds", 1}, "cookie-secure": {"session_cookie_secure", 1}, "cookie-http": {"session_cookie_httponly", 1}, "cookie-samesite": {"session_cookie_samesite_valid", 1}, "loop-p99": {"nodejs_eventloop_lag_p99_seconds", 1000}, "loop-max": {"nodejs_eventloop_lag_max_seconds", 1000}} {
		if v, ok := gauge(definition.Name); ok {
			values[id] = v * definition.Scale
		}
	}
	for id, pair := range map[string][2]string{"pool-active": {"app_db_pool_active", "app_db_pool_max"}, "saturation": {"app_active_requests", "app_request_capacity"}, "file-descriptors": {"process_open_fds", "process_max_fds"}} {
		n, nOK := gauge(pair[0])
		d, dOK := gauge(pair[1])
		if nOK && dOK {
			ratio(values, id, n, d)
		}
	}
	for id, def := range map[string]struct {
		Name    string
		Seconds float64
	}{"cookie-expiry": {"session_cookie_expiry_timestamp_seconds", 60}, "token-expiry": {"service_token_expiry_timestamp_seconds", 3600}} {
		if family := families[def.Name]; family != nil && len(family.Metric) > 0 {
			earliest := math.Inf(1)
			for _, m := range family.Metric {
				earliest = math.Min(earliest, scalar(m))
			}
			values[id] = (earliest - float64(time.Now().Unix())) / def.Seconds
		}
	}
	if v, ok := gauge("app_deployment_timestamp_seconds"); ok {
		values["deployment"] = (float64(time.Now().Unix()) - v) / 60
	}
	if family := families["app_timeout_budget_seconds"]; family != nil {
		for _, m := range family.Metric {
			if layer := label(m, "layer"); layer == "database" {
				values["timeout-db"] = scalar(m)
			} else if layer == "gateway" {
				values["timeout-gateway"] = scalar(m)
			}
		}
	}
	if family := families["http_requests_total"]; family != nil {
		raw["requests"] = 0
		raw["errors"] = 0
		raw["client-errors"] = 0
		for _, m := range family.Metric {
			raw["requests"] += scalar(m)
			status := label(m, "status")
			if strings.HasPrefix(status, "5") {
				raw["errors"] += scalar(m)
			}
			if strings.HasPrefix(status, "4") {
				raw["client-errors"] += scalar(m)
			}
		}
	}
	for name, key := range map[string]string{"app_retry_attempts_total": "retries", "app_request_timeouts_total": "timeouts"} {
		if v, ok := gauge(name); ok {
			raw[key] = v
		}
	}
	if family := families["app_cache_requests_total"]; family != nil {
		raw["cache_hits"] = 0
		raw["cache_total"] = 0
		for _, m := range family.Metric {
			raw["cache_total"] += scalar(m)
			if label(m, "result") == "hit" {
				raw["cache_hits"] += scalar(m)
			}
		}
	}
	if family := families["app_auth_requests_total"]; family != nil {
		raw["auth_failures"] = 0
		raw["auth_total"] = 0
		for _, m := range family.Metric {
			raw["auth_total"] += scalar(m)
			if label(m, "result") == "failure" {
				raw["auth_failures"] += scalar(m)
			}
		}
	}
	for name, family := range families {
		for _, m := range family.Metric {
			if h := m.Histogram; h != nil {
				raw[name+":count"] += float64(h.GetSampleCount())
				raw[name+":sum"] += h.GetSampleSum()
				for _, bucket := range h.Bucket {
					raw[name+":bucket:"+strconv.FormatFloat(bucket.GetUpperBound(), 'g', -1, 64)] += float64(bucket.GetCumulativeCount())
				}
			}
		}
	}
	previous := s.windowSample(record.Asset.ID, 5*time.Minute)
	for _, id := range []string{"requests", "retries", "timeouts"} {
		if v, ok := deltaRate(raw, previous, id); ok {
			values[id] = v
		}
	}
	requests, rOK := deltaRate(raw, previous, "requests")
	for _, id := range []string{"errors", "client-errors"} {
		if v, ok := deltaRate(raw, previous, id); ok && rOK {
			ratio(values, id, v, requests)
		}
	}
	if v, ok := values["errors"]; ok {
		values["availability"] = 100 - v
		values["slo-burn"] = v / 0.1
		hour := s.windowSample(record.Asset.ID, time.Hour)
		n, nOK := deltaRate(raw, hour, "errors")
		d, dOK := deltaRate(raw, hour, "requests")
		if nOK && dOK && d > 0 {
			values["slo-burn-hour"] = 100 * n / d / 0.1
		}
	}
	for id, pair := range map[string][2]string{"cache-hit": {"cache_hits", "cache_total"}, "auth-errors": {"auth_failures", "auth_total"}} {
		n, nOK := deltaRate(raw, previous, pair[0])
		d, dOK := deltaRate(raw, previous, pair[1])
		if nOK && dOK {
			ratio(values, id, n, d)
		}
	}
	for _, quantile := range []float64{.5, .95, .97, .99, .999} {
		if v, ok := histogramQuantile(raw, previous, "http_request_duration_seconds", quantile); ok {
			values["p"+strconv.FormatFloat(quantile*100, 'f', -1, 64)] = v * 1000
		}
	}
	for id, name := range map[string]string{"db-latency": "db_query_duration_seconds", "pool-wait": "db_pool_wait_seconds", "gc-pause": "app_gc_duration_seconds"} {
		if v, ok := histogramQuantile(raw, previous, name, .99); ok {
			values[id] = v * 1000
		}
	}
	count, cOK := deltaRate(raw, previous, "http_request_duration_seconds:count")
	sum, sOK := deltaRate(raw, previous, "http_request_duration_seconds:sum")
	if cOK && sOK && count > 0 {
		values["latency-mean"] = 1000 * sum / count
		values["little-law"] = sum
	}
	if v, ok := deltaRate(raw, previous, "db_query_duration_seconds:count"); ok {
		values["db-rate"] = v
	}
	if v, ok := deltaRate(raw, previous, "app_gc_duration_seconds:count"); ok {
		values["gc-frequency"] = v
	}
	return values, raw, nil
}
func histogramQuantile(raw map[string]float64, previous previousSample, name string, q float64) (float64, bool) {
	count, ok := deltaRate(raw, previous, name+":count")
	if !ok || count <= 0 {
		return 0, false
	}
	type bucket struct{ Bound, Count float64 }
	buckets := []bucket{}
	prefix := name + ":bucket:"
	for key := range raw {
		if strings.HasPrefix(key, prefix) {
			bound, e := strconv.ParseFloat(strings.TrimPrefix(key, prefix), 64)
			value, valid := deltaRate(raw, previous, key)
			if e == nil && valid {
				buckets = append(buckets, bucket{bound, value})
			}
		}
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].Bound < buckets[j].Bound })
	target := q * count
	lastBound, lastCount := 0.0, 0.0
	for _, b := range buckets {
		if b.Count >= target {
			if math.IsInf(b.Bound, 1) {
				return lastBound, true
			}
			if b.Count <= lastCount {
				return b.Bound, true
			}
			return lastBound + (b.Bound-lastBound)*(target-lastCount)/(b.Count-lastCount), true
		}
		lastBound, lastCount = b.Bound, b.Count
	}
	return 0, false
}

func (s *Service) windowSample(id string, window time.Duration) previousSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	threshold := time.Now().Add(-window)
	history := s.history[id]
	for i := len(history) - 1; i >= 0; i-- {
		if !history[i].At.After(threshold) {
			if threshold.Sub(history[i].At) > 45*time.Second {
				return previousSample{}
			}
			for j := i + 1; j < len(history); j++ {
				if history[j].At.Sub(history[j-1].At) > 45*time.Second {
					return previousSample{}
				}
			}
			return history[i]
		}
	}
	return previousSample{}
}
