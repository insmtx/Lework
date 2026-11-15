package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandHasAgent(t *testing.T) {
	cmd := newRootCommand()
	agentCmd, _, err := cmd.Find([]string{"agent"})
	if err != nil || agentCmd == nil || agentCmd.Name() != "agent" {
		t.Fatalf("agent command missing: %v", err)
	}
}

func TestAgentCommandIsRunnable(t *testing.T) {
	cmd := newAgentCommand()
	if cmd.Use != "agent" {
		t.Fatalf("agent command Use = %q, want agent", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Fatal("agent command must start the daemon when invoked without a subcommand")
	}
}

func TestAgentCommandHasVersionSubcommand(t *testing.T) {
	cmd := newAgentCommand()
	versionCmd, _, err := cmd.Find([]string{"version"})
	if err != nil || versionCmd == nil || versionCmd.Name() != "version" {
		t.Fatalf("version command missing: %v", err)
	}
}

func TestAgentVersionCommandPrintsVersion(t *testing.T) {
	cmd := newAgentCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version: %v", err)
	}
	if want := "lework agent " + version + "\n"; out.String() != want {
		t.Fatalf("version output = %q, want %q", out.String(), want)
	}
}

func TestAgentVersionFlagUsesUnifiedName(t *testing.T) {
	cmd := newAgentCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if want := "lework agent " + version + "\n"; out.String() != want {
		t.Fatalf("version flag output = %q, want %q", out.String(), want)
	}
}

func TestRootVersionFlagUsesUnifiedName(t *testing.T) {
	cmd := newRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute --version: %v", err)
	}
	if want := "lework " + version + "\n"; out.String() != want {
		t.Fatalf("version flag output = %q, want %q", out.String(), want)
	}
}

func TestAgentRequiresConfig(t *testing.T) {
	cmd := newAgentCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("err = %v, want config error", err)
	}
}

func TestAgentRejectsIncompleteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("node_name: test-node\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newAgentCommand()
	cmd.SetArgs([]string{"--config", path})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "control_plane_url") {
		t.Fatalf("err = %v, want control_plane_url validation error", err)
	}
}

func TestAgentConfigFlagDefaultsToPlatformPath(t *testing.T) {
	flag := newAgentCommand().Flags().Lookup("config")
	if flag == nil {
		t.Fatal("--config flag missing")
	}
	if flag.DefValue != resolveConfigPath() {
		t.Fatalf("--config default = %q, want %q", flag.DefValue, resolveConfigPath())
	}
}

func TestRootAgentConfigFlagIsAgentConfig(t *testing.T) {
	agentCmd, _, err := newRootCommand().Find([]string{"agent"})
	if err != nil || agentCmd == nil {
		t.Fatalf("find agent: %v", err)
	}
	flag := agentCmd.Flags().Lookup("config")
	if flag == nil {
		t.Fatal("agent --config flag missing")
	}
	if flag.Usage != "path to lework.yaml or agent.yaml" {
		t.Fatalf("agent --config usage = %q, want agent config, not legacy CLI config", flag.Usage)
	}
}

func TestIsStandaloneCommand(t *testing.T) {
	root := newRootCommand()
	for _, args := range [][]string{{"agent"}, {"agent", "version"}, {"serve"}} {
		cmd, _, err := root.Find(args)
		if err != nil || cmd == nil {
			t.Fatalf("find %v: %v", args, err)
		}
		if !isStandaloneCommand(cmd) {
			t.Fatalf("%v was not recognized as a standalone command", args)
		}
	}
	server, _, err := root.Find([]string{"server"})
	if err != nil || server == nil {
		t.Fatalf("find server: %v", err)
	}
	if isStandaloneCommand(server) {
		t.Fatal("server was incorrectly recognized as a standalone command")
	}
}
