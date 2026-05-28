package steps

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/worker/identity"
)

func TestWorkerModelProxyBaseURLAppendsV1(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "empty", addr: "", want: ""},
		{name: "host port", addr: "127.0.0.1:8081", want: "http://127.0.0.1:8081/v1"},
		{name: "port only", addr: ":8081", want: "http://127.0.0.1:8081/v1"},
		{name: "http", addr: "http://127.0.0.1:8081", want: "http://127.0.0.1:8081/v1"},
		{name: "already v1", addr: "http://127.0.0.1:8081/v1/", want: "http://127.0.0.1:8081/v1"},
	}

	oldProfile := identity.Get()
	defer identity.Set(oldProfile)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity.Set(identity.Profile{WorkerAddr: tt.addr})
			if got := workerModelProxyBaseURL(); got != tt.want {
				t.Fatalf("workerModelProxyBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
