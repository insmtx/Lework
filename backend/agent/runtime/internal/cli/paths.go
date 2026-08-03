package cli

import "strings"

// TaskRuntimeRoot returns the task-private root shared by every CLI runtime.
func TaskRuntimeRoot(taskDir, workDir string) string {
	if root := strings.TrimSpace(taskDir); root != "" {
		return root
	}
	return strings.TrimSpace(workDir)
}
