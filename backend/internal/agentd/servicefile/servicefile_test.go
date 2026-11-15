package servicefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	info := Info{URL: "http://127.0.0.1:4096", Password: "secret", Port: 4096, PID: 42}
	if err := Write(info); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat service file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("service file mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat service dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("service dir mode = %o, want 700", got)
	}

	got, err := Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Updated.IsZero() {
		t.Fatal("Read().Updated is zero, want the write timestamp")
	}
	got.Updated = info.Updated
	if got != info {
		t.Fatalf("Read() = %+v, want %+v", got, info)
	}
}

func TestWriteRejectsMissingURLOrPassword(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tests := []struct {
		name string
		info Info
	}{
		{name: "missing url", info: Info{Password: "secret"}},
		{name: "missing password", info: Info{URL: "http://127.0.0.1:4096"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Write(tt.info); err == nil {
				t.Fatal("Write() error = nil, want validation error")
			}
		})
	}
}

func TestReadMissingFileIsNotExist(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if _, err := Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read() error = %v, want fs.ErrNotExist", err)
	}
}

func TestReadRejectsInvalidContent(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create service dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write invalid service file: %v", err)
	}

	_, err = Read()
	if err == nil {
		t.Fatal("Read() error = nil, want invalid content error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("Read() error = %v, want an invalid-content error", err)
	}
}

func TestRemoveIsIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if err := Remove(); err != nil {
		t.Fatalf("Remove() on missing file error = %v, want nil", err)
	}
	if err := Write(Info{URL: "http://127.0.0.1:4096", Password: "secret", Port: 4096}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Remove(); err != nil {
			t.Fatalf("Remove() call %d error = %v, want nil", i+1, err)
		}
	}
}

func TestPathUsesXDGStateHome(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	want := filepath.Join(stateHome, "lework", "opencode-service.json")
	if path != want {
		t.Fatalf("Path() = %q, want %q", path, want)
	}
}

func TestPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)

	path, err := Path()
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}
	want := filepath.Join(home, ".local", "state", "lework", "opencode-service.json")
	if path != want {
		t.Fatalf("Path() = %q, want %q", path, want)
	}
}
