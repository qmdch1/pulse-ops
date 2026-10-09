package main

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

type SSHConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	JumpID      string `json:"jumpId"`
	Fingerprint string `json:"fingerprint"`
}
type Asset struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Kind              string    `json:"kind"`
	Address           string    `json:"address"`
	Port              int       `json:"port"`
	Username          string    `json:"username"`
	Database          string    `json:"database"`
	OracleConnectMode string    `json:"oracleConnectMode"`
	TLSMode           string    `json:"tlsMode"`
	OS                string    `json:"os"`
	MetricsURL        string    `json:"metricsUrl"`
	Environment       string    `json:"environment"`
	Tags              []string  `json:"tags"`
	SSH               SSHConfig `json:"ssh"`
	Dependencies      []string  `json:"dependencies"`
	Enabled           bool      `json:"enabled"`
	Version           int       `json:"version"`
	CreatedAt         string    `json:"createdAt"`
	UpdatedAt         string    `json:"updatedAt"`
	HasPassword       bool      `json:"hasPassword"`
	HasSSHPassword    bool      `json:"hasSshPassword"`
	HasPrivateKey     bool      `json:"hasPrivateKey"`
	HasPassphrase     bool      `json:"hasPassphrase"`
	Status            string    `json:"status"`
	LastSeen          string    `json:"lastSeen"`
	Message           string    `json:"message"`
}
type Secrets struct {
	Password    string `json:"password"`
	SSHPassword string `json:"sshPassword"`
	PrivateKey  string `json:"privateKey"`
	Passphrase  string `json:"passphrase"`
}
type StoredAsset struct {
	Asset   Asset   `json:"asset"`
	Secrets Secrets `json:"secrets"`
}
type AssetInput struct {
	Asset
	Password    *string `json:"password"`
	SSHPassword *string `json:"sshPassword"`
	PrivateKey  *string `json:"privateKey"`
	Passphrase  *string `json:"passphrase"`
}
type Observation struct {
	AssetID string             `json:"assetId"`
	Time    int64              `json:"time"`
	Values  map[string]float64 `json:"values"`
	Error   string             `json:"error,omitempty"`
}
type Audit struct {
	AssetID string `json:"assetId"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	At      string `json:"at"`
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339) }
func publicAsset(s StoredAsset) Asset {
	a := s.Asset
	a.HasPassword = s.Secrets.Password != ""
	a.HasSSHPassword = s.Secrets.SSHPassword != ""
	a.HasPrivateKey = s.Secrets.PrivateKey != ""
	a.HasPassphrase = s.Secrets.Passphrase != ""
	if a.Dependencies == nil {
		a.Dependencies = []string{}
	}
	if a.Tags == nil {
		a.Tags = []string{}
	}
	return a
}
func validHost(host string) bool {
	if host == "" {
		return true
	}
	if len(host) > 253 || strings.ContainsAny(host, " \r\n\t/\\@?#%") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, r := range host {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func validateAsset(a Asset, all map[string]StoredAsset) error {
	if err := validateLabels(a.Tags, 20); err != nil {
		return err
	}
	if a.Kind != "server" && a.Kind != "application" && !isSQLDatabase(a.Kind) && a.Kind != "redis" && a.Kind != "http" {
		return errors.New("지원하는 인프라 종류를 선택하세요")
	}
	if len(a.Name) > 160 || len(a.Username) > 128 || len(a.SSH.Username) > 128 || len(a.Database) > 128 || len(a.Environment) > 40 || strings.ContainsAny(a.Name+a.Username+a.Database+a.SSH.Username, "\x00\r\n") {
		return errors.New("입력값의 길이 또는 문자를 확인하세요")
	}
	if a.Port < 0 || a.Port > 65535 || a.SSH.Port < 0 || a.SSH.Port > 65535 {
		return errors.New("포트는 비워두거나 1–65535로 입력하세요")
	}
	if !validHost(a.SSH.Host) {
		return errors.New("SSH 주소 형식을 확인하세요")
	}
	if a.Address != "" {
		if a.Kind == "application" || a.Kind == "http" {
			if _, err := checkedURL(a.Address); err != nil {
				return err
			}
		} else if !validHost(a.Address) {
			return errors.New("서버 주소 형식을 확인하세요")
		}
	}
	if a.MetricsURL != "" {
		if _, err := checkedURL(a.MetricsURL); err != nil {
			return err
		}
	}
	if a.TLSMode != "" && a.TLSMode != "disable" && a.TLSMode != "verify-full" {
		return errors.New("TLS 설정을 확인하세요")
	}
	if a.OracleConnectMode != "" && a.OracleConnectMode != "service" && a.OracleConnectMode != "sid" {
		return errors.New("Oracle 연결 방식은 Service name 또는 SID를 선택하세요")
	}
	if a.Kind == "oracle" && a.Database != "" && !validOracleName(a.Database) {
		return errors.New("Oracle Service name / SID에는 영문·숫자·점·밑줄·하이픈만 입력하세요")
	}
	if a.OS != "" && a.OS != "linux" && a.OS != "macos" && a.OS != "windows" {
		return errors.New("운영체제를 확인하세요")
	}
	if a.SSH.Fingerprint != "" && (!strings.HasPrefix(a.SSH.Fingerprint, "SHA256:") || len(a.SSH.Fingerprint) > 100) {
		return errors.New("SHA256 서버 지문을 입력하세요")
	}
	for _, id := range a.Dependencies {
		if _, ok := all[id]; !ok || id == a.ID {
			return errors.New("연결 관계의 인프라를 확인하세요")
		}
	}
	visited := map[string]bool{a.ID: true}
	jump := a.SSH.JumpID
	for depth := 0; jump != ""; depth++ {
		if visited[jump] || depth >= 7 {
			return errors.New("점프 호스트 순환 또는 경유 한도 초과입니다")
		}
		visited[jump] = true
		next, ok := all[jump]
		if !ok || next.Asset.Kind != "server" {
			return errors.New("점프 호스트로 등록된 서버를 선택하세요")
		}
		jump = next.Asset.SSH.JumpID
	}
	return nil
}
func isSQLDatabase(kind string) bool {
	return kind == "postgres" || kind == "mysql" || kind == "mariadb" || kind == "oracle"
}
func validOracleName(name string) bool {
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func checkedURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 2048 || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return nil, errors.New("http:// 또는 https:// 주소를 입력하세요. 인증 정보는 별도 필드를 사용하세요")
	}
	return u, nil
}
