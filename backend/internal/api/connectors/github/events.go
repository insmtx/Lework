package github

import (
	"encoding/json"

	"github.com/google/go-github/v78/github"

	interactionevent "github.com/insmtx/Lework/backend/pkg/event"
)

// convertEvent converts GitHub events to Lework interaction events.
func (c *Connector) convertEvent(eventType string, event any) *interactionevent.Event {
	switch eventType {
	case "issue_comment":
		return c.convertIssueComment(event.(*github.IssueCommentEvent))
	case "pull_request":
		return c.convertPullRequest(event.(*github.PullRequestEvent))
	case "push":
		return c.convertPush(event.(*github.PushEvent))
	default:
		return nil
	}
}

// convertIssueComment converts GitHub IssueCommentEvent to Lework Event.
func (c *Connector) convertIssueComment(event *github.IssueCommentEvent) *interactionevent.Event {
	payload := rawPayloadMap(event)
	actor := event.GetSender().GetLogin()
	if actor == "" {
		actor = event.GetComment().GetUser().GetLogin()
	}
	return &interactionevent.Event{
		Channel:    c.ChannelCode(),
		EventType:  EventTypeIssueComment,
		Actor:      actor,
		Repository: event.GetRepo().GetFullName(),
		Context: map[string]interface{}{
			"provider":     "github",
			"action":       event.GetAction(),
			"sender_login": event.GetSender().GetLogin(),
		},
		Payload: payload,
	}
}

// convertPullRequest converts GitHub PullRequestEvent to Lework Event.
func (c *Connector) convertPullRequest(event *github.PullRequestEvent) *interactionevent.Event {
	if !isSupportedPullRequestAction(event.GetAction()) {
		return nil
	}

	payload := rawPayloadMap(event)
	return &interactionevent.Event{
		Channel:    c.ChannelCode(),
		EventType:  EventTypePullRequest,
		Actor:      event.GetSender().GetLogin(),
		Repository: event.GetRepo().GetFullName(),
		Context: map[string]interface{}{
			"provider":     "github",
			"action":       event.GetAction(),
			"sender_login": event.GetSender().GetLogin(),
		},
		Payload: payload,
	}
}

// convertPush converts GitHub PushEvent to Lework Event.
func (c *Connector) convertPush(event *github.PushEvent) *interactionevent.Event {
	payload := rawPayloadMap(event)
	actor := event.GetSender().GetLogin()
	if actor == "" {
		actor = event.GetPusher().GetName()
	}
	return &interactionevent.Event{
		Channel:    c.ChannelCode(),
		EventType:  EventTypePush,
		Actor:      actor,
		Repository: event.GetRepo().GetFullName(),
		Context: map[string]interface{}{
			"provider":     "github",
			"ref":          event.GetRef(),
			"sender_login": event.GetSender().GetLogin(),
		},
		Payload: payload,
	}
}

func rawPayloadMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return nil
	}

	return payload
}

func isSupportedPullRequestAction(action string) bool {
	switch action {
	case "opened", "reopened", "synchronize", "ready_for_review":
		return true
	default:
		return false
	}
}
