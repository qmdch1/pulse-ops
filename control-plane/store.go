package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	aead cipher.AEAD
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func openStore(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	var key []byte
	var e error
	if raw := os.Getenv("CONTROL_MASTER_KEY"); raw != "" {
		key, e = base64.StdEncoding.DecodeString(raw)
	} else {
		key, e = os.ReadFile(filepath.Join(dir, "master.key"))
		if os.IsNotExist(e) {
			key = make([]byte, 32)
			_, e = rand.Read(key)
			if e == nil {
				e = os.WriteFile(filepath.Join(dir, "master.key"), key, 0600)
			}
		}
	}
	if e != nil || len(key) != 32 {
		return nil, errors.New("master key must contain 32 bytes")
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.Join(dir, "pulse.db"))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS assets (id TEXT NOT NULL,payload BLOB NOT NULL);
 CREATE INDEX IF NOT EXISTS assets_id ON assets(id);
 CREATE TABLE IF NOT EXISTS observations (asset_id TEXT NOT NULL, observed_at INTEGER NOT NULL,payload TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS observation_asset_time ON observations(asset_id,observed_at);
 CREATE INDEX IF NOT EXISTS observation_time ON observations(observed_at);
 CREATE TABLE IF NOT EXISTS audit (asset_id TEXT,action TEXT,status TEXT,at TEXT);
 CREATE INDEX IF NOT EXISTS audit_time ON audit(at);
 CREATE TABLE IF NOT EXISTS integrations (id TEXT PRIMARY KEY,payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS operations (kind TEXT NOT NULL,id TEXT NOT NULL,at INTEGER NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(kind,id));
 CREATE INDEX IF NOT EXISTS operations_time ON operations(kind,at);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	_ = os.Chmod(filepath.Join(dir, "pulse.db"), 0600)
	return &Store{db: db, aead: aead}, nil
}
func (s *Store) seal(id string, v StoredAsset) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, raw, []byte(id)), nil
}
func (s *Store) allLocked() (map[string]StoredAsset, error) {
	rows, e := s.db.Query("SELECT id,payload FROM assets")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	all := map[string]StoredAsset{}
	for rows.Next() {
		var id string
		var b []byte
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		n := s.aead.NonceSize()
		if len(b) < n {
			return nil, errors.New("encrypted asset corrupt")
		}
		raw, e := s.aead.Open(nil, b[:n], b[n:], []byte(id))
		if e != nil {
			return nil, e
		}
		var record StoredAsset
		if e = json.Unmarshal(raw, &record); e != nil {
			return nil, e
		}
		all[id] = record
	}
	return all, rows.Err()
}
func (s *Store) All() (map[string]StoredAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allLocked()
}
func (s *Store) Get(id string) (StoredAsset, error) {
	all, e := s.All()
	if e != nil {
		return StoredAsset{}, e
	}
	a, ok := all[id]
	if !ok {
		return a, errors.New("등록된 인프라가 없습니다")
	}
	return a, nil
}
func (s *Store) List() ([]Asset, error) {
	all, e := s.All()
	if e != nil {
		return nil, e
	}
	out := []Asset{}
	for _, record := range all {
		out = append(out, publicAsset(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt < out[j].CreatedAt || out[i].CreatedAt == out[j].CreatedAt && out[i].ID < out[j].ID
	})
	return out, nil
}
func (s *Store) Save(input AssetInput) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.allLocked()
	if e != nil {
		return Asset{}, e
	}
	a := input.Asset
	secret := Secrets{}
	if a.ID == "" {
		if len(all) >= 200 {
			return Asset{}, errors.New("인프라는 최대 200개까지 등록할 수 있습니다")
		}
		a.ID = ID()
		a.CreatedAt = nowString()
		a.Enabled = false
		a.Version = 1
		a.Status = "draft"
	} else {
		old, ok := all[a.ID]
		if !ok {
			return Asset{}, errors.New("등록된 인프라가 없습니다")
		}
		if a.Version != old.Asset.Version {
			return Asset{}, errors.New("다른 변경이 저장되었습니다. 새로고침 후 다시 저장하세요")
		}
		secret = old.Secrets
		a.CreatedAt = old.Asset.CreatedAt
		a.Version++
		a.Enabled = false
		a.Status = "draft"
	}
	a.UpdatedAt = nowString()
	a.LastSeen = ""
	a.Message = "연결 대기"
	if a.Name == "" {
		a.Name = "이름 없는 인프라"
	}
	if a.Environment == "" {
		a.Environment = "미지정"
	}
	if input.Password != nil {
		secret.Password = *input.Password
	}
	if input.SSHPassword != nil {
		secret.SSHPassword = *input.SSHPassword
	}
	if input.PrivateKey != nil {
		secret.PrivateKey = *input.PrivateKey
	}
	if input.Passphrase != nil {
		secret.Passphrase = *input.Passphrase
	}
	if len(secret.PrivateKey) > 128*1024 || len(secret.Password) > 8192 || len(secret.SSHPassword) > 8192 || len(secret.Passphrase) > 8192 {
		return Asset{}, errors.New("인증 정보가 허용 크기를 초과했습니다")
	}
	if e = validateAsset(a, all); e != nil {
		return Asset{}, e
	}
	entry := StoredAsset{Asset: a, Secrets: secret}
	b, e := s.seal(a.ID, entry)
	if e != nil {
		return Asset{}, e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return Asset{}, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM assets WHERE id=?", a.ID); e != nil {
		return Asset{}, e
	}
	if _, e = tx.Exec("INSERT INTO assets(id,payload) VALUES(?,?)", a.ID, b); e != nil {
		return Asset{}, e
	}
	if _, e = tx.Exec("INSERT INTO audit VALUES(?,?,?,?)", a.ID, "asset.save", "ok", nowString()); e != nil {
		return Asset{}, e
	}
	if e = tx.Commit(); e != nil {
		return Asset{}, e
	}
	return publicAsset(entry), nil
}
func (s *Store) SetState(id string, version int, enabled bool, status, message string, values map[string]float64) (Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.allLocked()
	if e != nil {
		return Asset{}, e
	}
	record, ok := all[id]
	if !ok || record.Asset.Version != version {
		return Asset{}, errors.New("인프라 설정이 변경되었습니다")
	}
	record.Asset.Enabled = enabled
	record.Asset.Status = status
	record.Asset.Message = message
	if values != nil {
		record.Asset.LastSeen = nowString()
	}
	b, e := s.seal(id, record)
	if e != nil {
		return Asset{}, e
	}
	if _, e = s.db.Exec("UPDATE assets SET payload=? WHERE id=?", b, id); e != nil {
		return Asset{}, e
	}
	return publicAsset(record), nil
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.allLocked()
	if e != nil {
		return e
	}
	if _, ok := all[id]; !ok {
		return errors.New("등록된 인프라가 없습니다")
	}
	for otherID, record := range all {
		if otherID == id {
			continue
		}
		if record.Asset.SSH.JumpID == id {
			return errors.New("다른 인프라의 점프 호스트로 사용 중입니다")
		}
		for _, dep := range record.Asset.Dependencies {
			if dep == id {
				return errors.New("연결 관계에서 사용 중입니다. 관계를 먼저 해제하세요")
			}
		}
	}
	_, e = s.db.Exec("DELETE FROM assets WHERE id=?", id)
	return e
}
func (s *Store) Observe(o Observation) error {
	b, e := json.Marshal(o)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO observations VALUES(?,?,?)", o.AssetID, o.Time, string(b))
	return e
}
func (s *Store) Observations(ids []string, start int64, step int64) ([]Observation, error) {
	result := []Observation{}
	for _, id := range ids {
		rows, e := s.db.Query("SELECT payload FROM observations WHERE rowid IN (SELECT max(rowid) FROM observations WHERE asset_id=? AND observed_at>=? GROUP BY CAST(observed_at/? AS INTEGER)) ORDER BY observed_at DESC LIMIT 7000", id, start, step)
		if e != nil {
			return nil, e
		}
		seen := map[int64]bool{}
		for rows.Next() {
			var b string
			if e = rows.Scan(&b); e != nil {
				rows.Close()
				return nil, e
			}
			var o Observation
			if json.Unmarshal([]byte(b), &o) != nil {
				continue
			}
			bucket := o.Time / step
			if !seen[bucket] {
				seen[bucket] = true
				result = append(result, o)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result, nil
}
func (s *Store) Record(id, action, status string) {
	_, _ = s.db.Exec("INSERT INTO audit VALUES(?,?,?,?)", id, action, status, nowString())
}
func (s *Store) Audit() []Audit {
	out := []Audit{}
	rows, e := s.db.Query("SELECT asset_id,action,status,at FROM audit ORDER BY at DESC LIMIT 100")
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a Audit
		if rows.Scan(&a.AssetID, &a.Action, &a.Status, &a.At) == nil {
			out = append(out, a)
		}
	}
	return out
}
func (s *Store) Purge() {
	_, _ = s.db.Exec("DELETE FROM observations WHERE observed_at < ?", time.Now().Add(-15*24*time.Hour).Unix())
	_, _ = s.db.Exec("DELETE FROM audit WHERE at < ?", time.Now().Add(-90*24*time.Hour).UTC().Format(time.RFC3339))
	_, _ = s.db.Exec("DELETE FROM operations WHERE kind IN ('event','delivery') AND at < ?", time.Now().Add(-90*24*time.Hour).Unix())
}
