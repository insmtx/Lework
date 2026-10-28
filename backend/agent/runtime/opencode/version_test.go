package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/agent"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name   string
		output string
		wantOK bool
		want   Version
	}{
		{
			name:   "release version",
			output: "2.1.4\n",
			wantOK: true,
			want:   Version{Major: 2, Minor: 1, Patch: 4},
		},
		{
			name:   "prefixed version",
			output: "opencode v2.1.4\n",
			wantOK: true,
			want:   Version{Major: 2, Minor: 1, Patch: 4},
		},
		{
			name:   "v1 version still parses",
			output: "1.18.16\n",
			wantOK: true,
			want:   Version{Major: 1, Minor: 18, Patch: 16},
		},
		{
			name:   "garbage output",
			output: "abc",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseVersion(tt.output)
			if ok != tt.wantOK {
				t.Fatalf("ParseVersion(%q) ok = %v, want %v", tt.output, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Fatalf("ParseVersion(%q) = %+v, want %+v", tt.output, got, tt.want)
			}
		})
	}
}

func TestVersionRequireACPCompatible(t *testing.T) {
	if err := (Version{Major: 2, Minor: 1, Patch: 4}).RequireACPCompatible(); err != nil {
		t.Fatalf("2.1.4 should pass the gate: %v", err)
	}

	tests := []struct {
		name    string
		version Version
		want    string
	}{
		{
			name:    "v1 rejected with install hint",
			version: Version{Major: 1, Minor: 18, Patch: 16},
			want:    "opencode v2.1.x required, found 1.18.16; run: npm install -g @opencode/cli@2.1.4",
		},
		{
			name:    "v2.2 rejected",
			version: Version{Major: 2, Minor: 2, Patch: 0},
			want:    "opencode v2.1.x required, found 2.2.0; run: npm install -g @opencode/cli@2.1.4",
		},
		{
			name:    "v3 rejected",
			version: Version{Major: 3, Minor: 1, Patch: 0},
			want:    "opencode v2.1.x required, found 3.1.0; run: npm install -g @opencode/cli@2.1.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.version.RequireACPCompatible()
			if err == nil {
				t.Fatalf("%s should be rejected", tt.version)
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestProbeVersionRunsBinary(t *testing.T) {
	binary := writeFakeOpencode(t, `echo "2.1.4"`)

	got, err := ProbeVersion(context.Background(), binary)
	if err != nil {
		t.Fatalf("ProbeVersion: %v", err)
	}
	if got.String() != "2.1.4" {
		t.Fatalf("version = %s, want 2.1.4", got)
	}
	if !got.ACPCompatible() {
		t.Fatalf("2.1.4 should be ACP compatible")
	}
}

func TestProbeVersionRejectsUnrecognizedOutput(t *testing.T) {
	binary := writeFakeOpencode(t, `echo "abc"`)

	_, err := ProbeVersion(context.Background(), binary)
	if err == nil {
		t.Fatal("expected error for unrecognized version output")
	}
	if !strings.Contains(err.Error(), "unrecognized output") {
		t.Fatalf("error = %q, want unrecognized output", err.Error())
	}
}

func TestProbeVersionIncludesFailureDetail(t *testing.T) {
	binary := writeFakeOpencode(t, "echo 'EPERM: open log failed' >&2; exit 3")

	_, err := ProbeVersion(context.Background(), binary)
	if err == nil {
		t.Fatal("expected error for failing binary")
	}
	if !strings.Contains(err.Error(), "EPERM: open log failed") {
		t.Fatalf("error = %q, want CLI output detail", err.Error())
	}
}

func TestProbeVersionPropagatesExecFailure(t *testing.T) {
	_, err := ProbeVersion(context.Background(), filepath.Join(t.TempDir(), "missing-opencode"))
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "probe") {
		t.Fatalf("error = %q, want probe context", err.Error())
	}
}

func TestProbeVersionHonorsCanceledContext(t *testing.T) {
	binary := writeFakeOpencode(t, `sleep 5; echo "2.1.4"`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ProbeVersion(ctx, binary)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("error = %q, want context canceled", err.Error())
	}
}

func TestNewRejectsIncompatibleBinary(t *testing.T) {
	binary := writeFakeOpencode(t, `echo "1.18.16"`)

	_, err := New(binary, agent.RuntimeAdapterOptions{}, "")
	if err == nil {
		t.Fatal("New should reject opencode v1")
	}
	want := "opencode v2.1.x required, found 1.18.16; run: npm install -g @opencode/cli@2.1.4"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestNewAcceptsCompatibleBinary(t *testing.T) {
	binary := writeFakeOpencode(t, `echo "2.1.4"`)

	rt, err := New(binary, agent.RuntimeAdapterOptions{}, "")
	if err != nil {
		t.Fatalf("New should accept opencode 2.1.4: %v", err)
	}
	if rt == nil || rt.Name() != Kind {
		t.Fatalf("runtime = %v, want kind %q", rt, Kind)
	}
}

// writeFakeOpencode 生成一个只响应 --version 的假二进制，避免单测依赖本机安装。
func writeFakeOpencode(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "opencode")
	content := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}
	return path
}
