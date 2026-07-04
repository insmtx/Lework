package scheduler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/insmtx/Leros/backend/internal/worker"
)

func TestProcessSchedulerHealthRequiresHTTPReady(t *testing.T) {
	scheduler := NewProcessScheduler(nil).(*ProcessScheduler)
	instance := &ProcessInstance{
		ID:       "worker",
		WorkerID: "worker",
		Endpoint: "http://127.0.0.1:1",
	}

	if err := scheduler.healthCheck(instance); err == nil {
		t.Fatal("expected health check to fail before worker HTTP server is ready")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	instance.Endpoint = server.URL
	if err := scheduler.healthCheck(instance); err != nil {
		t.Fatalf("healthCheck failed: %v", err)
	}
}

func TestProcessWorkerEndpointDefaultsToPerWorkerPort(t *testing.T) {
	endpoint := processWorkerEndpoint(&worker.WorkerSpec{WorkerID: 7}, nil)
	if endpoint != "http://127.0.0.1:18087" {
		t.Fatalf("endpoint = %q, want http://127.0.0.1:18087", endpoint)
	}
}
