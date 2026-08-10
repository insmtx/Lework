package types

import "testing"

func TestSamplingParamsFromConfigParsesSnakeCaseKeys(t *testing.T) {
	c := LLMModelConfig{
		"top_p":             0.95,
		"frequency_penalty": 0.1,
		"presence_penalty":  float64(0),
		"limit": map[string]interface{}{
			"context": float64(82144),
			"output":  float64(42768),
		},
	}
	p := SamplingParamsFromConfig(c)
	if p.TopP == nil || *p.TopP != 0.95 {
		t.Fatalf("TopP = %v, want 0.95", p.TopP)
	}
	if p.FrequencyPenalty == nil || *p.FrequencyPenalty != 0.1 {
		t.Fatalf("FrequencyPenalty = %v, want 0.1", p.FrequencyPenalty)
	}
	if p.PresencePenalty == nil || *p.PresencePenalty != 0 {
		t.Fatalf("PresencePenalty = %v, want 0", p.PresencePenalty)
	}
	if p.Limit == nil || p.Limit.Context != 82144 || p.Limit.Output != 42768 {
		t.Fatalf("Limit = %#v, want {82144,42768}", p.Limit)
	}
}

func TestSamplingParamsFromConfigIgnoresEmptyLimit(t *testing.T) {
	c := LLMModelConfig{"topP": 0.5, "limit": map[string]interface{}{}}
	p := SamplingParamsFromConfig(c)
	if p.Limit != nil {
		t.Fatalf("Limit = %#v, want nil", p.Limit)
	}
}

func TestSamplingParamsFromConfigEmptyConfig(t *testing.T) {
	p := SamplingParamsFromConfig(nil)
	if p.TopP != nil || p.FrequencyPenalty != nil || p.PresencePenalty != nil || p.Limit != nil {
		t.Fatalf("expected zero params for empty config, got %#v", p)
	}
}
