package agentrun

import (
	"encoding/json"

	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func agentUsageToDomain(usage *agent.Usage) *agentrundomain.Usage {
	normalized := agent.EnsureUsage(usage)
	return &agentrundomain.Usage{
		TotalTokens:       normalized.InputTokens + normalized.OutputTokens,
		InputTokens:       normalized.InputTokens,
		OutputTokens:      normalized.OutputTokens,
		CacheInputTokens:  normalized.CacheInputTokens,
		CacheOutputTokens: normalized.CacheOutputTokens,
	}
}

func agentToolCallsToDomain(records []agent.ToolCallRecord) []agentrundomain.ToolCallRecord {
	if len(records) == 0 {
		return nil
	}
	result := make([]agentrundomain.ToolCallRecord, len(records))
	for index, record := range records {
		result[index] = agentrundomain.ToolCallRecord{
			CallID: record.CallID,
			Name:   record.Name,
			Result: append(json.RawMessage(nil), record.Result...),
			Error:  record.Error,
		}
	}
	return result
}

func runUsageToMessaging(usage *agentrundomain.Usage) *messaging.UsagePayload {
	if usage == nil {
		usage = &agentrundomain.Usage{}
	}
	return &messaging.UsagePayload{
		TotalTokens:       usage.InputTokens + usage.OutputTokens,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheInputTokens:  usage.CacheInputTokens,
		CacheOutputTokens: usage.CacheOutputTokens,
	}
}

func agentUsageToMessaging(usage *agent.Usage) *messaging.UsagePayload {
	normalized := agent.EnsureUsage(usage)
	return &messaging.UsagePayload{
		TotalTokens:       normalized.InputTokens + normalized.OutputTokens,
		InputTokens:       normalized.InputTokens,
		OutputTokens:      normalized.OutputTokens,
		CacheInputTokens:  normalized.CacheInputTokens,
		CacheOutputTokens: normalized.CacheOutputTokens,
	}
}
