package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// clearOpenCodePasswordEnv makes a test independent of the daemon's ambient
// environment, which resolveOpenCodePassword deliberately consults because the
// child inherits it.
func clearOpenCodePasswordEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENCODE_PASSWORD", "")
	t.Setenv("OPENCODE_SERVER_PASSWORD", "")
}

func TestResolveOpenCodePasswordPrefersOpenCodePassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)

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
	clearOpenCodePasswordEnv(t)

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

func TestResolveOpenCodePasswordUsesInheritedOpenCodePassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false when the process env provides a password")
	}
}

func TestResolveOpenCodePasswordUsesInheritedServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_SERVER_PASSWORD", "inherited-server")

	password, generated, err := resolveOpenCodePassword(map[string]string{})
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited-server" {
		t.Fatalf("password = %q, want the inherited OPENCODE_SERVER_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false when the process env provides a password")
	}
}

func TestResolveOpenCodePasswordInheritedOpenCodePasswordBeatsYAMLServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	// The child keeps the inherited OPENCODE_PASSWORD and reads it before the
	// yaml-supplied OPENCODE_SERVER_PASSWORD, so the inherited value wins.
	env := map[string]string{"OPENCODE_SERVER_PASSWORD": "yaml-server"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false")
	}
}

func TestResolveOpenCodePasswordYAMLPasswordBeatsInheritedServerPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_SERVER_PASSWORD", "inherited-server")

	env := map[string]string{"OPENCODE_PASSWORD": "yaml-primary"}

	password, generated, err := resolveOpenCodePassword(env)
	if err != nil {
		t.Fatalf("resolveOpenCodePassword() error = %v, want nil", err)
	}
	if password != "yaml-primary" {
		t.Fatalf("password = %q, want the yaml OPENCODE_PASSWORD", password)
	}
	if generated {
		t.Fatal("generated = true, want false")
	}
}

func TestResolveOpenCodePasswordGeneratesURLSafeValue(t *testing.T) {
	clearOpenCodePasswordEnv(t)

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
	clearOpenCodePasswordEnv(t)

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
	clearOpenCodePasswordEnv(t)

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

func TestOpenCodeEnvUsesInheritedPassword(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	env, password, err := openCodeEnv(map[string]string{"OPENCODE_LOG": "debug"})
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if _, ok := env["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("a generated password was injected despite the inherited password")
	}
}

func TestOpenCodeEnvEmptyYAMLPasswordDoesNotShadowInherited(t *testing.T) {
	clearOpenCodePasswordEnv(t)
	t.Setenv("OPENCODE_PASSWORD", "inherited")

	// `OPENCODE_PASSWORD: ""` in agent.yaml must mean "unset" instead of
	// overriding the inherited value when mergeEnv overlays the child env.
	input := map[string]string{
		"OPENCODE_PASSWORD":        "",
		"OPENCODE_SERVER_PASSWORD": "",
		"OPENCODE_LOG":             "debug",
	}

	env, password, err := openCodeEnv(input)
	if err != nil {
		t.Fatalf("openCodeEnv() error = %v, want nil", err)
	}
	if password != "inherited" {
		t.Fatalf("password = %q, want the inherited OPENCODE_PASSWORD", password)
	}
	if _, ok := env["OPENCODE_PASSWORD"]; ok {
		t.Fatal("an empty OPENCODE_PASSWORD reached the child env")
	}
	if _, ok := env["OPENCODE_SERVER_PASSWORD"]; ok {
		t.Fatal("an empty OPENCODE_SERVER_PASSWORD reached the child env")
	}
	if env["OPENCODE_LOG"] != "debug" {
		t.Fatalf("unrelated env entry was dropped: %v", env)
	}
}
