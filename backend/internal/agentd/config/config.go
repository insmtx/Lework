// Package config loads and validates the lework-agent daemon configuration
// from agent.yaml. Defaults are filled in by Load so that a config file only
// needs to carry deployment-specific values.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Defaults applied by Load when a field is left empty.
const (
	defaultOpenCodeBinary = "opencode"
	defaultOpenCodePort   = 4096
	defaultServiceMode    = "shared"
)

// Config is the agent daemon configuration read from agent.yaml.
type Config struct {
	// ControlPlaneURL is the WebSocket endpoint of the control plane.
	ControlPlaneURL string `yaml:"control_plane_url"`
	// EnrollmentToken authenticates the node on first registration.
	EnrollmentToken string `yaml:"enrollment_token"`
	// NodeName is the human-readable identity of this node.
	NodeName string `yaml:"node_name"`
	// TLSFingerprint pins the control-plane certificate when the URL is wss.
	TLSFingerprint string `yaml:"tls_fingerprint"`
	// WorkspaceRoots lists the absolute directories the agent may serve.
	// session.create rejects an empty or out-of-root directory when it is set.
	WorkspaceRoots []string `yaml:"workspace_roots"`
	// OpenCode configures the local opencode process.
	OpenCode OpenCodeConfig `yaml:"opencode"`
	// WorktreeRoot is where git worktrees are created.
	WorktreeRoot string `yaml:"worktree_root"`
	// RepoCacheRoot is where cloned repositories are cached.
	RepoCacheRoot string `yaml:"repo_cache_root"`
}

// OpenCodeConfig controls how the agent launches and reaches opencode.
type OpenCodeConfig struct {
	// Binary is the opencode executable name or absolute path.
	Binary string `yaml:"binary"`
	// Port is the local HTTP port opencode listens on.
	Port int `yaml:"port"`
	// ServiceMode is "shared" (one process for all projects) or
	// "per-project" (one process per project).
	ServiceMode string `yaml:"service_mode"`
	// Env holds extra environment variables for the opencode process.
	Env map[string]string `yaml:"env"`
}

// Load reads agent.yaml from path, applies defaults, and returns the config.
// It does not call Validate: callers decide when a config is complete enough
// to be checked, which keeps partially-specified test configs usable.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse agent config %s: %w", path, err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// Default returns a Config with the documented defaults applied, the same
// values Load fills for a config file. The combined-process startup uses it
// when no agent.yaml exists, so OpenCode defaults stay in this package.
func Default() Config {
	var cfg Config
	cfg.applyDefaults()
	return cfg
}

// ApplyDefaults fills empty fields with the documented defaults.
//
// Load already applies them for agent.yaml; the unified config loader decodes the
// agent section itself, so it calls this instead of duplicating the defaults.
func (c *Config) ApplyDefaults() {
	c.applyDefaults()
}

// applyDefaults fills empty fields with the documented defaults. Home-relative
// defaults are skipped when the home directory cannot be determined; Validate
// still accepts an explicit path in that case.
func (c *Config) applyDefaults() {
	if c.OpenCode.Binary == "" {
		c.OpenCode.Binary = defaultOpenCodeBinary
	}
	if c.OpenCode.Port == 0 {
		c.OpenCode.Port = defaultOpenCodePort
	}
	if c.OpenCode.ServiceMode == "" {
		c.OpenCode.ServiceMode = defaultServiceMode
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if c.WorktreeRoot == "" {
		c.WorktreeRoot = filepath.Join(home, ".lework", "worktrees")
	}
	if c.RepoCacheRoot == "" {
		c.RepoCacheRoot = filepath.Join(home, ".lework", "cache", "repos")
	}
}

// Validate reports whether the configuration can be used to run the agent.
// Errors name the offending field so operators can fix agent.yaml directly.
func (c *Config) Validate() error {
	if c.ControlPlaneURL == "" {
		return fmt.Errorf("config: control_plane_url is required")
	}
	u, err := url.Parse(c.ControlPlaneURL)
	if err != nil {
		return fmt.Errorf("config: control_plane_url is invalid: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("config: control_plane_url scheme must be ws or wss, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("config: control_plane_url must include a host")
	}
	if strings.TrimSpace(c.NodeName) == "" {
		return fmt.Errorf("config: node_name is required")
	}
	if len(c.WorkspaceRoots) == 0 {
		return fmt.Errorf("config: workspace_roots must list at least one directory")
	}
	for _, root := range c.WorkspaceRoots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("config: workspace root %q must be absolute", root)
		}
	}
	switch c.OpenCode.ServiceMode {
	case "shared", "per-project":
	default:
		return fmt.Errorf("config: opencode.service_mode must be shared or per-project, got %q", c.OpenCode.ServiceMode)
	}
	if c.OpenCode.Port < 1 || c.OpenCode.Port > 65535 {
		return fmt.Errorf("config: opencode.port %d must be between 1 and 65535", c.OpenCode.Port)
	}
	return nil
}
