package eventengine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	interactionevent "github.com/insmtx/Leros/backend/pkg/event"
)

func requestFromInteractionEvent(event *interactionevent.Event) *agent.RequestContext {
	if event == nil {
		return &agent.RequestContext{
			Input: agent.InputContext{
				Type: agent.InputTypeEvent,
			},
		}
	}

	return &agent.RequestContext{
		RunID:   event.EventID,
		TraceID: event.TraceID,
		Actor: agent.ActorContext{
			UserID:     event.Actor,
			Channel:    event.Channel,
			ExternalID: event.Actor,
		},
		Input: agent.InputContext{
			Type: agent.InputTypeEvent,
			Messages: []agent.InputMessage{{
				Role:    "user",
				Content: buildInteractionEventInput(event),
			}},
		},
		Metadata: map[string]any{
			"channel":       event.Channel,
			"event_type":    event.EventType,
			"subject":       event.Repository,
			"event_context": mapFromAny(event.Context),
			"event_payload": mapFromAny(event.Payload),
		},
	}
}

func buildInteractionEventInput(event *interactionevent.Event) string {
	if event == nil {
		return ""
	}

	contextMap := mapFromAny(event.Context)
	payloadMap := mapFromAny(event.Payload)
	sections := []string{
		"You are handling an external event inside Leros.",
		buildInteractionEventEnvelope(event),
		buildInteractionEventTask(event.EventType),
	}
	if contextSection := buildJSONSection("Event context", contextMap); contextSection != "" {
		sections = append(sections, contextSection)
	}
	if payloadSection := buildJSONSection("Raw event payload", payloadMap); payloadSection != "" {
		sections = append(sections, payloadSection)
	}

	return strings.Join(filterEmptyStrings(sections), "\n\n")
}

func buildInteractionEventEnvelope(event *interactionevent.Event) string {
	lines := []string{"Event envelope:"}
	if event.Channel != "" {
		lines = append(lines, "- channel: "+event.Channel)
	}
	if event.EventType != "" {
		lines = append(lines, "- event_type: "+event.EventType)
	}
	if event.Actor != "" {
		lines = append(lines, "- actor: "+event.Actor)
	}
	if event.Repository != "" {
		lines = append(lines, "- subject: "+event.Repository)
	}
	if event.EventID != "" {
		lines = append(lines, "- run_id: "+event.EventID)
	}
	if event.TraceID != "" {
		lines = append(lines, "- trace_id: "+event.TraceID)
	}
	return strings.Join(lines, "\n")
}

func buildInteractionEventTask(eventType string) string {
	base := "Task:\n- Understand what happened from the event payload.\n- Use available skills and tools to gather authoritative details before making claims.\n- If the event requires an external response, decide whether to publish one and keep it evidence-based."

	switch eventType {
	case "pull_request", "github.pull_request", "github.pull_request.opened":
		return base + "\n- This appears to be a GitHub pull request event. Review the change carefully before publishing any GitHub review."
	case "push", "github.push":
		return base + "\n- This appears to be a GitHub push event. Use the commit list and repository context to understand what changed before deciding whether any follow-up is needed."
	case "issue_comment", "github.issue_comment":
		return base + "\n- This appears to be a GitHub issue or pull request comment event. Decide whether a reply is needed."
	default:
		return base
	}
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil
	}
	return decoded
}

func buildJSONSection(title string, value any) string {
	if value == nil {
		return ""
	}

	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return ""
	}

	text := string(encoded)
	if len(text) > 6000 {
		text = text[:6000] + "\n... (truncated)"
	}

	return fmt.Sprintf("%s:\n```json\n%s\n```", title, text)
}

func filterEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
