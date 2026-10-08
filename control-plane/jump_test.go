package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startForwardingSSH runs an in-process SSH server that only accepts
// direct-tcpip channels, like a bastion used as a jump host.
func startForwardingSSH(t *testing.T) (int, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() == "pulse" && string(password) == "jump-test-only" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					raw.Close()
					return
				}
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					var target struct {
						Host       string
						Port       uint32
						OriginHost string
						OriginPort uint32
					}
					if incoming.ChannelType() != "direct-tcpip" || ssh.Unmarshal(incoming.ExtraData(), &target) != nil {
						incoming.Reject(ssh.UnknownChannelType, "forwarding only")
						continue
					}
					upstream, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
					if err != nil {
						incoming.Reject(ssh.ConnectionFailed, "unreachable")
						continue
					}
					channel, reqs, err := incoming.Accept()
					if err != nil {
						upstream.Close()
						continue
					}
					go ssh.DiscardRequests(reqs)
					go func() { io.Copy(channel, upstream); channel.Close() }()
					go func() { io.Copy(upstream, channel); upstream.Close() }()
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, ssh.FingerprintSHA256(signer.PublicKey())
}

func registerTestJump(t *testing.T, s *Service) string {
	t.Helper()
	port, fingerprint := startForwardingSSH(t)
	password := "jump-test-only"
	jump, err := s.store.Save(AssetInput{Asset: Asset{Name: "jump", Kind: "server", Address: "127.0.0.1", Port: port, SSH: SSHConfig{Username: "pulse", Fingerprint: fingerprint}}, SSHPassword: &password})
	if err != nil {
		t.Fatal(err)
	}
	return jump.ID
}

func listenTCP(t *testing.T, serve func(net.Conn)) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); serve(conn) }()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port
}

// Every collector reached through a jump host must get working socket deadlines
// and context cancellation, not only the SQL engines that wrapped it themselves.
func TestJumpDialerSupportsDeadlinesAndCancellation(t *testing.T) {
	s := newService(testStore(t), nil)
	jumpID := registerTestJump(t, s)
	port := listenTCP(t, func(conn net.Conn) {
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err == nil {
			conn.Write([]byte(line))
		}
		io.Copy(io.Discard, conn) // then stay silent until the client closes
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial, closeDial, err := s.dialer(ctx, Asset{Kind: "redis", Address: "127.0.0.1", Port: port, SSH: SSHConfig{JumpID: jumpID}})
	if err != nil {
		t.Fatal(err)
	}
	defer closeDial()
	conn, err := dial(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.Write([]byte("through-jump\n")); err != nil {
		t.Fatal(err)
	}
	if err = conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("jump connection rejected a read deadline: %v", err)
	}
	echo, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || echo != "through-jump\n" {
		t.Fatalf("bytes did not cross the SSH channel: %q %v", echo, err)
	}
	if err = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("silent upstream returned data")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("read deadline was not enforced: %v", err)
	}
}

// fakeRedis answers the RESP2 commands go-redis sends for a PING + INFO probe.
func fakeRedis(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		header, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(header, "*") {
			return
		}
		count, _ := strconv.Atoi(strings.TrimSpace(header[1:]))
		args := make([]string, 0, count)
		for i := 0; i < count; i++ {
			size, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			n, _ := strconv.Atoi(strings.TrimSpace(size[1:]))
			buf := make([]byte, n+2)
			if _, err = io.ReadFull(reader, buf); err != nil {
				return
			}
			args = append(args, string(buf[:n]))
		}
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			conn.Write([]byte("-ERR unknown command 'HELLO'\r\n"))
		case "PING":
			conn.Write([]byte("+PONG\r\n"))
		case "INFO":
			info := "used_memory:1048576\r\nmaxmemory:4194304\r\nconnected_clients:3\r\nblocked_clients:0\r\n"
			fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(info), info)
		default:
			conn.Write([]byte("+OK\r\n"))
		}
	}
}

func TestJumpRedisCollection(t *testing.T) {
	s := newService(testStore(t), nil)
	jumpID := registerTestJump(t, s)
	port := listenTCP(t, fakeRedis)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	values, _, err := s.collectRedis(ctx, StoredAsset{Asset: Asset{ID: "redis", Kind: "redis", Address: "127.0.0.1", Port: port, TLSMode: "disable", SSH: SSHConfig{JumpID: jumpID}}})
	if err != nil {
		t.Fatalf("Redis through jump failed: %v", err)
	}
	if values["redis-up"] != 1 || values["redis-clients"] != 3 || values["redis-memory"] != 25 {
		t.Fatalf("unexpected Redis values through jump: %v", values)
	}
}

// pgx cancels blocked reads by moving the socket deadline. Through a jump that
// only works with the bridge; otherwise a stalled server holds a collection slot.
func TestJumpPostgresHonoursCollectionDeadline(t *testing.T) {
	s := newService(testStore(t), nil)
	jumpID := registerTestJump(t, s)
	port := listenTCP(t, func(conn net.Conn) { io.Copy(io.Discard, conn) })
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := s.collectPostgres(ctx, StoredAsset{Asset: Asset{ID: "pg", Kind: "postgres", Address: "127.0.0.1", Port: port, Username: "u", TLSMode: "disable", SSH: SSHConfig{JumpID: jumpID}}, Secrets: Secrets{Password: "p"}})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("silent PostgreSQL server reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PostgreSQL collection through jump ignored its deadline")
	}
}

// Run only inside the isolated test Compose network with the test-ssh volume.
func TestIntegrationJumpPostgresRedis(t *testing.T) {
	if os.Getenv("PULSE_TEST_SSH_JUMP") != "true" {
		t.Skip("isolated test Compose only")
	}
	store := testStore(t)
	s := newService(store, nil)
	pin, err := os.ReadFile("/test-ssh/bastion.fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	password := "pulse_isolated_test_only"
	jump, err := store.Save(AssetInput{Asset: Asset{Kind: "server", Address: "test-bastion", SSH: SSHConfig{Username: "pulse", Fingerprint: strings.TrimSpace(string(pin))}}, SSHPassword: &password})
	if err != nil {
		t.Fatal(err)
	}
	targets := []struct {
		name    string
		record  StoredAsset
		collect func(context.Context, StoredAsset) (map[string]float64, map[string]float64, error)
		up      string
	}{
		{"postgres", StoredAsset{Asset: Asset{ID: "postgres", Kind: "postgres", Address: "postgres", Username: "pulse_test", Database: "pulse_test", TLSMode: "disable"}, Secrets: Secrets{Password: "pulse_test_only"}}, s.collectPostgres, "db-up"},
		{"redis", StoredAsset{Asset: Asset{ID: "redis", Kind: "redis", Address: "redis", TLSMode: "disable"}}, s.collectRedis, "redis-up"},
	}
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			for _, viaJump := range []bool{false, true} {
				record := target.record
				if viaJump {
					record.Asset.SSH.JumpID = jump.ID
				}
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				values, _, err := target.collect(ctx, record)
				cancel()
				if err != nil || values[target.up] != 1 {
					t.Fatalf("jump=%v: values %v, err %v", viaJump, values, err)
				}
				t.Logf("%s jump=%v: connected, statistics=%d", target.name, viaJump, len(values))
			}
		})
	}
}
