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

func TestConfigValidateRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8787", ":8787"} {
		cfg := validConfig()
		cfg.ListenAddr = addr
		err := cfg.Validate()
		if err == nil {
			t.Fatalf("Validate() with ListenAddr %q should fail", addr)
		}
		if !strings.Contains(err.Error(), "loopback") {
			t.Errorf("Validate() error = %q, want it to mention loopback", err)
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
	cfg.ListenAddr = "0.0.0.0:8787"

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() should fail for non-loopback address")
	}
	if strings.Contains(err.Error(), testEnrollmentToken) {
		t.Errorf("Validate() error leaked the enrollment token: %q", err)
	}
}
