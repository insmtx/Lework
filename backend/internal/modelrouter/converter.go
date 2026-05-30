package modelrouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ygpkg/yg-go/logs"
)

// Decoder converts protocol-specific messages to canonical IR.
type Decoder interface {
	DecodeRequest(body map[string]interface{}) (*IRRequest, error)
	DecodeResponse(body map[string]interface{}) (*IRResponse, error)
}

// Encoder converts canonical IR to protocol-specific messages.
type Encoder interface {
	EncodeRequest(ir *IRRequest) (map[string]interface{}, error)
	EncodeResponse(ir *IRResponse) (map[string]interface{}, error)
}

// decoders maps entry protocols to their decoder implementations.
var decoders = map[Protocol]Decoder{
	ProtocolOpenAIChat:        &openAIChatDecoder{},
	ProtocolOpenAIResponses:   &openAIResponsesDecoder{},
	ProtocolAnthropicMessages: &anthropicDecoder{},
}

// encoders maps entry protocols to their encoder implementations.
var encoders = map[Protocol]Encoder{
	ProtocolOpenAIChat:        &openAIChatEncoder{},
	ProtocolOpenAIResponses:   &openAIResponsesEncoder{},
	ProtocolAnthropicMessages: &anthropicEncoder{},
}

var errInvalidRequestBody = errors.New("parse request body")

func convertRequest(body []byte, entryProtocol, upstreamProtocol Protocol, upstreamModel string) ([]byte, error) {
	if entryProtocol == upstreamProtocol {
		return rewriteModelName(body, upstreamModel)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidRequestBody, err)
	}

	decoder, ok := decoders[entryProtocol]
	if !ok {
		return nil, fmt.Errorf("no decoder for protocol %s", entryProtocol)
	}

	ir, err := decoder.DecodeRequest(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s request: %w", entryProtocol, err)
	}

	ir.Model = upstreamModel

	encoder, ok := encoders[upstreamProtocol]
	if !ok {
		return nil, fmt.Errorf("no encoder for protocol %s", upstreamProtocol)
	}

	out, err := encoder.EncodeRequest(ir)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", upstreamProtocol, err)
	}

	return json.Marshal(out)
}

func convertResponse(body []byte, entryProtocol, upstreamProtocol Protocol) ([]byte, error) {
	if entryProtocol == upstreamProtocol {
		return body, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse response body: %w", err)
	}

	decoder, ok := decoders[upstreamProtocol]
	if !ok {
		return nil, fmt.Errorf("no decoder for protocol %s", upstreamProtocol)
	}

	ir, err := decoder.DecodeResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s response: %w", upstreamProtocol, err)
	}

	encoder, ok := encoders[entryProtocol]
	if !ok {
		return nil, fmt.Errorf("no encoder for protocol %s", entryProtocol)
	}

	out, err := encoder.EncodeResponse(ir)
	if err != nil {
		return nil, fmt.Errorf("encode %s response: %w", entryProtocol, err)
	}

	return json.Marshal(out)
}

func rewriteModelName(body []byte, modelName string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidRequestBody, err)
	}
	raw["model"] = modelName
	return json.Marshal(raw)
}

func convertStreamEvent(data []byte, entryProtocol, upstreamProtocol Protocol) ([][]byte, error) {
	return convertStreamEventWithState(data, entryProtocol, upstreamProtocol, nil)
}

func convertStreamEventWithState(data []byte, entryProtocol, upstreamProtocol Protocol, state *streamConversionState) ([][]byte, error) {
	if entryProtocol == upstreamProtocol {
		return [][]byte{data}, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse stream event: %w", err)
	}

	var irEvents []*IRStreamEvent
	switch upstreamProtocol {
	case ProtocolOpenAIChat:
		irEvents = decodeOpenAIChatStreamEvent(raw)
	case ProtocolOpenAIResponses:
		irEvents = decodeResponsesStreamEvent(raw)
	case ProtocolAnthropicMessages:
		eventType := getString(raw, "type")
		irEvents = decodeAnthropicStreamEvent(eventType, raw)
	}

	if len(irEvents) == 0 {
		return nil, nil
	}
	if state != nil {
		irEvents = state.prepareIRStreamEvents(entryProtocol, irEvents)
	}

	var result [][]byte
	for _, irEvent := range irEvents {
		var encodedEvents []map[string]interface{}

		switch entryProtocol {
		case ProtocolOpenAIChat:
			encodedEvents = encodeOpenAIChatStreamEvent(irEvent)
		case ProtocolOpenAIResponses:
			encodedEvents = encodeResponsesStreamEventWithState(irEvent, state)
		case ProtocolAnthropicMessages:
			encodedEvents = encodeAnthropicStreamEvent(irEvent)
		}

		for _, evt := range encodedEvents {
			if evt == nil {
				continue
			}
			b, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			result = append(result, b)
		}
	}

	return result, nil
}

type streamConversionState struct {
	responses   responsesStreamState
	textStarted map[int]bool
	textStopped map[int]bool
	toolStarted map[int]bool
	toolStopped map[int]bool

	// 记录上游 Anthropic 流中真实 tool block 的 ID 和名称
	toolBlockIDs   map[int]string
	toolBlockNames map[int]string
}

func newStreamConversionState() *streamConversionState {
	return &streamConversionState{
		toolBlockIDs:   make(map[int]string),
		toolBlockNames: make(map[int]string),
	}
}

func (s *streamConversionState) prepareIRStreamEvents(entryProtocol Protocol, events []*IRStreamEvent) []*IRStreamEvent {
	if s == nil {
		return events
	}
	if entryProtocol == ProtocolOpenAIResponses {
		return s.prepareResponsesIRStreamEvents(events)
	}
	if entryProtocol == ProtocolAnthropicMessages {
		return s.prepareAnthropicIRStreamEvents(events)
	}
	return events
}

func (s *streamConversionState) prepareResponsesIRStreamEvents(events []*IRStreamEvent) []*IRStreamEvent {
	var prepared []*IRStreamEvent
	for _, event := range events {
		if event == nil {
			continue
		}
		if event.Type == IRStreamContentDelta && event.DeltaType == "text" && !s.responses.hasStartedText(event.Index) {
			prepared = append(prepared, &IRStreamEvent{Type: IRStreamContentStart, Index: event.Index})
		}
		if event.Type == IRStreamMessageDelta {
			if s.responses.hasStartedText(0) && !s.responses.hasStoppedText(0) {
				prepared = append(prepared, &IRStreamEvent{Type: IRStreamContentStop, Index: 0})
			}
			s.responses.setMessageDelta(event)
		}
		prepared = append(prepared, event)
	}
	return prepared
}

func (s *streamConversionState) prepareAnthropicIRStreamEvents(events []*IRStreamEvent) []*IRStreamEvent {
	var prepared []*IRStreamEvent
	for _, event := range events {
		if event == nil {
			continue
		}
		// 捕获上游真实的 tool block ID 和名称
		if event.Type == IRStreamContentStart && event.ContentBlock != nil && event.ContentBlock.Type == IRBlockToolUse {
			if event.ContentBlock.ToolUseID != "" {
				s.toolBlockIDs[event.Index] = event.ContentBlock.ToolUseID
			}
			if event.ContentBlock.ToolUseName != "" {
				s.toolBlockNames[event.Index] = event.ContentBlock.ToolUseName
			}
		}
		switch event.Type {
		case IRStreamContentStart:
			prepared = append(prepared, s.prepareAnthropicContentStart(event)...)
		case IRStreamContentDelta:
			prepared = append(prepared, s.prepareAnthropicContentDelta(event)...)
		case IRStreamContentStop:
			prepared = append(prepared, s.prepareAnthropicContentStop(event)...)
		case IRStreamMessageDelta:
			prepared = append(prepared, s.closeAnthropicOpenBlocks()...)
			prepared = append(prepared, event)
		default:
			prepared = append(prepared, event)
		}
	}
	return prepared
}

func (s *streamConversionState) prepareAnthropicContentStart(event *IRStreamEvent) []*IRStreamEvent {
	if event.ContentBlock != nil && event.ContentBlock.Type == IRBlockToolUse {
		if s.hasToolStarted(event.Index) && !s.hasToolStopped(event.Index) {
			logs.Debugf("modelrouter: anthropic duplicate tool block start ignored index=%d", event.Index)
			return nil
		}
		s.markToolStarted(event.Index)
		s.clearToolStopped(event.Index)
		logs.Debugf("modelrouter: anthropic tool block started index=%d id=%s name=%s", event.Index, event.ContentBlock.ToolUseID, event.ContentBlock.ToolUseName)
		return []*IRStreamEvent{event}
	}
	if s.hasTextStarted(event.Index) && !s.hasTextStopped(event.Index) {
		logs.Debugf("modelrouter: anthropic duplicate text block start ignored index=%d", event.Index)
		return nil
	}
	s.markTextStarted(event.Index)
	s.clearTextStopped(event.Index)
	logs.Debugf("modelrouter: anthropic text block started index=%d", event.Index)
	return []*IRStreamEvent{event}
}

func (s *streamConversionState) prepareAnthropicContentDelta(event *IRStreamEvent) []*IRStreamEvent {
	var prepared []*IRStreamEvent
	switch event.DeltaType {
	case "text":
		if !s.hasTextStarted(event.Index) || s.hasTextStopped(event.Index) {
			prepared = append(prepared, &IRStreamEvent{
				Type:         IRStreamContentStart,
				Index:        event.Index,
				ContentBlock: &IRContentBlock{Type: IRBlockText},
			})
			s.markTextStarted(event.Index)
			s.clearTextStopped(event.Index)
			logs.Debugf("modelrouter: anthropic text block auto-started before delta index=%d", event.Index)
		}
	case "input_json":
		if !s.hasToolStarted(event.Index) || s.hasToolStopped(event.Index) {
			// 使用上游真实的 tool ID（如果已收集到），否则 fallback
			toolID := s.toolBlockIDs[event.Index]
			if toolID == "" {
				toolID = ensureToolBlockID(event.Index)
				logs.Debugf("modelrouter: anthropic using fallback tool ID for index=%d", event.Index)
			}
			toolName := s.toolBlockNames[event.Index]

			prepared = append(prepared, &IRStreamEvent{
				Type:  IRStreamContentStart,
				Index: event.Index,
				ContentBlock: &IRContentBlock{
					Type:        IRBlockToolUse,
					ToolUseID:   toolID,
					ToolUseName: toolName,
				},
			})
			s.markToolStarted(event.Index)
			s.clearToolStopped(event.Index)
			logs.Debugf("modelrouter: anthropic tool block auto-started before delta index=%d id=%s", event.Index, toolID)
		}
	}
	prepared = append(prepared, event)
	return prepared
}

func (s *streamConversionState) prepareAnthropicContentStop(event *IRStreamEvent) []*IRStreamEvent {
	if s.hasToolStarted(event.Index) && !s.hasToolStopped(event.Index) {
		s.markToolStopped(event.Index)
		logs.Debugf("modelrouter: anthropic tool block stopped index=%d", event.Index)
		return []*IRStreamEvent{event}
	}
	if s.hasTextStarted(event.Index) && !s.hasTextStopped(event.Index) {
		s.markTextStopped(event.Index)
		logs.Debugf("modelrouter: anthropic text block stopped index=%d", event.Index)
		return []*IRStreamEvent{event}
	}
	return nil
}

func (s *streamConversionState) closeAnthropicOpenBlocks() []*IRStreamEvent {
	var pending []*IRStreamEvent
	for _, index := range s.openToolIndexes() {
		pending = append(pending, &IRStreamEvent{Type: IRStreamContentStop, Index: index})
		s.markToolStopped(index)
		logs.Debugf("modelrouter: anthropic tool block auto-stopped before message_delta index=%d", index)
	}
	for _, index := range s.openTextIndexes() {
		pending = append(pending, &IRStreamEvent{Type: IRStreamContentStop, Index: index})
		s.markTextStopped(index)
		logs.Debugf("modelrouter: anthropic text block auto-stopped before message_delta index=%d", index)
	}
	return pending
}

func (s *streamConversionState) hasTextStarted(index int) bool {
	return s != nil && s.textStarted != nil && s.textStarted[index]
}

func (s *streamConversionState) markTextStarted(index int) {
	if s.textStarted == nil {
		s.textStarted = make(map[int]bool)
	}
	s.textStarted[index] = true
}

func (s *streamConversionState) hasTextStopped(index int) bool {
	return s != nil && s.textStopped != nil && s.textStopped[index]
}

func (s *streamConversionState) markTextStopped(index int) {
	if s.textStopped == nil {
		s.textStopped = make(map[int]bool)
	}
	s.textStopped[index] = true
}

func (s *streamConversionState) clearTextStopped(index int) {
	if s == nil || s.textStopped == nil {
		return
	}
	delete(s.textStopped, index)
}

func (s *streamConversionState) hasToolStarted(index int) bool {
	return s != nil && s.toolStarted != nil && s.toolStarted[index]
}

func (s *streamConversionState) markToolStarted(index int) {
	if s.toolStarted == nil {
		s.toolStarted = make(map[int]bool)
	}
	s.toolStarted[index] = true
}

func (s *streamConversionState) hasToolStopped(index int) bool {
	return s != nil && s.toolStopped != nil && s.toolStopped[index]
}

func (s *streamConversionState) markToolStopped(index int) {
	if s.toolStopped == nil {
		s.toolStopped = make(map[int]bool)
	}
	s.toolStopped[index] = true
}

func (s *streamConversionState) clearToolStopped(index int) {
	if s == nil || s.toolStopped == nil {
		return
	}
	delete(s.toolStopped, index)
}

func (s *streamConversionState) openTextIndexes() []int {
	return collectOpenIndexes(s.textStarted, s.textStopped)
}

func (s *streamConversionState) openToolIndexes() []int {
	return collectOpenIndexes(s.toolStarted, s.toolStopped)
}

func collectOpenIndexes(started, stopped map[int]bool) []int {
	if len(started) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(started))
	for index := range started {
		if stopped != nil && stopped[index] {
			continue
		}
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func ensureToolBlockID(index int) string {
	return fmt.Sprintf("toolu_stream_%d", index)
}
