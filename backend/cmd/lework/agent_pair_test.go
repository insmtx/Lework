package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	absFlag, err := filepath.Abs("node.credential")
	if err != nil {
		t.Fatalf("resolve absolute flag path: %v", err)
	}
	tests := []struct {
		name        string
		flagPath    string
		configPath  string
		configValue string
		want        string
	}{
		{"flag wins", "/flag/node.credential", "/cfg/agent.yaml", "/cfg/declared", "/flag/node.credential"},
		// 显式 flag 也必须是绝对路径：systemd/容器里的相对路径会随启动目录漂移。
		{"flag relative becomes absolute", "node.credential", "/cfg/agent.yaml", "", absFlag},
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

func TestClaimHasNoQRFlag(t *testing.T) {
	claimCmd, _, err := newAgentCommand().Find([]string{"claim"})
	if err != nil || claimCmd == nil {
		t.Fatalf("claim command missing: %v", err)
	}
	if claimCmd.Flags().Lookup("no-qr") == nil {
		t.Fatal("claim is missing --no-qr")
	}
}

// TestClaimErrorIsPrintedToStderr 固化可见性：cobra 的 SilenceErrors 会让用户
// 只看到退出码 1，看不见「没有待定配对」这类可执行提示。
func TestClaimErrorIsPrintedToStderr(t *testing.T) {
	dir := t.TempDir()
	cmd := newAgentCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"claim",
		"--credential-file", filepath.Join(dir, "node.credential"),
		"--config", filepath.Join(dir, "agent.yaml"),
	})

	if err := cmd.Execute(); err == nil {
		t.Fatal("claim succeeded without a claim file")
	}
	if !strings.Contains(stderr.String(), "no pending pairing") {
		t.Fatalf("stderr = %q, want the claim error text", stderr.String())
	}
}

func TestFindCredential(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "node.credential")
	if err := os.WriteFile(filePath, []byte("file-cred\n"), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}

	t.Run("file wins over env", func(t *testing.T) {
		t.Setenv(nodeCredentialEnv, "env-cred")
		raw, source, found, err := findCredential(filePath)
		if err != nil || !found || raw != "file-cred" || source != credentialSourceFile {
			t.Fatalf("findCredential = %q, %q, %t, %v", raw, source, found, err)
		}
	})

	t.Run("env when file missing", func(t *testing.T) {
		t.Setenv(nodeCredentialEnv, "env-cred")
		raw, source, found, err := findCredential(filepath.Join(dir, "missing.credential"))
		if err != nil || !found || raw != "env-cred" || source != credentialSourceEnv {
			t.Fatalf("findCredential = %q, %q, %t, %v", raw, source, found, err)
		}
	})

	t.Run("empty file falls back to env", func(t *testing.T) {
		emptyPath := filepath.Join(dir, "empty.credential")
		if err := os.WriteFile(emptyPath, []byte("  \n"), 0o600); err != nil {
			t.Fatalf("write empty credential: %v", err)
		}
		t.Setenv(nodeCredentialEnv, "env-cred")
		raw, source, found, err := findCredential(emptyPath)
		if err != nil || !found || raw != "env-cred" || source != credentialSourceEnv {
			t.Fatalf("findCredential = %q, %q, %t, %v", raw, source, found, err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Setenv(nodeCredentialEnv, "")
		raw, source, found, err := findCredential(filepath.Join(dir, "missing.credential"))
		if err != nil || found || raw != "" || source != "" {
			t.Fatalf("findCredential = %q, %q, %t, %v", raw, source, found, err)
		}
	})
}

// TestPairingModeForPriority 固化 reconnect 的来源优先级：已有凭证永远优先，
// 但 --code 与已有凭证同时出现时必须显式报错，不能静默丢弃 --code。
func TestPairingModeForPriority(t *testing.T) {
	const credentialPath = "/tmp/node.credential"
	tests := []struct {
		name            string
		code            string
		hasCredential   bool
		source          credentialSource
		enrollmentToken string
		want            pairingMode
		wantErr         string
	}{
		{
			name:          "code with file credential fails",
			code:          "ABC12",
			hasCredential: true,
			source:        credentialSourceFile,
			want:          pairingModeUseCredential,
			wantErr:       "already exists at /tmp/node.credential",
		},
		{
			name:          "code with env credential fails",
			code:          "ABC12",
			hasCredential: true,
			source:        credentialSourceEnv,
			want:          pairingModeUseCredential,
			wantErr:       "LEWORK_NODE_CREDENTIAL is set",
		},
		{
			name: "no credential with code claims",
			code: "ABC12",
			want: pairingModeCode,
		},
		{
			name:          "credential without code is used",
			hasCredential: true,
			source:        credentialSourceFile,
			want:          pairingModeUseCredential,
		},
		{
			name:            "enrollment token fallback",
			enrollmentToken: "enroll-token",
			want:            pairingModeEnrollmentToken,
		},
		{
			name: "otherwise interactive",
			want: pairingModeInteractive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := pairingModeFor(tt.code, tt.hasCredential, tt.source, credentialPath, tt.enrollmentToken)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("pairingModeFor: %v", err)
			}
			if mode != tt.want {
				t.Fatalf("mode = %v, want %v", mode, tt.want)
			}
		})
	}
}

func TestFatalPairError(t *testing.T) {
	fatal := []error{
		fmt.Errorf("claim dir: %w", pair.ErrClaimWrite),
		fmt.Errorf("already issued: %w", pair.ErrCredentialAlreadyIssued),
		fmt.Errorf("save: %w", errPairCredentialSave),
	}
	for _, err := range fatal {
		if !fatalPairError(err) {
			t.Fatalf("fatalPairError(%v) = false, want true", err)
		}
	}
	for _, err := range []error{pair.ErrPairingExpired, pair.ErrClaimCodeUnavailable, errors.New("network")} {
		if fatalPairError(err) {
			t.Fatalf("fatalPairError(%v) = true, want false", err)
		}
	}
}

// TestPairRetryFreshKeyAndBackoff 固化「每次尝试换新码 + 按阶梯退避」：同一张
// 过期/被接管的码重试永远不会成功，只会浪费用户的扫码动作。
func TestPairRetryFreshKeyAndBackoff(t *testing.T) {
	var keys []string
	var waits []time.Duration
	stop := errors.New("stop retry")
	counter := 0
	newKey := func() (string, error) {
		counter++
		return fmt.Sprintf("key-%d", counter), nil
	}
	attempt := func(_ context.Context, pairKey string) (string, error) {
		keys = append(keys, pairKey)
		return "", pair.ErrPairingExpired
	}
	wait := func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		if len(waits) == 2 {
			return stop
		}
		return nil
	}

	if _, err := pairRetry(context.Background(), newKey, attempt, wait); !errors.Is(err, stop) {
		t.Fatalf("pairRetry error = %v, want stop", err)
	}
	if len(keys) != 2 || keys[0] == keys[1] {
		t.Fatalf("pair keys = %v, want two distinct keys", keys)
	}
	if len(waits) != 2 || waits[0] != initialBackoff || waits[1] != nextBackoff(initialBackoff) {
		t.Fatalf("backoff waits = %v, want [%v %v]", waits, initialBackoff, nextBackoff(initialBackoff))
	}
}

func TestPairRetryStopsOnFatalError(t *testing.T) {
	calls := 0
	_, err := pairRetry(
		context.Background(),
		func() (string, error) { return "key", nil },
		func(context.Context, string) (string, error) {
			calls++
			return "", fmt.Errorf("claim dir: %w", pair.ErrClaimWrite)
		},
		func(context.Context, time.Duration) error {
			t.Fatal("must not back off after a fatal pairing error")
			return nil
		},
	)
	if calls != 1 {
		t.Fatalf("attempt calls = %d, want 1", calls)
	}
	if !errors.Is(err, pair.ErrClaimWrite) {
		t.Fatalf("pairRetry error = %v, want ErrClaimWrite", err)
	}
}

// TestPrintPendingPairSuppressesSecretsOffTTY 固化非 TTY 行为：二维码没有意义，
// 短码写进日志反而扩大泄露面；只提示 claim 文件的位置。
func TestPrintPendingPairSuppressesSecretsOffTTY(t *testing.T) {
	var out bytes.Buffer
	claimPath := filepath.Join(t.TempDir(), "node.claim")
	printPendingPair(&out, "AB234", "lework://node/pair?code=AB234", claimPath, false)

	text := out.String()
	if !strings.Contains(text, claimPath) || !strings.Contains(text, "lework agent claim") {
		t.Fatalf("output = %q, want the claim path and reprint hint", text)
	}
	for _, forbidden := range []string{"AB234", "lework://node/pair", "▀", "▄"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("non-TTY output leaked %q: %q", forbidden, text)
		}
	}
}

func TestRemoveClaimFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.claim")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write claim: %v", err)
	}
	removeClaimFile(path)
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("claim file still present: %v", err)
	}
	// 已不存在是正常情况，空路径也不该 panic。
	removeClaimFile(path)
	removeClaimFile("")
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
