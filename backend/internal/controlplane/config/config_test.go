package config

import (
	"strings"
	"testing"
	"time"

	appconfig "github.com/insmtx/lework/backend/config"
)

const testEnrollmentToken = "test-enrollment-token"

// validConfig 返回一份通过校验的配置，供各用例在此基础上做局部修改。
func validConfig() Config {
	cfg := Default()
	cfg.EnrollmentToken = testEnrollmentToken
	return cfg
}

func TestConfigDefault(t *testing.T) {
	cfg := Default()

	if cfg.ListenAddr != "127.0.0.1:8787" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:8787")
	}
	if cfg.DBPath == "" {
		t.Error("DBPath should not be empty")
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want %v", cfg.RequestTimeout, 30*time.Second)
	}
	if cfg.HeartbeatTimeout != 35*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want %v", cfg.HeartbeatTimeout, 35*time.Second)
	}
}

func TestConfigDefaultDBPath(t *testing.T) {
	path := DefaultDBPath()
	if path == "" {
		t.Fatal("DefaultDBPath should not be empty")
	}
	if !strings.HasSuffix(path, "controlplane.db") {
		t.Errorf("DefaultDBPath = %q, want suffix %q", path, "controlplane.db")
	}
}

func TestConfigValidateAcceptsLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "localhost:8787", "[::1]:8787"} {
		cfg := validConfig()
		cfg.ListenAddr = addr
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with ListenAddr %q returned error: %v", addr, err)
		}
	}
}

func TestConfigValidateAcceptsAnyBindAddress(t *testing.T) {
	for _, addr := range []string{
		"127.0.0.1:8787", "localhost:8787", "0.0.0.0:8787", ":8787", "192.168.1.2:8787", "127.0.0.1:0",
	} {
		cfg := validConfig()
		cfg.ListenAddr = addr
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with ListenAddr %q = %v, want nil", addr, err)
		}
	}
}

func TestConfigValidateRejectsMalformedListenAddr(t *testing.T) {
	for _, addr := range []string{"8787", "127.0.0.1", "host:port"} {
		cfg := validConfig()
		cfg.ListenAddr = addr
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate() with ListenAddr %q should fail", addr)
		}
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:8787": true, "localhost:8787": true, "[::1]:8787": true,
		"0.0.0.0:8787": false, ":8787": false, "192.168.1.2:8787": false, "garbage": false,
	}
	for addr, want := range tests {
		if got := IsLoopbackAddr(addr); got != want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestDialAddrFor(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1:8787":   "127.0.0.1:8787",
		"localhost:8787":   "localhost:8787",
		"0.0.0.0:8787":     "127.0.0.1:8787",
		":8787":            "127.0.0.1:8787",
		"[::]:8787":        "[::1]:8787",
		"192.168.1.2:9000": "192.168.1.2:9000",
		"garbage":          "garbage",
	}
	for addr, want := range tests {
		if got := DialAddrFor(addr); got != want {
			t.Errorf("DialAddrFor(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestConfigValidateRejectsEmptyToken(t *testing.T) {
	cfg := validConfig()
	cfg.EnrollmentToken = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() with empty token should fail")
	}
	if !strings.Contains(err.Error(), "enrollment_token") {
		t.Errorf("Validate() error = %q, want it to mention enrollment_token", err)
	}
}

func TestConfigValidateRejectsInvalidTimeouts(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(*Config)
	}{
		{"zero request timeout", "request_timeout", func(c *Config) { c.RequestTimeout = 0 }},
		{"negative request timeout", "request_timeout", func(c *Config) { c.RequestTimeout = -time.Second }},
		{"zero heartbeat timeout", "heartbeat_timeout", func(c *Config) { c.HeartbeatTimeout = 0 }},
		{"negative heartbeat timeout", "heartbeat_timeout", func(c *Config) { c.HeartbeatTimeout = -time.Second }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() should fail for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("Validate() error = %q, want it to mention %s", err, tc.field)
			}
		})
	}
}

func TestConfigValidateRejectsUnusableTokenChars(t *testing.T) {
	const unsafe = "bad token with spaces 123"
	cfg := validConfig()
	cfg.EnrollmentToken = unsafe

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() accepted a token with a space")
	}
	if strings.Contains(err.Error(), unsafe) {
		t.Errorf("Validate() error leaked the token (error len=%d)", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "printable ASCII") {
		t.Errorf("Validate() error does not name the charset constraint (error len=%d)", len(err.Error()))
	}
}

func TestConfigValidateDoesNotLeakToken(t *testing.T) {
	cfg := validConfig()
	// 用畸形地址触发校验失败，不再依赖回环限制。
	cfg.ListenAddr = "127.0.0.1"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail for a malformed listen address")
	}
	if strings.Contains(err.Error(), testEnrollmentToken) {
		t.Errorf("Validate() error leaked the enrollment token: %q", err)
	}
}

// clusterConfig 返回一份补齐了 NATS 与 PostgreSQL 的配置。
func clusterConfig() Config {
	cfg := validConfig()
	cfg.NATS = &appconfig.NATSConfig{URL: "nats://127.0.0.1:4222"}
	cfg.Database = &appconfig.DatabaseConfig{URL: "postgres://lework:lework@127.0.0.1:5432/lework"}
	return cfg
}

func TestValidateClusterRequiresNATSAndDatabase(t *testing.T) {
	if err := clusterConfig().ValidateCluster(); err != nil {
		t.Fatalf("ValidateCluster() with both URLs = %v, want nil", err)
	}

	cfg := clusterConfig()
	cfg.NATS = nil
	err := cfg.ValidateCluster()
	if err == nil {
		t.Fatal("ValidateCluster() without NATS should fail")
	}
	if !strings.Contains(err.Error(), "nats") {
		t.Errorf("ValidateCluster() error = %q, want it to mention nats", err)
	}

	cfg = clusterConfig()
	cfg.Database = nil
	err = cfg.ValidateCluster()
	if err == nil {
		t.Fatal("ValidateCluster() without Database should fail")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("ValidateCluster() error = %q, want it to mention database", err)
	}

	// 空 URL 与 nil 一样视为未配置。
	cfg = clusterConfig()
	cfg.NATS = &appconfig.NATSConfig{}
	if err := cfg.ValidateCluster(); err == nil || !strings.Contains(err.Error(), "nats") {
		t.Errorf("ValidateCluster() with empty NATS url = %v, want a nats error", err)
	}
	cfg = clusterConfig()
	cfg.Database = &appconfig.DatabaseConfig{}
	if err := cfg.ValidateCluster(); err == nil || !strings.Contains(err.Error(), "database") {
		t.Errorf("ValidateCluster() with empty database url = %v, want a database error", err)
	}
}

func TestValidateClusterRejectsWrongSchemes(t *testing.T) {
	cfg := clusterConfig()
	cfg.NATS.URL = "http://127.0.0.1:4222"
	if err := cfg.ValidateCluster(); err == nil || !strings.Contains(err.Error(), "nats") {
		t.Errorf("ValidateCluster() with an http NATS url = %v, want a scheme error", err)
	}

	cfg = clusterConfig()
	cfg.Database.URL = "mysql://lework:lework@127.0.0.1:3306/lework"
	if err := cfg.ValidateCluster(); err == nil || !strings.Contains(err.Error(), "postgres") {
		t.Errorf("ValidateCluster() with a mysql database url = %v, want a postgres error", err)
	}

	// postgresql:// 是 postgres 的等价写法。
	cfg = clusterConfig()
	cfg.Database.URL = "postgresql://lework:lework@127.0.0.1:5432/lework"
	if err := cfg.ValidateCluster(); err != nil {
		t.Errorf("ValidateCluster() with a postgresql url = %v, want nil", err)
	}
}

// TestValidateClusterDoesNotLeakURL 钉住：URL 可能内嵌凭据，错误文本只描述字段。
func TestValidateClusterDoesNotLeakURL(t *testing.T) {
	cfg := clusterConfig()
	cfg.Database.URL = "postgres://u:secret@h/db"
	cfg.NATS = &appconfig.NATSConfig{URL: "http://u:secret@h:4222"}

	err := cfg.ValidateCluster()
	if err == nil {
		t.Fatal("ValidateCluster() should fail for an invalid NATS scheme")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("ValidateCluster() error leaked the url: %q", err)
	}
}

// TestDefaultRegistrationAndLocalAdmin 钉住注册与本地直通策略的默认值：
// 默认开放注册，本机回环仍可直接以管理员身份操作。
func TestDefaultRegistrationAndLocalAdmin(t *testing.T) {
	cfg := Default()
	if cfg.RegistrationMode != "open" {
		t.Errorf("RegistrationMode = %q, want %q", cfg.RegistrationMode, "open")
	}
	if !cfg.LocalAdmin {
		t.Error("LocalAdmin = false, want true")
	}
}

func TestDefaultRegistrationModeIsOpen(t *testing.T) {
	if got := Default().RegistrationMode; got != "open" {
		t.Fatalf("Default().RegistrationMode = %q, want open", got)
	}
}

func TestValidateRejectsInviteMode(t *testing.T) {
	cfg := Default()
	cfg.EnrollmentToken = "token-123456"
	cfg.RegistrationMode = "invite"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil, want error for invite mode")
	}
}

func TestValidateAcceptsOpenAndClosed(t *testing.T) {
	for _, mode := range []string{"open", "closed"} {
		cfg := Default()
		cfg.EnrollmentToken = "token-123456"
		cfg.RegistrationMode = mode
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate(%q) = %v, want nil", mode, err)
		}
	}
}

func TestValidateRejectsUnknownRegistrationMode(t *testing.T) {
	cfg := validConfig()
	cfg.RegistrationMode = "public"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "registration_mode") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestDefaultClaimSettings(t *testing.T) {
	cfg := Default()
	if cfg.ClaimTTL != 5*time.Minute || cfg.ClaimCodeLength != 5 || cfg.SessionTTL != 30*24*time.Hour {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestConfigValidateAcceptsKnownRegistrationModes(t *testing.T) {
	for _, mode := range []string{"open", "closed"} {
		cfg := validConfig()
		cfg.RegistrationMode = mode
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with RegistrationMode %q = %v, want nil", mode, err)
		}
	}
}

func TestConfigValidateRejectsInvalidSessionAndClaimSettings(t *testing.T) {
	cases := []struct {
		name   string
		field  string
		mutate func(*Config)
	}{
		{"zero session ttl", "session_ttl", func(c *Config) { c.SessionTTL = 0 }},
		{"negative session ttl", "session_ttl", func(c *Config) { c.SessionTTL = -time.Hour }},
		{"zero claim ttl", "claim_ttl", func(c *Config) { c.ClaimTTL = 0 }},
		{"negative claim ttl", "claim_ttl", func(c *Config) { c.ClaimTTL = -time.Minute }},
		{"claim code too short", "claim_code_length", func(c *Config) { c.ClaimCodeLength = 3 }},
		{"claim code too long", "claim_code_length", func(c *Config) { c.ClaimCodeLength = 9 }},
		{"zero claim code length", "claim_code_length", func(c *Config) { c.ClaimCodeLength = 0 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() should fail for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("Validate() error = %q, want it to mention %s", err, tc.field)
			}
		})
	}
}

// TestConfigValidateAcceptsClaimCodeBounds 钉住 4..8 的两个端点也是合法取值。
func TestConfigValidateAcceptsClaimCodeBounds(t *testing.T) {
	for _, n := range []int{4, 8} {
		cfg := validConfig()
		cfg.ClaimCodeLength = n
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() with ClaimCodeLength %d = %v, want nil", n, err)
		}
	}
}

// TestValidateDoesNotRequireCluster 钉住：Validate 仍只校验本地启动参数，
// 单测与本地 SQLite 模式都不需要 NATS/PostgreSQL。
func TestValidateDoesNotRequireCluster(t *testing.T) {
	cfg := validConfig()
	if cfg.NATS != nil || cfg.Database != nil {
		t.Fatal("Default() must not invent cluster settings")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() without cluster settings = %v, want nil", err)
	}
}
