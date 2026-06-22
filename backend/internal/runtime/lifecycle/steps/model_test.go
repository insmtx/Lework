package steps

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/internal/agent"
	modelrouter "github.com/insmtx/Leros/backend/internal/modelrouter"
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
			if got := modelrouter.WorkerProxyBaseURL(); got != tt.want {
				t.Fatalf("modelrouter.WorkerProxyBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestModelStepWritesUpstreamConfigAndProxiesRequest(t *testing.T) {
	// Reset store before test
	modelStore.Put(modelrouter.UpstreamConfig{})

	oldProfile := identity.Get()
	defer identity.Set(oldProfile)
	identity.Set(identity.Profile{WorkerAddr: "127.0.0.1:8081"})

	req := &agent.RequestContext{
		Model: agent.ModelOptions{
			Provider:     "openai",
			Model:        "gpt-4.1",
			APIKey:       "sk-test-key",
			BaseURL:      "https://api.openai.com",
			BaseURLHasV1: true,
			Temperature:  0.7,
		},
	}

	step := ModelStep{}
	err := step.Run(context.Background(), &State{Request: req})
	if err != nil {
		t.Fatalf("ModelStep.Run() error = %v", err)
	}

	// Verify request BaseURL changed to proxy address
	if req.Model.BaseURL != "http://127.0.0.1:8081/v1" {
		t.Fatalf("request Model.BaseURL = %q, want %q", req.Model.BaseURL, "http://127.0.0.1:8081/v1")
	}

	// Verify request BaseURLHasV1 preserved from original request
	if !req.Model.BaseURLHasV1 {
		t.Fatal("request Model.BaseURLHasV1 should be preserved as true")
	}

	// Verify other fields unchanged
	if req.Model.Provider != "openai" {
		t.Fatalf("request Model.Provider = %q, want %q", req.Model.Provider, "openai")
	}
	if req.Model.Model != "gpt-4.1" {
		t.Fatalf("request Model.Model = %q, want %q", req.Model.Model, "gpt-4.1")
	}
	if req.Model.APIKey != "sk-test-key" {
		t.Fatalf("request Model.APIKey = %q, want %q", req.Model.APIKey, "sk-test-key")
	}
	if req.Model.Temperature != 0.7 {
		t.Fatalf("request Model.Temperature = %v, want %v", req.Model.Temperature, 0.7)
	}

	// Verify upstream config in store has original BaseURL
	cfg, err := modelStore.Resolve("gpt-4.1")
	if err != nil {
		t.Fatalf("failed to resolve upstream config: %v", err)
	}
	if cfg.BaseURL != "https://api.openai.com" {
		t.Fatalf("store config BaseURL = %q, want %q", cfg.BaseURL, "https://api.openai.com")
	}
	if !cfg.BaseURLHasV1 {
		t.Fatal("store config BaseURLHasV1 should be true")
	}
}

func TestModelStepValidatesRequiredFields(t *testing.T) {
	step := ModelStep{}

	tests := []struct {
		name  string
		model agent.ModelOptions
		want  string
	}{
		{
			name:  "missing provider",
			model: agent.ModelOptions{Model: "gpt-4.1", APIKey: "sk-test"},
			want:  "llm provider is required",
		},
		{
			name:  "missing model",
			model: agent.ModelOptions{Provider: "openai", APIKey: "sk-test"},
			want:  "llm model is required",
		},
		{
			name:  "missing api_key",
			model: agent.ModelOptions{Provider: "openai", Model: "gpt-4.1"},
			want:  "llm api_key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &agent.RequestContext{Model: tt.model}
			err := step.Run(context.Background(), &State{Request: req})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestModelStepWithEmptyWorkerAddress(t *testing.T) {
	// Reset store before test
	modelStore.Put(modelrouter.UpstreamConfig{})

	oldProfile := identity.Get()
	defer identity.Set(oldProfile)
	identity.Set(identity.Profile{WorkerAddr: ""})

	req := &agent.RequestContext{
		Model: agent.ModelOptions{
			Provider:     "openai",
			Model:        "gpt-4.1",
			APIKey:       "sk-test-key",
			BaseURL:      "https://api.openai.com",
			BaseURLHasV1: false,
		},
	}

	step := ModelStep{}
	err := step.Run(context.Background(), &State{Request: req})
	if err != nil {
		t.Fatalf("ModelStep.Run() error = %v", err)
	}

	// Verify request BaseURL is empty when worker address is empty
	if req.Model.BaseURL != "" {
		t.Fatalf("request Model.BaseURL = %q, want empty", req.Model.BaseURL)
	}

	// Verify upstream config still written to store with original BaseURL
	cfg, err := modelStore.Resolve("gpt-4.1")
	if err != nil {
		t.Fatalf("failed to resolve upstream config: %v", err)
	}
	if cfg.BaseURL != "https://api.openai.com" {
		t.Fatalf("store config BaseURL = %q, want %q", cfg.BaseURL, "https://api.openai.com")
	}
	// BaseURLHasV1 should be preserved as the request's original value (false)
	if cfg.BaseURLHasV1 {
		t.Fatal("store config BaseURLHasV1 should be preserved as false")
	}
}

func TestModelStepRequiresRequest(t *testing.T) {
	step := ModelStep{}
	err := step.Run(context.Background(), &State{Request: nil})
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if err.Error() != "request context is required" {
		t.Fatalf("error = %q, want %q", err.Error(), "request context is required")
	}
}
