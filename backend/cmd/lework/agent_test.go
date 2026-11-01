package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
	if got := controlPlaneHeader(""); got != nil {
		t.Fatalf("controlPlaneHeader(\"\") = %v, want nil", got)
	}
	header := controlPlaneHeader("secret-token")
	if got, want := header.Get("Authorization"), "Bearer secret-token"; got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
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
