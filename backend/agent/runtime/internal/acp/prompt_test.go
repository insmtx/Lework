package acp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

func TestPromptIdleTimeoutCancels(t *testing.T) {
	cancelFile := filepath.Join(t.TempDir(), "cancels.log")
	conn, workDir := startFakeConn(t, []string{
		"FAKE_ACP_PROMPT=silent",
		"FAKE_ACP_CANCEL_FILE=" + cancelFile,
	})
	defer conn.Close()
	sess := mustNewSession(t, conn, workDir)

	startedAt := time.Now()
	_, err := sess.PromptWithOptions(context.Background(), "hi", PromptOptions{
		IdleTimeout:        150 * time.Millisecond,
		CancelDrainTimeout: 2 * time.Second,
	})
	elapsed := time.Since(startedAt)

	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("error = %v, want ErrIdleTimeout", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("idle cancel took %s, want prompt return soon after cancel", elapsed)
	}
	if !strings.Contains(err.Error(), "长时间未操作") {
		t.Fatalf("error text = %q, want v1 wording", err)
	}
	waitForFileLines(t, cancelFile, 1, time.Second)
}

func TestPromptIdleDrainKillsProcess(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{"FAKE_ACP_PROMPT=ignore-cancel"})
	defer conn.Close()
	sess := mustNewSession(t, conn, workDir)

	startedAt := time.Now()
	_, err := sess.PromptWithOptions(context.Background(), "hi", PromptOptions{
		IdleTimeout:        100 * time.Millisecond,
		CancelDrainTimeout: 200 * time.Millisecond,
	})
	elapsed := time.Since(startedAt)

	if !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("error = %v, want ErrIdleTimeout", err)
	}
	if !strings.Contains(err.Error(), "取消未在") {
		t.Fatalf("error = %q, want drain timeout detail", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("drain kill took %s", elapsed)
	}
	select {
	case <-conn.Done():
	case <-time.After(time.Second):
		t.Fatal("process was not killed after cancel drain timeout")
	}
}

func TestPromptExternalCancelWaitsDrain(t *testing.T) {
	cancelFile := filepath.Join(t.TempDir(), "cancels.log")
	conn, workDir := startFakeConn(t, []string{
		"FAKE_ACP_PROMPT=silent",
		"FAKE_ACP_CANCEL_FILE=" + cancelFile,
	})
	defer conn.Close()
	sess := mustNewSession(t, conn, workDir)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	startedAt := time.Now()
	_, err := sess.PromptWithOptions(ctx, "hi", PromptOptions{
		CancelDrainTimeout: time.Second,
	})
	elapsed := time.Since(startedAt)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("external cancel took %s", elapsed)
	}
	waitForFileLines(t, cancelFile, 1, time.Second)
}

func TestPromptAuthErrorClassified(t *testing.T) {
	conn, workDir := startFakeConn(t, []string{"FAKE_ACP_PROMPT=auth"})
	defer conn.Close()
	sess := mustNewSession(t, conn, workDir)

	_, err := sess.Prompt(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected prompt error")
	}
	if got := ClassifyError(err); got != ErrorKindAuthRequired {
		t.Fatalf("kind = %q, want %q (err=%v)", got, ErrorKindAuthRequired, err)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"nil", nil, ""},
		{"auth", &acpsdk.RequestError{Code: -32000, Message: "auth"}, ErrorKindAuthRequired},
		{"invalid params", &acpsdk.RequestError{Code: -32602, Message: "bad cwd"}, ErrorKindInvalidParams},
		{"method not found", &acpsdk.RequestError{Code: -32601, Message: "unknown"}, ErrorKindProtocol},
		{"internal", &acpsdk.RequestError{Code: -32603, Message: "boom"}, ErrorKindRuntime},
		{"canceled", context.Canceled, ErrorKindCanceled},
		{"deadline", context.DeadlineExceeded, ErrorKindCanceled},
		{"idle", ErrIdleTimeout, ErrorKindIdleTimeout},
		{"wrapped idle", errWrap(ErrIdleTimeout), ErrorKindIdleTimeout},
		{"peer closed", errors.New("peer disconnected before response"), ErrorKindPeerClosed},
		{"generic", errors.New("something else"), ErrorKindRuntime},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.want {
				t.Fatalf("ClassifyError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// waitForFileLines 等文件累计到 want 行；取消通知可能晚于 Prompt 返回。
func waitForFileLines(t *testing.T, path string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if countLines(t, path) >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("file %s has %d lines, want %d", path, countLines(t, path), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func mustNewSession(t *testing.T, conn *Conn, workDir string) *Session {
	t.Helper()
	sess, err := conn.NewSession(context.Background(), workDir, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	return sess
}

func errWrap(err error) error {
	return fmt.Errorf("wrap: %w", err)
}
