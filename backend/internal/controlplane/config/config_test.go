package config

import (
	"strings"
	"testing"
	"time"
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
