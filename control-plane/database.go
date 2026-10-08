package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	go_ora "github.com/sijms/go-ora/v2"
)

type databaseDial func(context.Context, string, string) (net.Conn, error)

func (d databaseDial) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d(ctx, network, address)
}

// SSH forwarding channels reject SetReadDeadline/SetWriteDeadline. A local pipe
// supplies those semantics to SQL drivers while all bytes still travel through
// the verified SSH channel. Closing either direction closes the whole bridge.
type databaseTunnel struct {
	net.Conn
	peer     net.Conn
	upstream net.Conn
	once     sync.Once
}

func (c *databaseTunnel) Close() error {
	c.once.Do(func() { c.Conn.Close(); c.peer.Close(); c.upstream.Close() })
	return nil
}
func tunnelDatabaseDial(dial databaseDial) databaseDial {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		upstream, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		local, peer := net.Pipe()
		bridge := &databaseTunnel{Conn: local, peer: peer, upstream: upstream}
		go func() { io.Copy(upstream, peer); bridge.Close() }()
		go func() { io.Copy(peer, upstream); bridge.Close() }()
		return bridge, nil
	}
}

// Keep the absolute collection deadline even if a driver resets socket deadlines.
// The driver may cancel its connect context immediately after dialing; use the
// collection context for the lifetime of the established socket instead.
type collectionConn struct {
	net.Conn
	stop func() bool
}

func (c *collectionConn) Close() error { c.stop(); return c.Conn.Close() }
func collectionDial(ctx context.Context, dial databaseDial) databaseDial {
	return func(connectCtx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(connectCtx, network, address)
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		return &collectionConn{Conn: conn, stop: stop}, nil
	}
}
func dbPool(db *sql.DB) {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
}
func mysqlConfig(record StoredAsset, dial databaseDial) *mysql.Config {
	a := record.Asset
	port := a.Port
	if port == 0 {
		port = 3306
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = a.Username, record.Secrets.Password, a.Database
	cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(a.Address, strconv.Itoa(port))
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 8*time.Second, 3*time.Second, 3*time.Second
	cfg.DialFunc = dial
	// Do not log driver messages containing addresses or authentication details.
	cfg.Logger = log.New(io.Discard, "", 0)
	if a.TLSMode != "disable" {
		cfg.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: a.Address}
	}
	return cfg
}
func oracleDSN(record StoredAsset) (string, error) {
	a := record.Asset
	if a.Database == "" || !validOracleName(a.Database) {
		return "", errors.New("Oracle Service name 또는 SID를 입력하세요")
	}
	port := a.Port
	if port == 0 {
		port = 1521
	}
	options := map[string]string{"CONNECTION TIMEOUT": "8", "TIMEOUT": "3", "SSL VERIFY": "true"}
	service := a.Database
	if a.OracleConnectMode == "sid" {
		options["SID"] = a.Database
		service = ""
	}
	if a.TLSMode != "disable" {
		options["SSL"] = "true"
	}
	return go_ora.BuildUrl(a.Address, port, service, a.Username, record.Secrets.Password, options), nil
}

const mysqlStatusQuery = `SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Questions','Slow_queries','Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_row_lock_current_waits','Innodb_row_lock_waits','Bytes_received','Bytes_sent','Uptime')`
const mysqlVariablesQuery = `SHOW GLOBAL VARIABLES WHERE Variable_name IN ('max_connections')`
const oracleStatsQuery = `SELECT name, value FROM v$sysstat WHERE name IN ('user commits','user rollbacks','execute count','session logical reads','physical reads cache','bytes sent via SQL*Net to client','bytes received via SQL*Net from client')`

// These queries read a bounded set of server statistics, never application rows.
func numericStats(ctx context.Context, db *sql.DB, query string) (map[string]float64, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	rows, err := db.QueryContext(queryCtx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]float64{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			result[key] = n
		}
	}
	return result, rows.Err()
}
func dbProbe(ctx context.Context, db *sql.DB, query string) (map[string]float64, error) {
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.New("DB 접속 실패. 주소·계정·TLS·Service name/SID·경유 경로를 확인하세요")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	var one int
	if err := db.QueryRowContext(queryCtx, query).Scan(&one); err != nil || one != 1 {
		return nil, errors.New("DB 읽기 확인 실패")
	}
	return map[string]float64{"db-up": 1, "db-probe": float64(time.Since(start).Microseconds()) / 1000, "db-monitoring-ready": 1}, nil
}
func (s *Service) collectMySQL(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	dial, closeDial, err := s.dialer(ctx, record.Asset)
	if err != nil {
		return nil, nil, err
	}
	defer closeDial()
	if record.Asset.SSH.JumpID != "" {
		dial = tunnelDatabaseDial(dial)
	}
	connector, err := mysql.NewConnector(mysqlConfig(record, collectionDial(ctx, dial)))
	if err != nil {
		return nil, nil, errors.New("MySQL / MariaDB 연결 정보를 확인하세요")
	}
	db := sql.OpenDB(connector)
	dbPool(db)
	defer db.Close()
	values, err := dbProbe(ctx, db, "SELECT 1")
	if err != nil {
		return nil, nil, err
	}
	raw, err := numericStats(ctx, db, mysqlStatusQuery)
	if err != nil {
		values["db-monitoring-ready"] = 0
		return values, nil, nil
	}
	variables, err := numericStats(ctx, db, mysqlVariablesQuery)
	if err != nil {
		values["db-monitoring-ready"] = 0
	}
	for key, value := range variables {
		raw[key] = value
	}
	for _, key := range []string{"Threads_connected", "Threads_running", "Questions", "max_connections"} {
		if _, ok := raw[key]; !ok {
			values["db-monitoring-ready"] = 0
		}
	}
	applyMySQLStats(values, raw, s.previousFor(record.Asset.ID))
	return values, raw, nil
}
func copyDBStats(values, raw map[string]float64, keys map[string]string) {
	for id, key := range keys {
		if n, ok := raw[key]; ok {
			values[id] = n
		}
	}
}
func dbRate(values, raw map[string]float64, previous previousSample, id, key string) {
	if n, ok := deltaRate(raw, previous, key); ok {
		values[id] = n
	}
}
func applyMySQLStats(values, raw map[string]float64, previous previousSample) {
	copyDBStats(values, raw, map[string]string{"db-connections": "Threads_connected", "db-active": "Threads_running", "db-connection-limit": "max_connections", "db-lock-waiters": "Innodb_row_lock_current_waits", "db-uptime": "Uptime"})
	if current, ok := raw["Threads_connected"]; ok {
		ratio(values, "db-connection-usage", current, raw["max_connections"])
	}
	for id, key := range map[string]string{"db-statements": "Questions", "db-slow-queries": "Slow_queries", "db-lock-waits": "Innodb_row_lock_waits", "db-network-in": "Bytes_received", "db-network-out": "Bytes_sent"} {
		dbRate(values, raw, previous, id, key)
	}
	// This is InnoDB buffer pool effectiveness, not PostgreSQL shared buffers.
	logical, lok := deltaRate(raw, previous, "Innodb_buffer_pool_read_requests")
	physical, pok := deltaRate(raw, previous, "Innodb_buffer_pool_reads")
	if lok && pok && logical > 0 && physical <= logical {
		ratio(values, "mysql-buffer-hit", logical-physical, logical)
	}
}
func (s *Service) collectOracle(ctx context.Context, record StoredAsset) (map[string]float64, map[string]float64, error) {
	dsn, err := oracleDSN(record)
	if err != nil {
		return nil, nil, err
	}
	dial, closeDial, err := s.dialer(ctx, record.Asset)
	if err != nil {
		return nil, nil, err
	}
	defer closeDial()
	if record.Asset.SSH.JumpID != "" {
		dial = tunnelDatabaseDial(dial)
	}
	connector := go_ora.NewConnector(dsn).(*go_ora.OracleConnector)
	connector.Dialer(collectionDial(ctx, dial))
	connector.WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: record.Asset.Address})
	db := sql.OpenDB(connector)
	dbPool(db)
	defer db.Close()
	values, err := dbProbe(ctx, db, "SELECT 1 FROM DUAL")
	if err != nil {
		return nil, nil, err
	}
	raw, err := numericStats(ctx, db, oracleStatsQuery)
	if err != nil {
		raw = map[string]float64{}
		values["db-monitoring-ready"] = 0
	}
	for _, key := range []string{"user commits", "user rollbacks", "execute count"} {
		if _, ok := raw[key]; !ok {
			values["db-monitoring-ready"] = 0
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	var sessions, active, blocked float64
	err = db.QueryRowContext(queryCtx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='ACTIVE' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN blocking_session_status='VALID' THEN 1 ELSE 0 END),0) FROM v$session WHERE type='USER'`).Scan(&sessions, &active, &blocked)
	cancel()
	if err != nil {
		values["db-monitoring-ready"] = 0
	} else {
		values["db-connections"], values["db-active"], values["db-lock-waiters"] = sessions, active, blocked
	}
	queryCtx, cancel = context.WithTimeout(ctx, 3*time.Second)
	var used float64
	var limit string
	err = db.QueryRowContext(queryCtx, `SELECT current_utilization, limit_value FROM v$resource_limit WHERE resource_name='sessions'`).Scan(&used, &limit)
	cancel()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		values["db-monitoring-ready"] = 0
	} else if maximum, parseErr := strconv.ParseFloat(strings.TrimSpace(limit), 64); parseErr == nil && maximum > 0 {
		values["db-connection-limit"] = maximum
		// Oracle's resource limit includes background sessions; do not use the
		// user-session count above as its numerator.
		ratio(values, "db-connection-usage", used, maximum)
	}
	applyOracleStats(values, raw, s.previousFor(record.Asset.ID))
	return values, raw, nil
}
func applyOracleStats(values, raw map[string]float64, previous previousSample) {
	for id, key := range map[string]string{"db-statements": "execute count", "db-network-in": "bytes received via SQL*Net from client", "db-network-out": "bytes sent via SQL*Net to client"} {
		dbRate(values, raw, previous, id, key)
	}
	commits, cok := deltaRate(raw, previous, "user commits")
	rollbacks, rok := deltaRate(raw, previous, "user rollbacks")
	if cok && rok {
		values["db-transactions"] = commits + rollbacks
		ratio(values, "db-rollback", rollbacks, commits+rollbacks)
	}
	logical, lok := deltaRate(raw, previous, "session logical reads")
	physical, pok := deltaRate(raw, previous, "physical reads cache")
	if lok && pok && logical > 0 && physical <= logical {
		ratio(values, "oracle-buffer-hit", logical-physical, logical)
	}
}
