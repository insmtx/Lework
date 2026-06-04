package native

import (
	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/internal/runtime/events"
)

type runState struct {
	req          *agent.RequestContext
	eventSink    events.Sink
	userInput    string
	systemPrompt string
	toolBinding  toolBinding
	maxStep      int
}
