package main

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDatabaseDraftsAndConnectionConfig(t *testing.T) {
	store := testStore(t)
	for _, kind := range []string{"mysql", "mariadb", "oracle"} {
		a, err := store.Save(AssetInput{Asset: Asset{Kind: kind}})
		if err != nil || a.Enabled || a.Status != "draft" {
			t.Fatalf("blank %s draft: %v", kind, err)
		}
	}
	record := StoredAsset{Asset: Asset{Kind: "oracle", Address: "db.example", Database: "service.example", Username: "observer"}, Secrets: Secrets{Password: "p@ss:/?#&with space"}}
	dsn, err := oracleDSN(record)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if password != record.Secrets.Password || u.Query().Get("SSL") != "true" || u.Query().Get("SSL VERIFY") != "true" || u.Path != "/service.example" {
		t.Fatal("Oracle DSN failed escaping or TLS default")
	}
	record.Asset.OracleConnectMode = "sid"
	dsn, _ = oracleDSN(record)
	u, _ = url.Parse(dsn)
	if u.Query().Get("SID") != record.Asset.Database || u.Path != "/" {
		t.Fatal("SID not encoded separately")
	}
	record.Asset.Database = "FREE)(SERVER=evil"
	if validateAsset(record.Asset, nil) == nil {
		t.Fatal("Oracle descriptor injection accepted")
	}
	record.Asset.Database = ""
	if _, err := oracleDSN(record); err == nil {
		t.Fatal("Oracle connection requires service/SID, draft does not")
	}
	record.Asset = Asset{Kind: "mysql", Address: "db.example", Database: "name/with?characters", Username: "observer"}
	cfg := mysqlConfig(record, nil)
	if cfg.TLS == nil || cfg.TLS.InsecureSkipVerify || cfg.TLS.ServerName != "db.example" || cfg.Addr != "db.example:3306" || cfg.Passwd != record.Secrets.Password || cfg.DBName != record.Asset.Database {
		t.Fatal("MySQL config lost TLS or escaped credentials")
	}
	record.Asset.TLSMode = "disable"
	if mysqlConfig(record, nil).TLS != nil {
		t.Fatal("explicit plaintext ignored")
	}
}
func TestDatabaseCounterSemantics(t *testing.T) {
	previous := previousSample{At: time.Now().Add(-10 * time.Second), Values: map[string]float64{"Questions": 100, "Slow_queries": 4, "Innodb_buffer_pool_read_requests": 1000, "Innodb_buffer_pool_reads": 20}}
	raw := map[string]float64{"Questions": 150, "Slow_queries": 5, "Innodb_buffer_pool_read_requests": 1200, "Innodb_buffer_pool_reads": 30, "Threads_connected": 8, "max_connections": 10}
	values := map[string]float64{}
	applyMySQLStats(values, raw, previous)
	if math.Abs(values["mysql-buffer-hit"]-95) > 0.001 || values["db-connection-usage"] != 80 || math.Abs(values["db-statements"]-5) > 0.02 {
		t.Fatalf("bad rate/ratio: %v", values)
	}
	if _, ok := values["db-transactions"]; ok {
		t.Fatal("Questions falsely presented as transactions")
	}
	if _, ok := values["db-active"]; ok {
		t.Fatal("missing status became zero")
	}
	raw["Questions"] = 1
	raw["Innodb_buffer_pool_reads"] = 1
	values = map[string]float64{}
	applyMySQLStats(values, raw, previous)
	if _, ok := values["db-statements"]; ok {
		t.Fatal("counter reset created rate")
	}
	if _, ok := values["mysql-buffer-hit"]; ok {
		t.Fatal("counter reset created hit ratio")
	}
	values = map[string]float64{}
	applyOracleStats(values, map[string]float64{"execute count": 42}, previousSample{})
	if len(values) != 0 {
		t.Fatal("first or missing Oracle sample fabricated measurements")
	}
	previous = previousSample{At: time.Now().Add(-10 * time.Second), Values: map[string]float64{"user commits": 20, "user rollbacks": 2, "session logical reads": 200, "physical reads cache": 20}}
	values = map[string]float64{}
	applyOracleStats(values, map[string]float64{"user commits": 38, "user rollbacks": 4, "session logical reads": 400, "physical reads cache": 30}, previous)
	if math.Abs(values["db-rollback"]-10) > 0.01 || math.Abs(values["oracle-buffer-hit"]-95) > 0.01 {
		t.Fatalf("Oracle delta formulas: %v", values)
	}
}
func TestDatabaseCollectionCancelsSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	left, right := net.Pipe()
	defer right.Close()
	dial := collectionDial(ctx, func(context.Context, string, string) (net.Conn, error) { return left, nil })
	conn, err := dial(context.Background(), "tcp", "unused")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancel()
	_ = right.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = right.Read(make([]byte, 1)); err == nil {
		t.Fatal("cancelled collection left socket open")
	}
}
func TestDatabaseTunnelDeadline(t *testing.T) {
	upstream, server := net.Pipe()
	defer server.Close()
	dial := tunnelDial(func(context.Context, string, string) (net.Conn, error) { return upstream, nil })
	conn, err := dial(context.Background(), "tcp", "unused")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Read(make([]byte, 1))
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("SSH bridge did not enforce deadline: %v", err)
	}
	conn.Close()
	_ = server.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = server.Read(make([]byte, 1)); err == nil {
		t.Fatal("upstream left open after tunnel close")
	}
}

// Run only inside the explicitly provisioned local database test network.
func TestIntegrationDatabaseEngines(t *testing.T) {
	if os.Getenv("PULSE_TEST_DATABASES") != "true" {
		t.Skip("isolated database Compose only")
	}
	store := testStore(t)
	s := newService(store, nil)
	pin, err := os.ReadFile("/test-ssh/bastion.fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	sshPassword := "pulse_isolated_test_only"
	jump, err := store.Save(AssetInput{Asset: Asset{Kind: "server", Address: "test-bastion", SSH: SSHConfig{Username: "pulse", Fingerprint: strings.TrimSpace(string(pin))}}, SSHPassword: &sshPassword})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"mysql", "mariadb", "oracle"} {
		t.Run(kind, func(t *testing.T) {
			record := StoredAsset{Asset: Asset{ID: kind, Kind: kind, Address: kind, Username: "pulse_observer", Database: "pulse_test", TLSMode: "disable"}, Secrets: Secrets{Password: "pulse_test_only"}}
			if kind == "oracle" {
				record.Asset.Database = "FREEPDB1"
			}
			collect := s.collectMySQL
			if kind == "oracle" {
				collect = s.collectOracle
			}
			for _, viaJump := range []bool{false, true} {
				if viaJump {
					record.Asset.SSH.JumpID = jump.ID
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				values, raw, err := collect(ctx, record)
				cancel()
				if err != nil {
					t.Fatalf("jump=%v: %v", viaJump, err)
				}
				if values["db-up"] != 1 || values["db-monitoring-ready"] != 1 || values["db-connections"] < 1 || values["db-probe"] < 0 {
					t.Fatalf("jump=%v unexpected values %v raw keys %d", viaJump, values, len(raw))
				}
				s.previous[kind] = previousSample{At: time.Now().Add(-15 * time.Second), Values: raw}
				t.Logf("%s jump=%v: connected, statistics=%d", kind, viaJump, len(values))
			}
			record.Secrets.Password = "wrong_password_never_log"
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, _, err = collect(ctx, record)
			cancel()
			if err == nil || strings.Contains(err.Error(), record.Secrets.Password) {
				t.Fatal("bad credentials accepted or disclosed")
			}
			record.Secrets.Password = "pulse_test_only"
			record.Asset.SSH.JumpID = ""
			record.Asset.TLSMode = "verify-full"
			ctx, cancel = context.WithTimeout(context.Background(), 12*time.Second)
			_, _, err = collect(ctx, record)
			cancel()
			if err == nil {
				t.Fatal("untrusted/plaintext test database passed verified TLS")
			}
		})
	}
	t.Run("oracle-sid", func(t *testing.T) {
		record := StoredAsset{Asset: Asset{ID: "oracle-sid", Kind: "oracle", Address: "oracle", Username: "system", Database: "FREE", OracleConnectMode: "sid", TLSMode: "disable"}, Secrets: Secrets{Password: "Pulse_isolated_root_2026"}}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		values, _, err := s.collectOracle(ctx, record)
		if err != nil || values["db-up"] != 1 {
			t.Fatalf("SID connection: %v", err)
		}
	})
	t.Run("oracle-limited-permissions", func(t *testing.T) {
		record := StoredAsset{Asset: Asset{ID: "oracle-limited", Kind: "oracle", Address: "oracle", Username: "pulse_basic", Database: "FREEPDB1", TLSMode: "disable"}, Secrets: Secrets{Password: "pulse_test_only"}}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		values, _, err := s.collectOracle(ctx, record)
		if err != nil || values["db-up"] != 1 || values["db-monitoring-ready"] != 0 {
			t.Fatalf("partial permission: values %v, err %v", values, err)
		}
		if _, ok := values["db-connections"]; ok {
			t.Fatal("missing permission fabricated session count")
		}
		encoded, _ := json.Marshal(values)
		t.Logf("limited account: %s", encoded)
	})
}
