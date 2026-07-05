package process

import "fmt"

// ResolveRunWorkDir returns the configured run workdir.
func ResolveRunWorkDir(workDir string) (string, error) {
	if workDir == "" {
		return "", fmt.Errorf("no workdir configured")
	}
	return workDir, nil
}
