package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Both CLI protocols are machine readable. Exit code zero alone is not proof
// that the agent completed its turn; a terminal protocol result is required.
type aiRunResult struct {
	Text          string
	Session       string
	InputTokens   int64
	OutputTokens  int64
	UsageReported bool
}
type boundedAIOutput struct {
	mu    sync.Mutex
	value strings.Builder
}

func (b *boundedAIOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := 128*1024 - b.value.Len()
	if room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		_, _ = b.value.Write(p)
	}
	return n, nil
}
func (b *boundedAIOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.value.String() }
func aiArguments(provider, model string) ([]string, error) {
	var args []string
	switch provider {
	case "codex":
		args = []string{"exec", "--json", "--sandbox", "workspace-write", "-c", "approval_policy=\"never\""}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, "-")
	case "claude":
		args = []string{"-p", "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits", "--allowedTools", "Read,Edit,Write,Glob,Grep,Bash"}
		if model != "" {
			args = append(args, "--model", model)
		}
	default:
		return nil, errors.New("지원하지 않는 실행 대상입니다")
	}
	return args, nil
}
func parseAIEvent(provider string, raw []byte, result *aiRunResult) (bool, error) {
	var event struct {
		Type              string            `json:"type"`
		Subtype           string            `json:"subtype"`
		ThreadID          string            `json:"thread_id"`
		SessionID         string            `json:"session_id"`
		Result            string            `json:"result"`
		IsError           bool              `json:"is_error"`
		PermissionDenials []json.RawMessage `json:"permission_denials"`
		Errors            []string          `json:"errors"`
		Item              struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Usage *struct {
			InputTokens   int64 `json:"input_tokens"`
			OutputTokens  int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return false, errors.New("실행 도구의 JSON 응답을 읽지 못했습니다")
	}
	if provider == "codex" {
		switch event.Type {
		case "thread.started":
			result.Session = event.ThreadID
		case "item.completed":
			if event.Item.Type == "agent_message" {
				if len(result.Text) < 128*1024 {
					result.Text += event.Item.Text + "\n"
				}
			}
		case "turn.completed":
			if event.Usage != nil {
				result.InputTokens += event.Usage.InputTokens
				result.OutputTokens += event.Usage.OutputTokens
				result.UsageReported = true
			}
			return true, nil
		case "turn.failed", "error":
			return true, errors.New("Codex 실행에 실패했습니다. 로그인·모델 접근 권한과 실행 환경을 확인하세요")
		}
	} else {
		if event.SessionID != "" {
			result.Session = event.SessionID
		}
		if event.Type == "result" {
			result.Text = event.Result
			if event.Usage != nil {
				result.InputTokens = event.Usage.InputTokens + event.Usage.CacheCreation + event.Usage.CacheRead
				result.OutputTokens = event.Usage.OutputTokens
				result.UsageReported = true
			}
			if event.IsError || event.Subtype != "success" || len(event.PermissionDenials) > 0 {
				return true, errors.New("Claude 실행이 완료되지 않았습니다. 로그인·모델 접근 권한 또는 거부된 도구 권한을 확인하세요")
			}
			return true, nil
		}
	}
	return false, nil
}
func runAIAgent(ctx context.Context, provider, model, workspace, prompt string) (aiRunResult, error) {
	result := aiRunResult{}
	args, err := aiArguments(provider, model)
	if err != nil {
		return result, err
	}
	cmd := exec.CommandContext(ctx, aiExecutable(provider), args...)
	cmd.Dir = workspace
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(key, "CONTROL_") && !strings.HasPrefix(key, "DASHBOARD_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 3 * time.Second
	prepareAICommand(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, errors.New("실행 프로세스를 열지 못했습니다")
	}
	if err = cmd.Start(); err != nil {
		return result, fmt.Errorf("%s를 시작하지 못했습니다. 설치 경로와 로그인 상태를 확인하세요", provider)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	completed := false
	var protocolErr error
	for scanner.Scan() {
		terminal, e := parseAIEvent(provider, scanner.Bytes(), &result)
		if e != nil {
			protocolErr = e
			_ = cmd.Cancel()
			break
		}
		if terminal {
			completed = true
		}
		if len(result.Text) > 128*1024 {
			result.Text = result.Text[:128*1024]
		}
	}
	scanErr := scanner.Err()
	if scanErr != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	if protocolErr != nil {
		return result, protocolErr
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if scanErr != nil {
		return result, errors.New("실행 도구의 출력 한도 또는 연결 상태를 확인하세요")
	}
	if waitErr != nil {
		return result, fmt.Errorf("%s 프로세스가 오류로 종료되었습니다. 로그인과 실행 환경을 확인하세요", provider)
	}
	if !completed {
		return result, errors.New("실행 도구가 완료 결과를 보내지 않았습니다")
	}
	return result, nil
}
