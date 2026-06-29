package provider

import (
	"os"
	"sort"
	"strings"
)

// BuildBaseEnv 返回当前环境并附加额外的环境变量。
func BuildBaseEnv(extraEnv map[string]string) []string {
	builder := newEnvBuilder(os.Environ())
	builder.applyMap(extraEnv)
	return builder.slice()
}

// BuildRunEnv 为 CLI 进程组装环境变量条目。
func BuildRunEnv(baseEnv []string, extraEnv []string, modelEnv map[string]string) []string {
	builder := newEnvBuilder(baseEnv)
	builder.applyEntries(extraEnv)
	builder.applyMap(modelEnv)
	return builder.slice()
}

type envBuilder struct {
	values map[string]string
}

func newEnvBuilder(entries []string) *envBuilder {
	builder := &envBuilder{
		values: make(map[string]string, len(entries)),
	}
	builder.applyEntries(entries)
	return builder
}

func (b *envBuilder) applyMap(entries map[string]string) {
	for key, value := range entries {
		key = strings.TrimSpace(key)
		if key == "" || value == "" {
			continue
		}
		b.values[key] = value
	}
}

func (b *envBuilder) applyEntries(entries []string) {
	for _, entry := range entries {
		key, value, ok := splitEnvEntry(entry)
		if !ok {
			continue
		}
		b.values[key] = value
	}
}

func (b *envBuilder) slice() []string {
	keys := make([]string, 0, len(b.values))
	for key := range b.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+b.values[key])
	}
	return env[:len(env):len(env)]
}

func splitEnvEntry(entry string) (key string, value string, ok bool) {
	parts := strings.SplitN(entry, "=", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key = strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false
	}
	return key, parts[1], true
}
