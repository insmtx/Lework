// Package servicefile publishes the local OpenCode service endpoint owned by
// the lework agent so a same-user TUI can attach to it.
//
// The file carries the HTTP basic-auth password, so it is written 0600 and its
// directory 0700. Passwords must never be logged or echoed in errors; callers
// only pass the value into the child environment.
package servicefile

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileName is fixed so every lework process on the node agrees on the location
// without additional discovery.
const fileName = "opencode-service.json"

// passwordFileName persists a generated password across agent restarts. A
// running TUI keeps the password from its own environment, so a fresh value
// would leave it retrying forever with "Authentication required".
const passwordFileName = "opencode-password"

// Info is the published endpoint and credential of the agent-owned OpenCode
// service. PID identifies the agent process that wrote it, so a second agent
// does not remove another instance's file on shutdown.
type Info struct {
	URL      string    `json:"url"`
	Password string    `json:"password"`
	Port     int       `json:"port"`
	PID      int       `json:"pid"`
	Updated  time.Time `json:"updated"`
}

// Path returns the per-user state file path. XDG_STATE_HOME wins when set;
// otherwise the XDG default under the home directory is used. A node without a
// resolvable home directory cannot publish or read the endpoint.
func Path() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); dir != "" {
		return filepath.Join(dir, "lework", fileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for opencode service file: %w", err)
	}
	return filepath.Join(home, ".local", "state", "lework", fileName), nil
}

// Write publishes info atomically: the temporary file is created 0600 in the
// destination directory and renamed over the previous file, so a concurrent
// reader never observes a half-written endpoint.
func Write(info Info) error {
	if strings.TrimSpace(info.URL) == "" {
		return errors.New("servicefile: url is required")
	}
	if info.Password == "" {
		return errors.New("servicefile: password is required")
	}
	if info.Updated.IsZero() {
		info.Updated = time.Now().UTC()
	}

	path, err := Path()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("encode service file: %w", err)
	}
	data = append(data, '\n')

	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write service file %s: %w", path, err)
	}
	return nil
}

// LoadOrCreatePassword returns the persisted generated password, storing
// candidate on first use. It exists so a restarted agent keeps the same
// credential and an attached TUI can reconnect without being relaunched.
func LoadOrCreatePassword(candidate string) (string, error) {
	if candidate == "" {
		return "", errors.New("servicefile: password is required")
	}
	path, err := Path()
	if err != nil {
		return "", err
	}
	passwordPath := filepath.Join(filepath.Dir(path), passwordFileName)

	data, err := os.ReadFile(passwordPath)
	if err == nil {
		if saved := strings.TrimSpace(string(data)); saved != "" {
			return saved, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read opencode password %s: %w", passwordPath, err)
	}

	if err := writeAtomic(passwordPath, []byte(candidate+"\n")); err != nil {
		return "", fmt.Errorf("store opencode password %s: %w", passwordPath, err)
	}
	return candidate, nil
}

// writeAtomic replaces path with data through a 0600 temp file in the same
// directory, so a concurrent reader never observes a partial value.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Read loads the published endpoint. A missing file is reported as
// fs.ErrNotExist so callers can tell "agent not running" from a broken file.
func Read() (Info, error) {
	path, err := Path()
	if err != nil {
		return Info{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, fmt.Errorf("read service file %s: %w", path, err)
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, fmt.Errorf("decode service file %s: %w", path, err)
	}
	if strings.TrimSpace(info.URL) == "" || info.Password == "" {
		return Info{}, fmt.Errorf("decode service file %s: invalid service file", path)
	}
	return info, nil
}

// Remove deletes the published endpoint. It is safe to call when the file is
// already gone, which keeps agent shutdown idempotent.
func Remove() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove service file %s: %w", path, err)
	}
	return nil
}
