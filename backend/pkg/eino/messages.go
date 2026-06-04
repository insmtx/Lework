package eino

import (
	"strings"

	"github.com/cloudwego/eino/adk"
	einoschema "github.com/cloudwego/eino/schema"
)

// Message is a provider-neutral role/content message snapshot.
type Message struct {
	Role    string
	Content string
}

// BuildMessages converts role/content snapshots into Eino ADK messages.
func BuildMessages(messages []Message) []adk.Message {
	if len(messages) == 0 {
		return nil
	}

	result := make([]adk.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" {
			continue
		}

		role := msg.Role
		if role == "" {
			role = "user"
		}

		var adkMsg adk.Message
		switch role {
		case "system":
			adkMsg = einoschema.SystemMessage(msg.Content)
		case "assistant":
			adkMsg = einoschema.AssistantMessage(msg.Content, nil)
		case "tool":
			adkMsg = einoschema.ToolMessage(msg.Content, "")
		default:
			adkMsg = einoschema.UserMessage(msg.Content)
		}
		result = append(result, adkMsg)
	}

	return result
}

// AppendUserMessage appends a user message to existing Eino ADK messages.
func AppendUserMessage(existing []adk.Message, userInput string) []adk.Message {
	if strings.TrimSpace(userInput) == "" {
		return existing
	}

	msgs := make([]adk.Message, 0, len(existing)+1)
	msgs = append(msgs, existing...)
	msgs = append(msgs, einoschema.UserMessage(userInput))
	return msgs
}
