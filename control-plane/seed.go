//go:build testseed

package main

import (
	"context"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// Only the explicitly isolated Compose test environment may register these assets.
func (s *Service) seed(ctx context.Context) error {
	existing, err := s.store.All()
	if err != nil {
		return err
	}
	names := map[string]Asset{}
	for _, record := range existing {
		names[record.Asset.Name] = record.Asset
	}
	add := func(a Asset, secrets Secrets) (Asset, error) {
		if found, ok := names[a.Name]; ok {
			return found, nil
		}
		a.Environment = "Docker test"
		saved, e := s.store.Save(AssetInput{Asset: a, Password: &secrets.Password, SSHPassword: &secrets.SSHPassword, PrivateKey: &secrets.PrivateKey})
		if e != nil {
			return saved, e
		}
		names[a.Name] = saved
		return s.store.Enable(saved.ID, true)
	}
	db, e := add(Asset{Name: "PostgreSQL · test", Kind: "postgres", Address: "postgres", Port: 5432, Username: "pulse_test", Database: "pulse_test", TLSMode: "disable"}, Secrets{Password: "pulse_test_only"})
	if e != nil {
		return e
	}
	if os.Getenv("CONTROL_TEST_DATABASES") == "true" {
		for _, a := range []Asset{
			{Name: "MySQL · test", Kind: "mysql", Address: "mysql", Port: 3306, Username: "pulse_observer", Database: "pulse_test", TLSMode: "disable"},
			{Name: "MariaDB · test", Kind: "mariadb", Address: "mariadb", Port: 3306, Username: "pulse_observer", Database: "pulse_test", TLSMode: "disable"},
			{Name: "Oracle · test", Kind: "oracle", Address: "oracle", Port: 1521, Username: "pulse_observer", Database: "FREEPDB1", OracleConnectMode: "service", TLSMode: "disable"},
		} {
			if _, err := add(a, Secrets{Password: "pulse_test_only"}); err != nil {
				return err
			}
		}
	}
	cache, e := add(Asset{Name: "Redis · test", Kind: "redis", Address: "redis", Port: 6379}, Secrets{})
	if e != nil {
		return e
	}
	addresses, e := net.DefaultResolver.LookupHost(ctx, "demo-api")
	if e != nil {
		return e
	}
	sort.Strings(addresses)
	ids := []string{}
	for _, address := range addresses {
		if net.ParseIP(address).To4() == nil {
			continue
		}
		app, e := add(Asset{Name: "API · " + address, Kind: "application", Address: "http://" + net.JoinHostPort(address, "8080"), Dependencies: []string{db.ID, cache.ID}}, Secrets{})
		if e != nil {
			return e
		}
		ids = append(ids, app.ID)
	}
	if _, e = add(Asset{Name: "Frontend · test", Kind: "http", Address: "http://frontend:8080/health", Dependencies: ids}, Secrets{}); e != nil {
		return e
	}
	for attempt := 0; attempt < 20; attempt++ {
		_, first := os.Stat("/test-ssh/bastion.fingerprint")
		_, second := os.Stat("/test-ssh/node.fingerprint")
		if first == nil && second == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	pin, e := os.ReadFile("/test-ssh/bastion.fingerprint")
	if e != nil {
		return e
	}
	bastion, e := add(Asset{Name: "SSH bastion · test", Kind: "server", Address: "test-bastion", OS: "linux", SSH: SSHConfig{Username: "pulse", Fingerprint: strings.TrimSpace(string(pin))}}, Secrets{SSHPassword: "pulse_isolated_test_only"})
	if e != nil {
		return e
	}
	pin, e = os.ReadFile("/test-ssh/node.fingerprint")
	if e != nil {
		return e
	}
	key, e := os.ReadFile("/test-ssh/client_key")
	if e != nil {
		return e
	}
	_, e = add(Asset{Name: "SSH node · test", Kind: "server", Address: "test-node", OS: "linux", SSH: SSHConfig{Username: "pulse", JumpID: bastion.ID, Fingerprint: strings.TrimSpace(string(pin))}}, Secrets{PrivateKey: string(key)})
	return e
}
