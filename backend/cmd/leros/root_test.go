package main

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		level string
		want  zapcore.Level
		valid bool
	}{
		{name: "empty defaults to info", want: zapcore.InfoLevel, valid: true},
		{name: "debug", level: "debug", want: zapcore.DebugLevel, valid: true},
		{name: "info", level: "info", want: zapcore.InfoLevel, valid: true},
		{name: "warn", level: "warn", want: zapcore.WarnLevel, valid: true},
		{name: "error", level: "error", want: zapcore.ErrorLevel, valid: true},
		{name: "trimmed and case insensitive", level: " WARN ", want: zapcore.WarnLevel, valid: true},
		{name: "invalid defaults to info", level: "trace", want: zapcore.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envDev, "")
			got, valid := resolveLogLevel(tt.level)
			if got != tt.want {
				t.Fatalf("resolveLogLevel(%q) = %s, want %s", tt.level, got, tt.want)
			}
			if valid != tt.valid {
				t.Fatalf("resolveLogLevel(%q) valid = %v, want %v", tt.level, valid, tt.valid)
			}
		})
	}
}

func TestResolveLogLevelUsesDevAsFallback(t *testing.T) {
	t.Setenv(envDev, "true")
	got, valid := resolveLogLevel("")
	if got != zapcore.DebugLevel {
		t.Fatalf("empty log level with LEROS_DEV = %s, want debug", got)
	}
	if !valid {
		t.Fatal("empty log level should be valid")
	}

	got, valid = resolveLogLevel("error")
	if got != zapcore.ErrorLevel {
		t.Fatalf("configured log level with LEROS_DEV = %s, want error", got)
	}
	if !valid {
		t.Fatal("configured error log level should be valid")
	}
}

func TestRootCommandHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "long help flag", args: []string{"--help"}},
		{name: "short help flag", args: []string{"-h"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeRootCommand(tt.args...)
			if err != nil {
				t.Fatalf("execute help: %v", err)
			}
			for _, expected := range []string{"server", "worker", "skill", "login", "logout", "session"} {
				if !strings.Contains(output, expected) {
					t.Fatalf("help output missing %q:\n%s", expected, output)
				}
			}
		})
	}
}

func TestHelpCommandIsNotSupported(t *testing.T) {
	output, err := executeRootCommand("help")
	if err == nil {
		t.Fatalf("expected help command to fail:\n%s", output)
	}
	if !strings.Contains(err.Error(), `unknown command "help"`) {
		t.Fatalf("unexpected help command error: %v", err)
	}
}

func TestCommandHelpFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "skill long help",
			args:     []string{"skill", "--help"},
			expected: []string{"Search, install, list, and uninstall skills", "install", "search"},
		},
		{
			name:     "skill short help",
			args:     []string{"skill", "-h"},
			expected: []string{"Search, install, list, and uninstall skills", "install", "search"},
		},
		{
			name:     "login help",
			args:     []string{"login", "--help"},
			expected: []string{"Log in to Leros"},
		},
		{
			name:     "logout help",
			args:     []string{"logout", "--help"},
			expected: []string{"Log out of Leros"},
		},
		{
			name:     "session help",
			args:     []string{"session", "--help"},
			expected: []string{"Manage sessions"},
		},
		{
			name:     "server help",
			args:     []string{"server", "--help"},
			expected: []string{"Start the HTTP server that handles API requests and publishes external events."},
		},
		{
			name:     "worker help",
			args:     []string{"worker", "--help"},
			expected: []string{"Start the background worker service for processing asynchronous tasks and events.", "codex", "claude", "--default-runtime"},
		},
		{
			name:     "worker codex help",
			args:     []string{"worker", "codex", "--help"},
			expected: []string{"Start a standalone Leros worker that subscribes to org.{org_id}.worker.{worker_id}.task"},
		},
		{
			name:     "worker claude help",
			args:     []string{"worker", "claude", "--help"},
			expected: []string{"Start a standalone Leros worker that subscribes to org.{org_id}.worker.{worker_id}.task"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeRootCommand(tt.args...)
			if err != nil {
				t.Fatalf("execute help %v: %v", tt.args, err)
			}
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Fatalf("help output missing %q:\n%s", expected, output)
				}
			}
		})
	}
}

func TestWorkerShortHelp(t *testing.T) {
	output, err := executeRootCommand("worker", "-h")
	if err != nil {
		t.Fatalf("execute worker help: %v", err)
	}
	for _, expected := range []string{"codex", "claude", "--default-runtime"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("worker help output missing %q:\n%s", expected, output)
		}
	}
}

func TestLegacyWorkerRuntimeCommandsAreNotSupported(t *testing.T) {
	for _, args := range [][]string{
		{"worker", "codex-worker"},
		{"worker", "claude-worker"},
	} {
		output, err := executeRootCommand(args...)
		if err == nil {
			t.Fatalf("expected %v to fail:\n%s", args, output)
		}
		if !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("unexpected error for %v: %v", args, err)
		}
	}
}

func TestPlaceholderCommands(t *testing.T) {
	tests := []struct {
		args     []string
		expected []string
	}{
		{
			args:     []string{"login"},
			expected: []string{"leros login is not implemented yet"},
		},
		{
			args:     []string{"logout"},
			expected: []string{"leros logout is not implemented yet"},
		},
	}

	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			output, err := executeRootCommand(tt.args...)
			if err != nil {
				t.Fatalf("execute %v: %v", tt.args, err)
			}
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Fatalf("output missing %q:\n%s", expected, output)
				}
			}
		})
	}
}

func TestNewRootCommandDoesNotDuplicateCommands(t *testing.T) {
	for i := 0; i < 2; i++ {
		cmd := newRootCommand()
		seen := make(map[string]bool)
		for _, child := range cmd.Commands() {
			name := child.Name()
			if seen[name] {
				t.Fatalf("duplicate root command %q", name)
			}
			seen[name] = true
		}
	}
}

func TestSkillCommands(t *testing.T) {
	t.Run("install no args", func(t *testing.T) {
		_, err := executeRootCommand("skill", "install")
		if err == nil {
			t.Fatal("expected error for install with no args")
		}
		if !strings.Contains(err.Error(), "accepts 1 arg") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("search no args", func(t *testing.T) {
		_, err := executeRootCommand("skill", "search")
		if err == nil {
			t.Fatal("expected error for search with no args")
		}
		if !strings.Contains(err.Error(), "accepts 1 arg") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("install help", func(t *testing.T) {
		output, err := executeRootCommand("skill", "install", "--help")
		if err != nil {
			t.Fatalf("install help: %v", err)
		}
		for _, expected := range []string{"Install a skill", "identifier", "--force", "--yes"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("install help missing %q:\n%s", expected, output)
			}
		}
	})

	t.Run("search help", func(t *testing.T) {
		output, err := executeRootCommand("skill", "search", "--help")
		if err != nil {
			t.Fatalf("search help: %v", err)
		}
		for _, expected := range []string{"Search for skills", "--limit", "--json"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("search help missing %q:\n%s", expected, output)
			}
		}
	})

	t.Run("invalid short name", func(t *testing.T) {
		_, err := executeRootCommand("skill", "install", "nonexistent-skill-xyz-123")
		if err == nil {
			t.Fatal("expected error for nonexistent skill")
		}
	})

	t.Run("root help includes skill", func(t *testing.T) {
		output, err := executeRootCommand("--help")
		if err != nil {
			t.Fatalf("root help: %v", err)
		}
		for _, expected := range []string{"skill", "Manage skills"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("root help missing %q:\n%s", expected, output)
			}
		}
	})
}

func executeRootCommand(args ...string) (string, error) {
	cmd := newRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}
