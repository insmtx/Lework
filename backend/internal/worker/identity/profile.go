package identity

import (
	"fmt"
	"os"
	"sync/atomic"
)

// Profile records process-wide worker identity and endpoint metadata.
type Profile struct {
	OrgID      uint
	WorkerID   uint
	ServerAddr string
	WorkerAddr string
}

var profile atomic.Value

// Set records process-wide worker profile.
func Set(value Profile) {
	profile.Store(value)
}

// Get returns the worker profile bound to this process.
func Get() Profile {
	value := profile.Load()
	if value == nil {
		return Profile{}
	}
	return value.(Profile)
}

// OrgID returns the organization ID bound to this worker process.
func OrgID() uint {
	return Get().OrgID
}

// WorkerID returns the worker ID bound to this worker process.
func WorkerID() uint {
	return Get().WorkerID
}

// ServerAddr returns the server address this worker connects to.
func ServerAddr() string {
	return Get().ServerAddr
}

// WorkerAddr returns the worker HTTP service address.
func WorkerAddr() string {
	return Get().WorkerAddr
}

// GitAuthorEnv returns environment variables that set the git author and committer
// identity to the current worker's profile.
func GitAuthorEnv() []string {
	p := Get()
	name := fmt.Sprintf("leros-worker-o%d-w%d", p.OrgID, p.WorkerID)
	email := fmt.Sprintf("worker-w%d@org%d.leros.local", p.WorkerID, p.OrgID)
	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+email,
	)
}
