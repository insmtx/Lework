package spark

import (
	"context"
	"testing"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/types"
)

func TestModelRuntimeRequiresExactDeploymentBinding(t *testing.T) {
	model := validModel()
	model.State = "ready"
	s := modelFixture(t, model, nil)
	s.cfg.ModelBindings = []config.SparkModelBinding{{LLMModelID: 1, ModelID: "qwen", ModelName: "Qwen", BaseURL: "http://local:58089"}}
	refs := []types.LLMModelReference{
		{ID: 1, OrgID: 7, Model: "Qwen", BaseURL: "http://local:58089"},
		{ID: 1, OrgID: 8, Model: "Qwen", BaseURL: "http://local:58089"},
		{ID: 2, OrgID: 7, Model: "Qwen", BaseURL: "http://local:58089"},
		{ID: 1, OrgID: 7, Model: "Qwen", BaseURL: "https://cloud"},
		{ID: 1, OrgID: 7, Model: "Another", BaseURL: "http://local:58089"},
	}
	got := s.ModelRuntimes(context.Background(), refs)
	if got[0].Source != "spark" || got[0].State != "ready" || got[0].ValidUntil <= time.Now().Unix() {
		t.Fatalf("bound: %+v", got[0])
	}
	for _, r := range got[1:] {
		if r.Source != "configuration" || r.State != "unobserved" {
			t.Fatalf("false binding: %+v", r)
		}
	}
}

func TestModelRuntimeFailsClosed(t *testing.T) {
	for _, tc := range []string{"stale", "wrong_model", "unreachable", "duplicate"} {
		t.Run(tc, func(t *testing.T) {
			model := validModel()
			model.State = "ready"
			if tc == "stale" {
				model.ObservedAt -= 90
			}
			if tc == "wrong_model" {
				model.ModelID = "other"
			}
			s := modelFixture(t, model, nil)
			s.cfg.ModelBindings = []config.SparkModelBinding{{LLMModelID: 1, ModelID: "qwen", ModelName: "Qwen", BaseURL: "http://local"}}
			if tc == "unreachable" {
				s.cfg.SocketPath = "/tmp/nonexistent-leros-test.sock"
			}
			if tc == "duplicate" {
				s.cfg.ModelBindings = append(s.cfg.ModelBindings, s.cfg.ModelBindings[0])
			}
			got := s.ModelRuntimes(context.Background(), []types.LLMModelReference{{ID: 1, OrgID: 7, Model: "Qwen", BaseURL: "http://local"}})[0]
			if tc == "wrong_model" && got.Source != "unknown" {
				t.Fatalf("mismatched runtime source: %+v", got)
			}
			if got.State == "ready" || got.ValidUntil != 0 {
				t.Fatalf("must not report ready: %+v", got)
			}
		})
	}
}
