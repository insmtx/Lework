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

func TestLoadOrCreatePasswordPersistsValue(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	got, err := LoadOrCreatePassword("first")
	if err != nil {
		t.Fatalf("LoadOrCreatePassword() error = %v", err)
	}
	if got != "first" {
		t.Fatalf("LoadOrCreatePassword() = %q, want first", got)
	}

	again, err := LoadOrCreatePassword("second")
	if err != nil {
		t.Fatalf("LoadOrCreatePassword() second error = %v", err)
	}
	if again != "first" {
		t.Fatalf("LoadOrCreatePassword() = %q, want the persisted first", again)
	}

	path := filepath.Join(stateHome, "lework", "opencode-password")
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat password file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("password file mode = %o, want 600", got)
	}
}

func TestLoadOrCreatePasswordRejectsEmptyCandidate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if _, err := LoadOrCreatePassword(""); err == nil {
		t.Fatal("LoadOrCreatePassword() error = nil, want a validation error")
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

func TestReadOpenCodePassesThroughSharedService(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := filepath.Join(stateHome, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create opencode state dir: %v", err)
	}
	payload := `{"id":"svc","version":"2.2.7","url":"http://127.0.0.1:49374","pid":42,` +
		`"password":"shared-secret"}`
	if err := os.WriteFile(filepath.Join(dir, "service.json"), []byte(payload), 0o600); err != nil {
		t.Fatalf("write opencode service file: %v", err)
	}

	got, err := ReadOpenCode()
	if err != nil {
		t.Fatalf("ReadOpenCode() error = %v", err)
	}
	if got.URL != "http://127.0.0.1:49374" || got.Password != "shared-secret" {
		t.Fatalf("ReadOpenCode() = %+v, want the registered endpoint", got)
	}
	if got.PID != 42 {
		t.Fatalf("ReadOpenCode().PID = %d, want 42", got.PID)
	}
}

func TestReadOpenCodeMissingFileIsNotExist(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	if _, err := ReadOpenCode(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ReadOpenCode() error = %v, want fs.ErrNotExist", err)
	}
}

// 老版本只写密码、不写 URL；这种文件不能拿来 attach，必须当成不可用，
// 否则 agent 会拿空地址去健康检查。
func TestReadOpenCodeRejectsMissingURL(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	dir := filepath.Join(stateHome, "opencode")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create opencode state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.json"), []byte(`{"password":"secret"}`), 0o600); err != nil {
		t.Fatalf("write opencode service file: %v", err)
	}

	if _, err := ReadOpenCode(); err == nil {
		t.Fatal("ReadOpenCode() error = nil, want invalid endpoint error")
	}
}
