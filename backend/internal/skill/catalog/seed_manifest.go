package catalog

import (
	"fmt"
	"strings"
)

// ParseSeedManifest parses the content of a .seed-manifest file into a map of
// skillName → hash. Each non-empty line must be "name:hash". Malformed lines
// are skipped and returned as a slice of warning strings for callers to log.
func ParseSeedManifest(data []byte) (entries map[string]string, warnings []string) {
	entries = make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			warnings = append(warnings, fmt.Sprintf("invalid line in seed manifest: %s", line))
			continue
		}
		entries[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return
}
