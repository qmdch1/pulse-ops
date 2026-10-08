package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

type SSHConnection struct {
	clients []*ssh.Client
	once    sync.Once
}

func (c *SSHConnection) Close() {
	c.once.Do(func() {
		for i := len(c.clients) - 1; i >= 0; i-- {
			_ = c.clients[i].Close()
		}
	})
}
func (c *SSHConnection) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	return c.clients[len(c.clients)-1].DialContext(ctx, network, address)
}
func (c *SSHConnection) Session() (*ssh.Session, error) {
	return c.clients[len(c.clients)-1].NewSession()
}
func sshEndpoint(a Asset) (string, int, string) {
	host := a.SSH.Host
	if host == "" && a.Kind == "server" {
		host = a.Address
	}
	port := a.SSH.Port
	if port == 0 && a.Kind == "server" {
		port = a.Port
	}
	if port == 0 {
		port = 22
	}
	return host, port, a.SSH.Username
}
func (s *Service) sshConnect(ctx context.Context, id string, probe bool) (*SSHConnection, string, error) {
	all, e := s.store.All()
	if e != nil {
		return nil, "", e
	}
	var chain []StoredAsset
	visited := map[string]bool{}
	current := id
	for current != "" {
		if len(chain) >= 8 || visited[current] {
			return nil, "", errors.New("점프 호스트 경로가 유효하지 않습니다")
		}
		visited[current] = true
		record, ok := all[current]
		if !ok {
			return nil, "", errors.New("점프 호스트가 등록되어 있지 않습니다")
		}
		chain = append(chain, record)
		current = record.Asset.SSH.JumpID
	}
	out := &SSHConnection{}
	fail := func(err error) (*SSHConnection, string, error) { out.Close(); return nil, "", err }
	for i := len(chain) - 1; i >= 0; i-- {
		record := chain[i]
		host, port, user := sshEndpoint(record.Asset)
		if host == "" {
			return fail(errors.New("SSH 주소가 비어 있습니다. 등록 정보를 보완하세요"))
		}
		if user == "" && !probe {
			return fail(errors.New("SSH username이 필요합니다"))
		}
		probing := probe && i == 0
		var auth []ssh.AuthMethod
		if !probing {
			if record.Secrets.PrivateKey != "" {
				var signer ssh.Signer
				if record.Secrets.Passphrase != "" {
					signer, e = ssh.ParsePrivateKeyWithPassphrase([]byte(record.Secrets.PrivateKey), []byte(record.Secrets.Passphrase))
				} else {
					signer, e = ssh.ParsePrivateKey([]byte(record.Secrets.PrivateKey))
				}
				if e != nil {
					return fail(errors.New("PEM 키 또는 passphrase를 확인하세요"))
				}
				auth = append(auth, ssh.PublicKeys(signer))
			}
			if record.Secrets.SSHPassword != "" {
				auth = append(auth, ssh.Password(record.Secrets.SSHPassword))
			}
		}
		var found string
		callback := func(_ string, remote net.Addr, key ssh.PublicKey) error {
			found = ssh.FingerprintSHA256(key)
			if probing {
				return errors.New("fingerprint inspection complete")
			}
			pin := record.Asset.SSH.Fingerprint
			if pin != "" {
				if subtle.ConstantTimeCompare([]byte(pin), []byte(found)) != 1 {
					return errors.New("서버 지문이 변경되었습니다. 연결을 차단했습니다")
				}
				return nil
			}
			if path := os.Getenv("CONTROL_KNOWN_HOSTS"); path != "" {
				verify, err := knownhosts.New(path)
				if err != nil {
					return errors.New("known_hosts 파일을 읽을 수 없습니다")
				}
				return verify(net.JoinHostPort(host, strconv.Itoa(port)), remote, key)
			}
			return errors.New("서버 지문 확인이 필요합니다. SSH 지문을 확인한 후 저장하세요")
		}
		hopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		address := net.JoinHostPort(host, strconv.Itoa(port))
		var raw net.Conn
		if len(out.clients) == 0 {
			raw, e = (&net.Dialer{}).DialContext(hopCtx, "tcp", address)
		} else {
			raw, e = out.Dial(hopCtx, "tcp", address)
		}
		if e != nil {
			cancel()
			return fail(fmt.Errorf("%s: SSH 주소·포트·경유 경로에 연결할 수 없습니다", record.Asset.Name))
		}
		deadline, _ := hopCtx.Deadline()
		_ = raw.SetDeadline(deadline)
		stop := context.AfterFunc(hopCtx, func() { raw.Close() })
		cc, ch, req, err := ssh.NewClientConn(raw, address, &ssh.ClientConfig{User: user, Auth: auth, HostKeyCallback: callback, Timeout: 10 * time.Second})
		stopped := stop()
		cancel()
		if probing && found != "" {
			raw.Close()
			out.Close()
			return nil, found, nil
		}
		if err != nil || !stopped {
			raw.Close()
			if found != "" && record.Asset.SSH.Fingerprint == "" {
				return fail(errors.New("서버 지문을 먼저 확인하고 저장하세요"))
			}
			if found != "" && record.Asset.SSH.Fingerprint != found {
				return fail(errors.New("서버 지문이 등록 값과 일치하지 않아 차단했습니다"))
			}
			return fail(fmt.Errorf("%s: SSH 인증 또는 연결 실패. username·password·PEM을 확인하세요", record.Asset.Name))
		}
		_ = raw.SetDeadline(time.Time{})
		out.clients = append(out.clients, ssh.NewClient(cc, ch, req))
	}
	return out, "", nil
}
func (s *Service) dialer(ctx context.Context, a Asset) (func(context.Context, string, string) (net.Conn, error), func(), error) {
	if a.SSH.JumpID == "" {
		return (&net.Dialer{Timeout: 8 * time.Second}).DialContext, func() {}, nil
	}
	conn, _, e := s.sshConnect(ctx, a.SSH.JumpID, false)
	if e != nil {
		return nil, func() {}, e
	}
	return conn.Dial, conn.Close, nil
}
func readCommand(ctx context.Context, conn *SSHConnection, command string) (string, error) {
	session, e := conn.Session()
	if e != nil {
		return "", e
	}
	defer session.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	var output limitedBuffer
	output.limit = 256 * 1024
	session.Stdout = &output
	session.Stderr = &limitedBuffer{limit: 1024}
	e = session.Run(command)
	if output.overflow {
		return "", errors.New("수집 결과가 크기 한도를 초과했습니다")
	}
	return string(output.data), e
}

type limitedBuffer struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - len(b.data)
	if n > remaining {
		b.overflow = true
		p = p[:max(0, remaining)]
	}
	b.data = append(b.data, p...)
	return n, nil
}
