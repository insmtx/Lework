package seed

import (
	"context"
	"os"
	"strings"
)

// loadEnvVars 读取 os.Environ()，返回模板变量表。
func loadEnvVars(_ context.Context) (map[string]string, error) {
	env := map[string]string{}
	for _, s := range os.Environ() {
		k, v, ok := strings.Cut(s, "=")
		if ok {
			env[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return env, nil
}
