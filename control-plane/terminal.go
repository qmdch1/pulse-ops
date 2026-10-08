package main

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"
	"io"
	"net/http"
	"sync"
	"time"
)

type terminalTicket struct {
	AssetID string
	Expires time.Time
}
type terminalInput struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (s *Service) terminal(w http.ResponseWriter, r *http.Request) {
	if !s.origins[r.Header.Get("Origin")] {
		http.Error(w, "Origin denied", 403)
		return
	}
	s.mu.Lock()
	ticket, ok := s.tickets[r.URL.Query().Get("ticket")]
	delete(s.tickets, r.URL.Query().Get("ticket"))
	if !ok || time.Now().After(ticket.Expires) || s.sessions >= 8 {
		s.mu.Unlock()
		http.Error(w, "Session expired or capacity exceeded", 401)
		return
	}
	s.sessions++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.sessions--; s.mu.Unlock() }()
	ws, e := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.originHosts()})
	if e != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(65536)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Hour)
	defer cancel()
	var writeMu sync.Mutex
	send := func(kind websocket.MessageType, b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		c, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		return ws.Write(c, kind, b)
	}
	status := func(kind, message string) {
		b, _ := json.Marshal(map[string]string{"type": kind, "message": message})
		_ = send(websocket.MessageText, b)
	}
	connection, _, e := s.sshConnect(ctx, ticket.AssetID, false)
	if e != nil {
		status("error", e.Error())
		s.store.Record(ticket.AssetID, "terminal.open", "failed")
		return
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, connection.Close)
	defer stop()
	session, e := connection.Session()
	if e != nil {
		status("error", "SSH 세션 생성 실패")
		return
	}
	defer session.Close()
	if e = session.RequestPty("xterm-256color", 30, 100, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); e != nil {
		status("error", "PTY를 열 수 없습니다")
		return
	}
	stdin, e := session.StdinPipe()
	if e != nil {
		return
	}
	stdout, e := session.StdoutPipe()
	if e != nil {
		return
	}
	stderr, e := session.StderrPipe()
	if e != nil {
		return
	}
	if e = session.Shell(); e != nil {
		status("error", "셸을 시작할 수 없습니다")
		return
	}
	s.store.Record(ticket.AssetID, "terminal.open", "connected")
	defer s.store.Record(ticket.AssetID, "terminal.close", "closed")
	status("connected", "SSH 연결됨")
	var output sync.WaitGroup
	output.Add(2)
	copyOutput := func(src io.Reader) {
		defer output.Done()
		buffer := make([]byte, 16384)
		for {
			n, err := src.Read(buffer)
			if n > 0 {
				if send(websocket.MessageBinary, buffer[:n]) != nil {
					cancel()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go copyOutput(stdout)
	go copyOutput(stderr)
	go func() {
		_ = session.Wait()
		output.Wait()
		status("exit", "터미널 세션이 종료되었습니다")
		cancel()
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, done := context.WithTimeout(ctx, 10*time.Second)
				err := ws.Ping(pingCtx)
				done()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	for {
		_, raw, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var input terminalInput
		if json.Unmarshal(raw, &input) != nil {
			return
		}
		switch input.Type {
		case "input":
			if _, e = io.WriteString(stdin, input.Data); e != nil {
				return
			}
		case "resize":
			if input.Cols >= 2 && input.Cols <= 500 && input.Rows >= 2 && input.Rows <= 500 {
				_ = session.WindowChange(input.Rows, input.Cols)
			}
		case "disconnect":
			return
		default:
			status("error", "알 수 없는 터미널 입력입니다")
		}
	}
}
