package spark

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/types"
)

// ModelRuntimes observes only explicitly bound local models, without invoking inference or changing state.
func (s *Service) ModelRuntimes(ctx context.Context, refs []types.LLMModelReference) []types.LLMModelRuntime {
	result := make([]types.LLMModelRuntime, len(refs))
	matched := make(map[int]string)
	counts := make(map[uint]int)
	for _, binding := range s.cfg.ModelBindings {
		counts[binding.LLMModelID]++
	}
	for i, ref := range refs {
		result[i] = types.LLMModelRuntime{Source: "configuration", State: "unobserved"}
		if s.cfg.OrgID == 0 || ref.OrgID != s.cfg.OrgID || ref.ID == 0 {
			continue
		}
		for _, binding := range s.cfg.ModelBindings {
			if counts[binding.LLMModelID] != 1 || binding.LLMModelID != ref.ID ||
				!shortText(binding.ModelID) || binding.ModelName == "" || binding.BaseURL == "" ||
				binding.ModelName != ref.Model || strings.TrimRight(binding.BaseURL, "/") != strings.TrimRight(ref.BaseURL, "/") {
				continue
			}
			result[i] = types.LLMModelRuntime{Source: "spark", State: "unknown"}
			matched[i] = binding.ModelID
		}
	}
	if len(matched) == 0 || !s.cfg.ModelControl || !shortText(s.cfg.HostID) || !filepath.IsAbs(s.cfg.SocketPath) {
		return result
	}
	// 一批列表只读取一次 agent；失败时保留来源，但不保留过期的在线结论。
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	model, err := s.modelRequest(ctx, nil)
	if err != nil {
		return result
	}
	for i, id := range matched {
		if model.ModelID != id {
			result[i].Source = "unknown"
			continue
		}
		result[i].State = model.State
		result[i].ObservedAt = model.ObservedAt
		result[i].ValidUntil = model.ObservedAt + 30
	}
	return result
}
