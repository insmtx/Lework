package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/runner"
	"github.com/insmtx/lework/backend/internal/agentd/transport"
)

func TestDefaultConfigPathFor(t *testing.T) {
	if got, want := defaultConfigPathFor("linux"), "/etc/lework/agent.yaml"; got != want {
		t.Fatalf("linux default = %q, want %q", got, want)
	}
	for _, goos := range []string{"darwin", "windows"} {
		got := defaultConfigPathFor(goos)
		normalized := strings.ReplaceAll(got, `\`, "/")
		if !strings.HasSuffix(normalized, "Lework/agent.yaml") {
			t.Fatalf("%s default = %q, want a Lework/agent.yaml suffix", goos, got)
		}
	}
}

func TestDefaultConfigPathForWithoutConfigDir(t *testing.T) {
	original := userConfigDir
	userConfigDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userConfigDir = original })

	for _, goos := range []string{"darwin", "freebsd"} {
		if got, want := defaultConfigPathFor(goos), "agent.yaml"; got != want {
			t.Fatalf("%s fallback = %q, want honest relative %q", goos, got, want)
		}
	}
}

func TestEnvSliceIsSorted(t *testing.T) {
	got := envSlice(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("envSlice = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("envSlice = %v, want %v", got, want)
		}
	}
	if got := envSlice(nil); got != nil {
		t.Fatalf("envSlice(nil) = %v, want nil", got)
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{current: time.Second, want: 2 * time.Second},
		{current: 2 * time.Second, want: 4 * time.Second},
		{current: 4 * time.Second, want: 8 * time.Second},
		{current: 8 * time.Second, want: 16 * time.Second},
		{current: 16 * time.Second, want: 30 * time.Second},
		{current: 30 * time.Second, want: 30 * time.Second},
		{current: 0, want: time.Second},
	}
	for _, tt := range tests {
		if got := nextBackoff(tt.current); got != tt.want {
			t.Errorf("nextBackoff(%s) = %s, want %s", tt.current, got, tt.want)
		}
	}
}

func TestShouldResetBackoff(t *testing.T) {
	tests := []struct {
		connectedFor time.Duration
		want         bool
	}{
		{connectedFor: 0, want: false},
		{connectedFor: 9 * time.Second, want: false},
		{connectedFor: backoffResetAfter - time.Millisecond, want: false},
		{connectedFor: backoffResetAfter, want: true},
		{connectedFor: time.Minute, want: true},
	}
	for _, tt := range tests {
		if got := shouldResetBackoff(tt.connectedFor); got != tt.want {
			t.Errorf("shouldResetBackoff(%s) = %t, want %t", tt.connectedFor, got, tt.want)
		}
	}
}

func TestWaitBackoffReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := waitBackoff(ctx, time.Hour); err == nil {
		t.Fatal("waitBackoff returned nil on canceled context")
	}
}

func TestControlPlaneHeader(t *testing.T) {
	if got := controlPlaneHeader("", ""); got != nil {
		t.Fatalf("controlPlaneHeader(\"\", \"\") = %v, want nil", got)
	}
	// 凭证优先于 enrollment token：节点身份只能有一个 Bearer 值。
	if got := controlPlaneHeader("node-cred", "enroll"); got.Get("Authorization") != "Bearer node-cred" {
		t.Fatalf("Authorization = %q, want the node credential", got.Get("Authorization"))
	}
	// 还没有凭证时沿用旧令牌直通，老部署无需重新配对。
	if got := controlPlaneHeader("", "enroll"); got.Get("Authorization") != "Bearer enroll" {
		t.Fatalf("Authorization = %q, want the enrollment token", got.Get("Authorization"))
	}
	if got := controlPlaneHeader("node-cred", ""); got.Get("Authorization") != "Bearer node-cred" {
		t.Fatalf("Authorization = %q, want the node credential", got.Get("Authorization"))
	}
}

func TestCredentialRejected(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"401 handshake", fmt.Errorf("dial ws://cp: %w", &transport.HandshakeError{StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized"}), true},
		{"403 handshake", fmt.Errorf("dial ws://cp: %w", &transport.HandshakeError{StatusCode: http.StatusForbidden, Status: "403 Forbidden"}), false},
		{"plain handshake", &transport.HandshakeError{StatusCode: http.StatusBadGateway, Status: "502 Bad Gateway"}, false},
		{"generic dial failure", errors.New("dial tcp: connection refused"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := credentialRejected(tt.err); got != tt.want {
				t.Fatalf("credentialRejected() = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestFatalRunError 固化「凭证落盘失败必须让进程退出」：重试只会让控制面用
// 旧令牌再建一个节点、再签发一份永远写不下的凭证。
func TestFatalRunError(t *testing.T) {
	persistErr := fmt.Errorf("agentd: %w", fmt.Errorf("persist node credential: %w",
		runner.ErrCredentialPersist))
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"credential persist failure", persistErr, true},
		{"direct sentinel", runner.ErrCredentialPersist, true},
		{"connection ended", errors.New("agentd: read frame: EOF"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fatalRunError(tt.err); got != tt.want {
				t.Fatalf("fatalRunError() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRedactHidesSecret(t *testing.T) {
	err := errors.New("dial wss://host/connect?token=s3cret failed")
	got := redact(err, "s3cret")
	if strings.Contains(got, "s3cret") {
		t.Fatalf("redact leaked the secret: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("redact = %q, want a redaction marker", got)
	}
	if got := redact(nil, "s3cret"); got != "" {
		t.Fatalf("redact(nil) = %q, want empty", got)
	}
}
