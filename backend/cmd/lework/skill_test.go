package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSkillCommandUsesCodeAndHasOnlyFirstVersionOperations(t *testing.T) {
	cmd := newSkillCommand()
	wantUses := map[string]string{
		"ls":     "ls",
		"add":    "add <skill_code>",
		"remove": "remove <skill_code>",
	}
	if len(cmd.Commands()) != len(wantUses) {
		t.Fatalf("skill subcommands = %d, want %d", len(cmd.Commands()), len(wantUses))
	}
	for _, sub := range cmd.Commands() {
		want, ok := wantUses[sub.Name()]
		if !ok {
			t.Fatalf("unexpected skill subcommand %q", sub.Name())
		}
		if sub.Use != want {
			t.Fatalf("%s use = %q, want %q", sub.Name(), sub.Use, want)
		}
		if len(sub.Aliases) != 0 {
			t.Fatalf("%s aliases = %#v, want none", sub.Name(), sub.Aliases)
		}
	}
}

func TestSkillMutationRequiresProjectID(t *testing.T) {
	err := runSkillMutation(&cobra.Command{}, false, true, "", "bid-review")
	if err == nil || !strings.Contains(err.Error(), "--project-id is required") {
		t.Fatalf("runSkillMutation() error = %v", err)
	}
}
