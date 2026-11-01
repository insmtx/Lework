package supervisor

import (
	"context"
	"errors"
	"strings"
)

// Process is one supervised child process. Start hands the lifetime of the
// process to ctx: canceling ctx or calling Stop must both terminate it.
type Process interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Wait() error
}

// Spec is a fully-resolved child process description. It carries no secrets:
// callers should pass credentials through Env, never through Args, because
// arguments are visible in the process table.
type Spec struct {
	Binary string
	Args   []string
	Env    []string
	Dir    string
}

// Spawner creates a Process without starting it. It exists so the supervisor
// can be tested without launching a real opencode binary.
type Spawner interface {
	Spawn(ctx context.Context, spec Spec) (Process, error)
}

// ExecSpawner is the production Spawner backed by os/exec. Whether a process
// can be supervised at all is platform-dependent; unsupported platforms report
// ErrUnsupported from Spawn.
type ExecSpawner struct{}

// Spawn validates the spec and returns a process that is not yet running.
func (ExecSpawner) Spawn(ctx context.Context, spec Spec) (Process, error) {
	if spec.Binary == "" {
		return nil, errors.New("agentd: spawn spec requires a binary")
	}
	return newProcess(ctx, spec)
}

// mergeEnv overlays extra onto base. Duplicate keys are collapsed so an
// operator-supplied value replaces the inherited one instead of being passed
// to the child twice, where which copy wins would be platform-dependent.
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}

	overridden := make(map[string]struct{}, len(extra))
	for _, kv := range extra {
		if key, ok := envKey(kv); ok {
			overridden[key] = struct{}{}
		}
	}

	merged := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		if key, ok := envKey(kv); ok {
			if _, replaced := overridden[key]; replaced {
				continue
			}
		}
		merged = append(merged, kv)
	}
	return append(merged, extra...)
}

// envKey extracts the name from a KEY=VALUE entry. Entries without a usable
// key are kept rather than dropped, because the child may still interpret them.
func envKey(kv string) (string, bool) {
	i := strings.IndexByte(kv, '=')
	if i <= 0 {
		return "", false
	}
	return kv[:i], true
}
