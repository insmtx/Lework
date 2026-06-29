package provider

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"
)

const versionDetectTimeout = 2 * time.Second

// CLIToolSpec describes a built-in external AI coding CLI.
type CLIToolSpec struct {
	Name         string
	DisplayName  string
	Binary       string
	InstallCmd   string
	DetectCmd    string
	VersionRegex string
	Default      bool
}

// BuiltinCLITools lists the external AI coding CLIs Leros can detect.
var BuiltinCLITools = []CLIToolSpec{
	{
		Name:         "claude",
		DisplayName:  "Claude Code",
		Binary:       "claude",
		InstallCmd:   "npm install -g @anthropic-ai/claude-code",
		DetectCmd:    "claude --version",
		VersionRegex: `Claude CLI[,:]? v?(\d+\.\d+\.\d+)`,
		Default:      true,
	},
	{
		Name:         "codex",
		DisplayName:  "Codex CLI",
		Binary:       "codex",
		InstallCmd:   "npm install -g @openai/codex",
		DetectCmd:    "codex --version",
		VersionRegex: `(\d+\.\d+\.\d+)`,
		Default:      false,
	},
	{
		Name:         "opencode",
		DisplayName:  "OpenCode",
		Binary:       "opencode",
		InstallCmd:   "npm install -g opencode-ai",
		DetectCmd:    "opencode --version",
		VersionRegex: `(\d+\.\d+\.\d+)`,
		Default:      false,
	},
}

// CLIToolStatus reports whether a built-in CLI is available in the current environment.
type CLIToolStatus struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Binary      string `json:"binary"`
	Installed   bool   `json:"installed"`
	Version     string `json:"version,omitempty"`
	Path        string `json:"path,omitempty"`
	InstallCmd  string `json:"install_cmd"`
	Default     bool   `json:"is_default"`
}

// DiscoverAvailableCLI detects all built-in CLI tools from the current PATH.
func DiscoverAvailableCLI() []CLIToolStatus {
	var mu sync.Mutex
	results := make([]CLIToolStatus, 0, len(BuiltinCLITools))
	var wg sync.WaitGroup

	for _, spec := range BuiltinCLITools {
		wg.Add(1)
		go func(s CLIToolSpec) {
			defer wg.Done()
			status := detectSingleCLI(s)
			mu.Lock()
			results = append(results, status)
			mu.Unlock()
		}(spec)
	}

	wg.Wait()

	// Keep the preferred runtime first while preserving relative order otherwise.
	sortByDefault(results)

	return results
}

func detectSingleCLI(spec CLIToolSpec) CLIToolStatus {
	status := CLIToolStatus{
		Name:        spec.Name,
		DisplayName: spec.DisplayName,
		Binary:      spec.Binary,
		InstallCmd:  spec.InstallCmd,
		Default:     spec.Default,
	}

	path, err := exec.LookPath(spec.Binary)
	if err != nil {
		logs.Debugf("CLI tool %q not found in PATH: %v", spec.Binary, err)
		return status
	}

	status.Installed = true
	status.Path = path

	version, err := getCLIVersion(spec)
	if err != nil {
		logs.Warnf("Failed to get version for %q: %v", spec.Binary, err)
		status.Version = "unknown"
	} else {
		status.Version = version
	}

	logs.Infof("Detected CLI tool: %s (%s) v%s at %s",
		status.DisplayName, status.Name, status.Version, status.Path)

	return status
}

func getCLIVersion(spec CLIToolSpec) (string, error) {
	if spec.DetectCmd == "" {
		return "", nil
	}

	parts := splitCommand(spec.DetectCmd)
	if len(parts) == 0 {
		return "", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionDetectTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ctx.Err()
		}
		return "", err
	}

	outputStr := strings.TrimSpace(string(output))

	if spec.VersionRegex != "" {
		re := regexp.MustCompile(spec.VersionRegex)
		matches := re.FindStringSubmatch(outputStr)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}

	return outputStr, nil
}

func splitCommand(cmd string) []string {
	var result []string
	var current []rune
	inQuote := false
	var quoteChar rune

	for _, ch := range cmd {
		if !inQuote && (ch == '"' || ch == '\'') {
			inQuote = true
			quoteChar = ch
		} else if inQuote && ch == quoteChar {
			inQuote = false
		} else if !inQuote && ch == ' ' {
			if len(current) > 0 {
				result = append(result, string(current))
				current = nil
			}
		} else {
			current = append(current, ch)
		}
	}

	if len(current) > 0 {
		result = append(result, string(current))
	}

	return result
}

func sortByDefault(statuses []CLIToolStatus) {
	sort.SliceStable(statuses, func(i, j int) bool {
		return statuses[i].Default && !statuses[j].Default
	})
}

// PreferredCLIName chooses the preferred installed runtime from detected CLI status.
func PreferredCLIName(available []CLIToolStatus) string {
	for _, s := range available {
		if s.Installed && s.Default {
			return s.Name
		}
	}

	for _, s := range available {
		if s.Installed {
			return s.Name
		}
	}

	return ""
}

// GetCLIToolSpec returns the built-in CLI spec with the given runtime name.
func GetCLIToolSpec(name string) *CLIToolSpec {
	for i := range BuiltinCLITools {
		if BuiltinCLITools[i].Name == name {
			return &BuiltinCLITools[i]
		}
	}
	return nil
}

// SkillDirForCLI returns the conventional skill directory for a supported CLI.
func SkillDirForCLI(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude":
		return filepath.Join(home, ".claude", "skills")
	case "codex":
		return filepath.Join(home, ".agents", "skills")
	case "opencode":
		return filepath.Join(home, ".config", "opencode", "skills")
	default:
		return ""
	}
}
