//go:build windows

package supervisor

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnsupported reports a platform capability that lands after I0. Callers can
// test for it with errors.Is and fall back to an externally managed service.
var ErrUnsupported = errors.New("agentd: unsupported platform operation")

// newProcess is a compile-safe placeholder: process groups have no Windows
// equivalent in this iteration, so spawning is refused rather than silently
// behaving differently from Unix.
func newProcess(context.Context, Spec) (Process, error) {
	return nil, fmt.Errorf("agentd: process supervision not implemented on windows: %w", ErrUnsupported)
}
