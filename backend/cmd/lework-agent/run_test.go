package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestResolveOpenCodePasswordPrefersOpenCodePassword(t *testing.T) {
	env := map[string]string{
		"OPENCODE_PASSWORD":        "primary",
		"OPENCODE_SERVER_PASSWORD": "fallback",
	}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "primary" {
		t.Fatalf("password = %q, want OPENCODE_PASSWORD value", password)
	}
	if generated {
		t.Fatal("generated = true, want false for a configured password")
	}
}

func TestResolveOpenCodePasswordUsesServerPassword(t *testing.T) {
	env := map[string]string{"OPENCODE_SERVER_PASSWORD": "server"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "server" {
		t.Fatalf("password = %q, want OPENCODE_SERVER_PASSWORD value", password)
	}
	if generated {
		t.Fatal("generated = true, want false for a configured password")
	}
}

func TestResolveOpenCodePasswordGeneratesURLSafeValue(t *testing.T) {
	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if !generated {
		t.Fatal("generated = false, want true when no password is configured")
	}
	if password == "" {
		t.Fatal("generated password is empty")
	}
	if strings.ContainsAny(password, "+/=") {
		t.Fatalf("generated password %q is not unpadded base64url", password)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(password)
	if err != nil {
		t.Fatalf("generated password is not base64url: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("generated password decodes to %d bytes, want 32", len(decoded))
	}
}

func TestOpenCodeEnvDoesNotMutateInput(t *testing.T) {
	input := map[string]string{"OPENCODE_LOG": "debug"}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password == "" {
		t.Fatal("openCodeEnv() returned an empty password")
	}

	if len(input) != 1 {
		t.Fatalf("input map gained keys: %v", input)
	}
	if _, ok := input["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("input map was given the generated password")
	}

	if env["OPENCODE_LOG"] != "debug" {
		t.Fatalf("copied env lost OPENCODE_LOG: %v", env)
	}
	if env["OPENCODE_SERVER_PASSWORD"] != password {
		t.Fatalf("copied env password = %q, want %q", env["OPENCODE_SERVER_PASSWORD"], password)
	}

	// The copy must be independent, not an alias of the loaded config.
	env["OPENCODE_LOG"] = "trace"
	if input["OPENCODE_LOG"] != "debug" {
		t.Fatalf("mutating the copy changed the input: %v", input)
	}
}

func TestOpenCodeEnvKeepsConfiguredPassword(t *testing.T) {
	input := map[string]string{"OPENCODE_SERVER_PASSWORD": "configured"}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "configured" {
		t.Fatalf("password = %q, want configured", password)
	}
	if env["OPENCODE_SERVER_PASSWORD"] != "configured" {
		t.Fatalf("env password = %q, want configured", env["OPENCODE_SERVER_PASSWORD"])
	}
}
