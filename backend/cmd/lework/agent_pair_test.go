package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/credential"
	"github.com/insmtx/lework/backend/internal/agentd/pair"
)

func TestAgentHasPairingFlags(t *testing.T) {
	cmd := newAgentCommand()
	for _, name := range []string{"code", "no-qr", "credential-file"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("--%s flag missing", name)
		}
	}
}

func TestAgentHasClaimSubcommand(t *testing.T) {
	claimCmd, _, err := newAgentCommand().Find([]string{"claim"})
	if err != nil || claimCmd == nil || claimCmd.Name() != "claim" {
		t.Fatalf("claim command missing: %v", err)
	}
	if claimCmd.RunE == nil {
		t.Fatal("claim command has no RunE")
	}
}

// TestAgentRejectsCodeWithCredentialFile 固化互斥：--code 走配对，--credential-file
// 走既成凭证，两者同时给出会让「凭证优先」与「显式配对」互相打架。
func TestAgentRejectsCodeWithCredentialFile(t *testing.T) {
	cmd := newAgentCommand()
	cmd.SetArgs([]string{"--code", "ABC12", "--credential-file", "/tmp/node.credential"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--code and --credential-file are mutually exclusive") {
		t.Fatalf("err = %v, want the mutual exclusion error", err)
	}
}

func TestClaimPrintsPendingCode(t *testing.T) {
	dir := t.TempDir()
	credentialPath := filepath.Join(dir, "node.credential")
	writeClaimForTest(t, pair.ClaimPath(credentialPath), pair.Claim{
		Link:            "lework://node/pair?code=AB234",
		PairKey:         "pair-key",
		Code:            "AB234",
		ControlPlaneURL: "ws://cp.example",
		ExpiresAt:       time.Now().Add(5 * time.Minute),
	})

	cmd := newAgentCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claim", "--credential-file", credentialPath, "--config", filepath.Join(dir, "agent.yaml")})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("claim: %v", err)
	}
	text := out.String()
	for _, want := range []string{"AB234", "lework://node/pair", "Expires at"} {
		if !strings.Contains(text, want) {
			t.Fatalf("claim output = %q, want %q", text, want)
		}
	}
	// claim 是终端命令，非 TTY 也必须打印文字（但不应尝试画二维码）。
	if strings.Contains(text, "▀") || strings.Contains(text, "▄") {
		t.Fatal("claim drew a QR code to a non-terminal writer")
	}
}

func TestClaimExpiredReturnsErrorWithoutCode(t *testing.T) {
	dir := t.TempDir()
	credentialPath := filepath.Join(dir, "node.credential")
	writeClaimForTest(t, pair.ClaimPath(credentialPath), pair.Claim{
		Link:      "lework://node/pair?code=ZZ999",
		PairKey:   "pair-key",
		Code:      "ZZ999",
		ExpiresAt: time.Now().Add(-time.Minute),
	})

	cmd := newAgentCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"claim", "--credential-file", credentialPath, "--config", filepath.Join(dir, "agent.yaml")})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("claim accepted an expired claim file")
	}
	if !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "lework agent") {
		t.Fatalf("err = %v, want an expired message pointing at lework agent", err)
	}
	if strings.Contains(out.String(), "ZZ999") {
		t.Fatalf("expired claim printed the dead code: %q", out.String())
	}
}

func TestClaimWithoutClaimFileExplainsHowToStart(t *testing.T) {
	dir := t.TempDir()
	cmd := newAgentCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"claim",
		"--credential-file", filepath.Join(dir, "node.credential"),
		"--config", filepath.Join(dir, "agent.yaml"),
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("claim succeeded without a claim file")
	}
	if !strings.Contains(err.Error(), "lework agent") {
		t.Fatalf("err = %v, want a hint to run lework agent", err)
	}
}

func TestResolveCredentialPath(t *testing.T) {
	tests := []struct {
		name        string
		flagPath    string
		configPath  string
		configValue string
		want        string
	}{
		{"flag wins", "/flag/node.credential", "/cfg/agent.yaml", "/cfg/declared", "/flag/node.credential"},
		{"config beats default", "", "/cfg/agent.yaml", "/cfg/declared", "/cfg/declared"},
		{"default next to config", "", "/cfg/agent.yaml", "", credential.DefaultPath("/cfg/agent.yaml")},
		{"default without config", "", "", "", credential.DefaultPath("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCredentialPath(tt.flagPath, tt.configPath, tt.configValue); got != tt.want {
				t.Fatalf("resolveCredentialPath = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPairFailureGuidance 保证每条配对失败都有可执行的中文/英文提示，且提示
// 文本永远不含凭证占位符——它是直接打到用户终端的最后一道脱敏边界。
func TestPairFailureGuidance(t *testing.T) {
	const secret = "raw-credential-sentinel"
	sentinels := []error{
		pair.ErrPairingExpired,
		pair.ErrCredentialAlreadyIssued,
		pair.ErrClaimCodeUnavailable,
		pair.ErrClaimAttemptsExceeded,
		pair.ErrPairConnReplaced,
		errors.New("some generic transport failure"),
	}
	seen := map[string]string{}
	for _, err := range sentinels {
		guidance := pairFailureGuidance(err)
		if strings.TrimSpace(guidance) == "" {
			t.Fatalf("pairFailureGuidance(%v) is empty", err)
		}
		for _, placeholder := range []string{secret, "%s", "%v", "%!", "<credential>", "credential="} {
			if strings.Contains(guidance, placeholder) {
				t.Fatalf("guidance for %v contains %q: %q", err, placeholder, guidance)
			}
		}
		seen[guidance] = err.Error()
	}
	if len(seen) != len(sentinels) {
		t.Fatalf("guidance is not distinct per failure: %v", seen)
	}
}

func writeClaimForTest(t *testing.T, path string, claim pair.Claim) {
	t.Helper()
	raw, err := json.Marshal(claim)
	if err != nil {
		t.Fatalf("encode claim: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create claim dir: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write claim: %v", err)
	}
}
