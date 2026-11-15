package main

import (
	"bytes"
	"testing"

	controlplaneconfig "github.com/insmtx/lework/backend/internal/controlplane/config"
)

func TestAnnouncePairingSilentWhenNotTerminal(t *testing.T) {
	var buf bytes.Buffer
	cfg := controlplaneconfig.Default()
	cfg.ListenAddr = "127.0.0.1:8787"
	cfg.EnrollmentToken = "secret-token"
	announcePairing(&buf, cfg, pairOptions{publicURL: "http://dev.ckeyer.com"})
	if buf.Len() != 0 {
		t.Fatalf("non-terminal output = %q, want empty", buf.String())
	}
}

func TestAnnouncePairingSilentWhenDisabled(t *testing.T) {
	var buf bytes.Buffer
	cfg := controlplaneconfig.Default()
	cfg.ListenAddr = "127.0.0.1:8787"
	cfg.EnrollmentToken = "secret-token"
	announcePairing(&buf, cfg, pairOptions{publicURL: "http://dev.ckeyer.com", noQR: true})
	if buf.Len() != 0 {
		t.Fatalf("--no-qr output = %q, want empty", buf.String())
	}
}
