package modelrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/gin-gonic/gin"

	"github.com/insmtx/Lework/backend/internal/llm"
	"github.com/insmtx/Lework/backend/pkg/llmprotocol"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Non-stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleNonStreamResponse(
	c *gin.Context,
	caller llm.Caller,
	orgID uint,
	modelCfg *llm.ModelConfig,
	upstreamBodyBytes []byte,
	entryProtocol, upstreamProtocol llmprotocol.Protocol,
	entryAdapter, upstreamAdapter llmprotocol.ProtocolAdapter,
	dl *DebugLogger,
) {
	result, err := caller.CallRaw(c.Request.Context(), orgID, modelCfg, upstreamBodyBytes)
	if err != nil {
		dl.LogError("call_raw", err)
		var upErr *llm.UpstreamError
		if errors.As(err, &upErr) {
			statusCode := upErr.StatusCode
			if statusCode >= 500 {
				statusCode = http.StatusBadGateway
			}
			c.JSON(statusCode, parseUpstreamErrorBody(upErr.Body, entryProtocol))
		} else if result != nil && len(result.RawResponseBody) > 0 {
			c.JSON(http.StatusBadGateway, parseUpstreamErrorBody(result.RawResponseBody, entryProtocol))
		} else {
			handleCallError(c, entryProtocol, err)
		}
		return
	}

	respBody := result.RawResponseBody
	dl.LogUpstreamResponse(respBody)

	var rawResp map[string]interface{}
	if err := sonic.Unmarshal(respBody, &rawResp); err != nil {
		c.JSON(http.StatusBadGateway, newEntryError(entryProtocol, "invalid upstream response"))
		return
	}

	irResp, err := upstreamAdapter.DecodeResponse(rawResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("decode upstream response: %v", err)))
		return
	}

	entryBody, err := entryAdapter.EncodeResponse(irResp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, fmt.Sprintf("encode entry response: %v", err)))
		return
	}

	entryBytes, err := marshalJSON(entryBody)
	if err != nil {
		c.JSON(http.StatusInternalServerError, newEntryError(entryProtocol, "marshal entry response failed"))
		return
	}

	dl.LogEntryResponse(entryBytes)
	c.Data(http.StatusOK, "application/json", entryBytes)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Stream response handling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func handleStreamResponse(
	c *gin.Context,
	caller llm.Caller,
	orgID uint,
	modelCfg *llm.ModelConfig,
	upstreamBodyBytes []byte,
	entryProtocol, upstreamProtocol llmprotocol.Protocol,
	entryAdapter, upstreamAdapter llmprotocol.ProtocolAdapter,
	dl *DebugLogger,
) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	c.Writer.WriteHeaderNow()
	c.Writer.Flush()

	var once sync.Once
	closeDone := make(chan struct{})
	w := c.Writer

	sink := &rawSSESink{
		writer:           w,
		flusher:          w,
		entryProtocol:    entryProtocol,
		upstreamProtocol: upstreamProtocol,
		upstreamAdapter:  upstreamAdapter,
		entryAdapter:     entryAdapter,
		dl:               dl,
		aggregator:       llmprotocol.NewStreamAggregator(),
		closeOnce:        &once,
		closeDone:        closeDone,
	}

	result, err := caller.StreamRaw(c.Request.Context(), orgID, modelCfg, upstreamBodyBytes, sink)
	if err != nil {
		dl.LogError("stream_raw", err)
		if result != nil && len(result.RawResponseBody) > 0 {
			sink.flushError(errors.New(string(result.RawResponseBody)))
		} else {
			sink.flushError(err)
		}
		return
	}

	sink.finalize()
}

// rawSSESink implements llm.RawChunkSink to receive raw SSE chunks from CallerHTTP
// and perform protocol conversion + SSE formatting.
type rawSSESink struct {
	writer           http.ResponseWriter
	flusher          http.Flusher
	entryProtocol    llmprotocol.Protocol
	upstreamProtocol llmprotocol.Protocol
	upstreamAdapter  llmprotocol.ProtocolAdapter
	entryAdapter     llmprotocol.ProtocolAdapter
	dl               *DebugLogger
	aggregator       *llmprotocol.StreamAggregator
	state            sinkState
	closeOnce        *sync.Once
	closeDone        chan struct{}
	mu               sync.Mutex
}

type sinkState struct {
	upstream    interface{}
	entry       interface{}
	eventType   string
	currentData strings.Builder
}

func (s *rawSSESink) initState() {
	if s.state.upstream == nil {
		s.state.upstream = s.upstreamAdapter.NewStreamState()
	}
	if s.state.entry == nil {
		s.state.entry = s.entryAdapter.NewStreamState()
	}
}

func (s *rawSSESink) EmitRawChunk(ctx context.Context, chunk []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.initState()

	s.dl.LogUpstreamStreamChunk(chunk)

	if s.entryProtocol == s.upstreamProtocol {
		if _, err := s.writer.Write(chunk); err != nil {
			return err
		}
		s.flusher.Flush()
		s.dl.LogEntryStreamChunk(chunk)
		return nil
	}

	text := string(chunk)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "event: ") {
			s.state.eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				s.dl.LogUpstreamStreamChunk([]byte("data: [DONE]\n\n"))
				s.flushCurrentData()
				s.writeIREvents(s.aggregator.Finalize())
				if s.upstreamProtocol == llmprotocol.ProtocolOpenAIChat {
					s.dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
					s.writer.Write([]byte("data: [DONE]\n\n"))
					s.flusher.Flush()
				}
				return nil
			}
			s.state.currentData.WriteString(data)
			continue
		}
		if line == "" && s.state.currentData.Len() > 0 {
			s.flushCurrentData()
		}
	}

	return nil
}

func (s *rawSSESink) flushCurrentData() {
	if s.state.currentData.Len() == 0 {
		return
	}
	dataStr := s.state.currentData.String()
	s.state.currentData.Reset()

	var rawUpstream map[string]interface{}
	if err := sonic.Unmarshal([]byte(dataStr), &rawUpstream); err != nil {
		return
	}

	irEvents, err := s.upstreamAdapter.DecodeStreamEvent(rawUpstream, s.state.upstream)
	if err != nil {
		return
	}

	for _, irEvt := range irEvents {
		fixedEvents := s.aggregator.ProcessIREvent(irEvt)
		s.writeIREvents(fixedEvents)
	}

	s.state.eventType = ""
}

func (s *rawSSESink) writeIREvents(events []*llmprotocol.IRStreamEvent) {
	for _, evt := range events {
		payloads, err := s.entryAdapter.EncodeStreamEvent(evt, s.state.entry)
		if err != nil {
			continue
		}
		for _, payload := range payloads {
			payloadBytes, err := marshalJSON(payload)
			if err != nil {
				continue
			}
			evtType := s.state.eventType
			if evtType == "" {
				if v, ok := payload["type"].(string); ok {
					evtType = v
				}
			}
			formatted := formatSSE(s.entryProtocol, evtType, payloadBytes)
			s.dl.LogEntryStreamChunk(formatted)
			if _, err := s.writer.Write(formatted); err != nil {
				return
			}
			s.flusher.Flush()
		}

		if evt.Type == llmprotocol.IRStreamDone && s.entryProtocol == llmprotocol.ProtocolOpenAIChat {
			s.dl.LogEntryStreamChunk([]byte("data: [DONE]\n\n"))
			s.writer.Write([]byte("data: [DONE]\n\n"))
			s.flusher.Flush()
		}
	}
}

func (s *rawSSESink) finalize() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.aggregator != nil && !s.aggregator.IsDone() {
		s.writeIREvents(s.aggregator.Finalize())
	}

	s.closeOnce.Do(func() {
		close(s.closeDone)
	})
}

func (s *rawSSESink) flushError(err error) {
	errBytes, _ := marshalJSON(newEntryError(s.entryProtocol, err.Error()))
	formatted := formatSSE(s.entryProtocol, "error", errBytes)
	s.writer.Write(formatted)
	s.flusher.Flush()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SSE formatting
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func formatSSE(proto llmprotocol.Protocol, eventType string, data []byte) []byte {
	switch proto {
	case llmprotocol.ProtocolOpenAIChat:
		return []byte(fmt.Sprintf("data: %s\n\n", string(data)))
	default: // Anthropic, Responses, Gemini use event: header
		return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(data)))
	}
}
