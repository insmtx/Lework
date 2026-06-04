package llmprotocol

import (
	"fmt"
	"strings"
)

// geminiAdapter implements ProtocolAdapter for the Google Gemini API.
type geminiAdapter struct{}

// geminiStreamState tracks Gemini-specific streaming state.
// Gemini streaming sends incremental GenerateContentResponse chunks
// where multiple parts can appear in a single chunk. Unlike other protocols,
// there is no explicit part-start/stop — we infer from index changes.
type geminiStreamState struct {
	partsStarted map[int]bool // track which part indices have been started
	lastPartIdx  int          // highest part index seen
	responseID   string
	model        string
	done         bool // prevent double-stop after finishReason emitted
}

func init() {
	registerAdapterOnInit(&geminiAdapter{})
}

// Protocol returns ProtocolGemini.
func (a *geminiAdapter) Protocol() Protocol {
	return ProtocolGemini
}

// ---------------------------------------------------------------------------
// DecodeRequest: Gemini raw → IR
// ---------------------------------------------------------------------------

// roleMapping maps Gemini role strings to IRRole.
var geminiRoleToIR = map[string]IRRole{
	"user":  IRRoleUser,
	"model": IRRoleAssistant,
}

func (a *geminiAdapter) DecodeRequest(raw map[string]interface{}) (*IRRequest, error) {
	req := &IRRequest{}

	// System instruction
	if si, ok := raw["systemInstruction"].(map[string]interface{}); ok {
		if parts, ok := si["parts"].([]interface{}); ok {
			for _, p := range parts {
				if pm, ok := p.(map[string]interface{}); ok {
					if t := getString(pm, "text"); t != "" {
						req.System += t
					}
				}
			}
		}
	}

	// Contents → Messages
	if rawContents, ok := raw["contents"].([]interface{}); ok {
		for _, c := range rawContents {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			roleStr := getString(cm, "role")
			role := geminiRoleToIR[roleStr]
			if role == "" {
				role = IRRoleUser // default fallback
			}

			msg := IRMessage{Role: role}
			if rawParts, ok := cm["parts"].([]interface{}); ok {
				msg.Parts = decodeGeminiParts(rawParts)
			}
			req.Messages = append(req.Messages, msg)
		}
	}

	// Tools
	if rawTools, ok := raw["tools"].([]interface{}); ok {
		for _, t := range rawTools {
			tm, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			if fds, ok := tm["functionDeclarations"].([]interface{}); ok {
				for _, fd := range fds {
					fdm, ok := fd.(map[string]interface{})
					if !ok {
						continue
					}
					decl := IRToolDecl{
						Type:        "function",
						Name:        getString(fdm, "name"),
						Description: getString(fdm, "description"),
						Parameters:  fdm["parameters"],
					}
					req.Tools = append(req.Tools, decl)
				}
			}
		}
	}

	// Tool choice
	if tc, ok := raw["toolConfig"].(map[string]interface{}); ok {
		if fc, ok := tc["functionCallingConfig"].(map[string]interface{}); ok {
			mode := getString(fc, "mode")
			switch mode {
			case "NONE":
				req.ToolChoice = &IRToolChoice{Type: "none"}
			case "AUTO":
				req.ToolChoice = &IRToolChoice{Type: "auto"}
			case "ANY":
				req.ToolChoice = &IRToolChoice{Type: "any"}
			default:
				// specific function call — mode is not directly used for named choice
				req.ToolChoice = &IRToolChoice{Type: "any"}
			}
			if ac, ok := fc["allowedFunctionNames"].([]interface{}); ok && len(ac) > 0 {
				if s, ok := ac[0].(string); ok {
					req.ToolChoice.Name = s
				}
			}
		}
	}

	// Generation config
	if gc, ok := raw["generationConfig"].(map[string]interface{}); ok {
		if t, ok := getFloat(gc, "temperature"); ok {
			req.Temperature = &t
		}
		if topP, ok := getFloat(gc, "topP"); ok {
			req.TopP = &topP
		}
		if mt, ok := getInt(gc, "maxOutputTokens"); ok {
			req.MaxTokens = mt
		}
		if ss, ok := getStringList(gc, "stopSequences"); ok {
			req.Stop = ss
		}
	}

	return req, nil
}

func decodeGeminiParts(rawParts []interface{}) []IRContentPart {
	var parts []IRContentPart
	for _, rp := range rawParts {
		pm, ok := rp.(map[string]interface{})
		if !ok {
			continue
		}
		part := decodeGeminiPart(pm)
		if part != nil {
			parts = append(parts, *part)
		}
	}
	return parts
}

func decodeGeminiPart(pm map[string]interface{}) *IRContentPart {
	// Text part
	if text := getString(pm, "text"); text != "" || hasExplicitKey(pm, "text") {
		return &IRContentPart{Type: IRPartText, Text: text}
	}

	// Function call
	if fc, ok := pm["functionCall"].(map[string]interface{}); ok {
		argsMap, _ := fc["args"].(map[string]interface{})
		tc := &IRToolCallPart{
			ID:            generatePartID(),
			Name:          getString(fc, "name"),
			ArgumentsJSON: argsMap,
		}
		// Marshal args to raw JSON string
		if tc.ArgumentsJSON != nil {
			raw, err := marshalJSON(tc.ArgumentsJSON)
			if err == nil {
				tc.ArgumentsRaw = string(raw)
			}
		}
		return &IRContentPart{Type: IRPartToolCall, ID: tc.ID, ToolCall: tc}
	}

	// Function response
	if fr, ok := pm["functionResponse"].(map[string]interface{}); ok {
		tr := &IRToolResultPart{
			ToolCallID: getString(fr, "name"), // Gemini uses function name as identifier
		}
		if resp, ok := fr["response"]; ok {
			tr.Content = []IRContentPart{
				{Type: IRPartText, Text: contentToString(resp)},
			}
		}
		return &IRContentPart{Type: IRPartToolResult, ToolResult: tr}
	}

	// Inline data (image, audio, etc.)
	if id, ok := pm["inlineData"].(map[string]interface{}); ok {
		mimeType := getString(id, "mimeType")
		data := getString(id, "data")
		partType := mimeTypeToIRPartType(mimeType)
		return &IRContentPart{
			Type: partType,
			Metadata: map[string]string{
				"mime_type": mimeType,
				"data":      data,
			},
		}
	}

	// File data
	if fd, ok := pm["fileData"].(map[string]interface{}); ok {
		mimeType := getString(fd, "mimeType")
		fileURI := getString(fd, "fileUri")
		return &IRContentPart{
			Type: IRPartFile,
			Metadata: map[string]string{
				"mime_type": mimeType,
				"file_uri":  fileURI,
			},
		}
	}

	return nil
}

// mimeTypeToIRPartType maps a MIME type to the appropriate IRPartType.
func mimeTypeToIRPartType(mimeType string) IRPartType {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return IRPartImage
	case strings.HasPrefix(mimeType, "audio/"):
		return IRPartAudio
	case strings.HasPrefix(mimeType, "video/"):
		return IRPartFile
	default:
		return IRPartFile
	}
}

// hasExplicitKey checks if the map has a specific key set (even to empty/zero value).
// This is needed because Gemini may send `"text": ""` for empty text parts in streaming.
func hasExplicitKey(m map[string]interface{}, key string) bool {
	_, ok := m[key]
	return ok
}

// partIDCounter for generating unique IDs.
var partIDCounter int

func generatePartID() string {
	partIDCounter++
	return fmt.Sprintf("gemini_call_%d", partIDCounter)
}

// ---------------------------------------------------------------------------
// EncodeRequest: IR → Gemini raw
// ---------------------------------------------------------------------------

func (a *geminiAdapter) EncodeRequest(ir *IRRequest) (map[string]interface{}, error) {
	raw := make(map[string]interface{})

	// System instruction
	if ir.System != "" {
		raw["systemInstruction"] = map[string]interface{}{
			"parts": []interface{}{
				map[string]interface{}{"text": ir.System},
			},
		}
	}

	// Messages → contents
	var contents []interface{}
	for _, msg := range ir.Messages {
		content := map[string]interface{}{
			"role":  irRoleToGemini(msg.Role),
			"parts": encodeGeminiParts(msg.Parts),
		}
		contents = append(contents, content)
	}
	raw["contents"] = contents

	// Tools
	if len(ir.Tools) > 0 {
		var functionDecls []interface{}
		for _, tool := range ir.Tools {
			fd := map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
			}
			if tool.Parameters != nil {
				fd["parameters"] = tool.Parameters
			}
			functionDecls = append(functionDecls, fd)
		}
		raw["tools"] = []interface{}{
			map[string]interface{}{"functionDeclarations": functionDecls},
		}
	}

	// Tool choice → toolConfig
	if ir.ToolChoice != nil {
		tc := map[string]interface{}{}
		switch ir.ToolChoice.Type {
		case "none":
			tc["mode"] = "NONE"
		case "auto":
			tc["mode"] = "AUTO"
		case "any", "required":
			tc["mode"] = "ANY"
		}
		if ir.ToolChoice.Name != "" {
			tc["allowedFunctionNames"] = []interface{}{ir.ToolChoice.Name}
		}
		raw["toolConfig"] = map[string]interface{}{
			"functionCallingConfig": tc,
		}
	}

	// Generation config
	gc := make(map[string]interface{})
	hasGC := false
	if ir.Temperature != nil {
		gc["temperature"] = *ir.Temperature
		hasGC = true
	}
	if ir.TopP != nil {
		gc["topP"] = *ir.TopP
		hasGC = true
	}
	if ir.MaxTokens > 0 {
		gc["maxOutputTokens"] = ir.MaxTokens
		hasGC = true
	}
	if len(ir.Stop) > 0 {
		gc["stopSequences"] = ir.Stop
		hasGC = true
	}
	if hasGC {
		raw["generationConfig"] = gc
	}

	return raw, nil
}

func irRoleToGemini(role IRRole) string {
	switch role {
	case IRRoleUser:
		return "user"
	case IRRoleAssistant:
		return "model"
	case IRRoleTool:
		return "user" // Gemini doesn't have a tool role; function responses go as user
	default:
		return "user"
	}
}

func encodeGeminiParts(parts []IRContentPart) []interface{} {
	var result []interface{}
	for _, part := range parts {
		switch part.Type {
		case IRPartText:
			result = append(result, map[string]interface{}{"text": part.Text})
		case IRPartToolCall:
			if part.ToolCall != nil {
				tc := map[string]interface{}{
					"name": part.ToolCall.Name,
				}
				if part.ToolCall.ArgumentsJSON != nil {
					tc["args"] = part.ToolCall.ArgumentsJSON
				} else if part.ToolCall.ArgumentsRaw != "" {
					tc["args"] = part.ToolCall.ArgumentsRaw
				}
				result = append(result, map[string]interface{}{"functionCall": tc})
			}
		case IRPartToolResult:
			if part.ToolResult != nil {
				fr := map[string]interface{}{
					"name": part.ToolResult.ToolCallID,
				}
				// Extract text content from the content parts
				var respText string
				for _, cp := range part.ToolResult.Content {
					if cp.Type == IRPartText {
						respText = cp.Text
						break
					}
				}
				fr["response"] = map[string]interface{}{"text": respText}
				result = append(result, map[string]interface{}{"functionResponse": fr})
			}
		case IRPartImage, IRPartAudio, IRPartFile:
			md := part.Metadata
			if md == nil {
				md = map[string]string{}
			}
			// For images/audio/video, use inlineData
			if part.Type == IRPartFile && md["file_uri"] != "" {
				result = append(result, map[string]interface{}{
					"fileData": map[string]interface{}{
						"mimeType": md["mime_type"],
						"fileUri":  md["file_uri"],
					},
				})
			} else {
				result = append(result, map[string]interface{}{
					"inlineData": map[string]interface{}{
						"mimeType": md["mime_type"],
						"data":     md["data"],
					},
				})
			}
		default:
			// reasoning, refusal — not used in Gemini input
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// DecodeResponse: Gemini raw → IR
// ---------------------------------------------------------------------------

func (a *geminiAdapter) DecodeResponse(raw map[string]interface{}) (*IRResponse, error) {
	resp := &IRResponse{
		ID:      getString(raw, "id"),
		Model:   getString(raw, "model"),
		Created: now(),
	}

	// Candidates
	if rawCandidates, ok := raw["candidates"].([]interface{}); ok && len(rawCandidates) > 0 {
		if cm, ok := rawCandidates[0].(map[string]interface{}); ok {
			// Content parts
			if content, ok := cm["content"].(map[string]interface{}); ok {
				if rawParts, ok := content["parts"].([]interface{}); ok {
					resp.Content = decodeGeminiParts(rawParts)
				}
			}
			// Finish reason
			if fr := getString(cm, "finishReason"); fr != "" {
				resp.StopReason = geminiFinishReasonToIR(fr)
			}
		}
	}

	// Usage metadata
	if um, ok := raw["usageMetadata"].(map[string]interface{}); ok {
		resp.Usage = &IRUsage{
			InputTokens:  getIntDefault(um, "promptTokenCount"),
			OutputTokens: getIntDefault(um, "candidatesTokenCount"),
			TotalTokens:  getIntDefault(um, "totalTokenCount"),
		}
	}

	return resp, nil
}

func geminiFinishReasonToIR(reason string) IRStopReason {
	switch reason {
	case "STOP":
		return IRStopEndTurn
	case "MAX_TOKENS":
		return IRStopMaxTokens
	case "SAFETY":
		return IRStopContentFilter
	case "RECITATION":
		return IRStopContentFilter
	case "OTHER", "FINISH_REASON_UNSPECIFIED":
		return IRStopEndTurn
	default:
		return IRStopEndTurn
	}
}

// ---------------------------------------------------------------------------
// EncodeResponse: IR → Gemini raw
// ---------------------------------------------------------------------------

func (a *geminiAdapter) EncodeResponse(ir *IRResponse) (map[string]interface{}, error) {
	raw := make(map[string]interface{})

	raw["id"] = ir.ID
	if ir.Created > 0 {
		raw["created"] = ir.Created
	}

	candidate := map[string]interface{}{}

	if len(ir.Content) > 0 {
		candidate["content"] = map[string]interface{}{
			"role":  "model",
			"parts": encodeGeminiParts(ir.Content),
		}
	}

	if ir.StopReason != "" {
		candidate["finishReason"] = irStopReasonToGemini(ir.StopReason)
	}

	raw["candidates"] = []interface{}{candidate}

	if ir.Usage != nil {
		raw["usageMetadata"] = map[string]interface{}{
			"promptTokenCount":     ir.Usage.InputTokens,
			"candidatesTokenCount": ir.Usage.OutputTokens,
			"totalTokenCount":      ir.Usage.TotalTokens,
		}
	}

	return raw, nil
}

func irStopReasonToGemini(reason IRStopReason) string {
	switch reason {
	case IRStopEndTurn:
		return "STOP"
	case IRStopMaxTokens, IRStopLength:
		return "MAX_TOKENS"
	case IRStopContentFilter:
		return "SAFETY"
	case IRStopToolUse:
		return "STOP"
	default:
		return "STOP"
	}
}

// ---------------------------------------------------------------------------
// Stream State
// ---------------------------------------------------------------------------

func (a *geminiAdapter) NewStreamState() interface{} {
	return &geminiStreamState{
		partsStarted: make(map[int]bool),
		lastPartIdx:  -1,
	}
}

// ---------------------------------------------------------------------------
// DecodeStreamEvent: Gemini SSE → IR stream events
// ---------------------------------------------------------------------------

func (a *geminiAdapter) DecodeStreamEvent(raw map[string]interface{}, state interface{}) ([]*IRStreamEvent, error) {
	st, ok := state.(*geminiStreamState)
	if !ok {
		return nil, fmt.Errorf("gemini: invalid stream state type %T", state)
	}

	if st.done {
		return nil, nil
	}

	var events []*IRStreamEvent

	// Extract candidate content parts
	rawCandidates, _ := raw["candidates"].([]interface{})
	if len(rawCandidates) == 0 {
		return nil, nil
	}
	cm, ok := rawCandidates[0].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	content, _ := cm["content"].(map[string]interface{})
	if content == nil {
		return nil, nil
	}

	rawParts, _ := content["parts"].([]interface{})

	// Process each part in this chunk. Each part's text is the FULL text so far
	// (not a delta), but in practice Gemini streaming sends incremental chunks
	// where each part's text is just the new content for that index.
	//
	// For simplicity and compatibility with common proxy implementations,
	// we treat the text as a delta for the part at its index.
	for i, rp := range rawParts {
		pm, ok := rp.(map[string]interface{})
		if !ok {
			continue
		}

		text := getString(pm, "text")

		// Track the part index: Gemini parts come in order, each chunk
		// may add more text to the same part.
		if st.lastPartIdx < i {
			st.lastPartIdx = i
		}

		if !st.partsStarted[i] {
			// First time seeing this part → start event
			st.partsStarted[i] = true
			events = append(events, &IRStreamEvent{
				Type:       IRStreamContentStart,
				Index:      i,
				ResponseID: st.responseID,
				Part:       &IRContentPart{Type: IRPartText},
			})
		}

		// Always emit a delta event with the new text
		if text != "" {
			events = append(events, &IRStreamEvent{
				Type:      IRStreamContentDelta,
				Index:     i,
				DeltaText: text,
			})
		}
	}

	// Check for finish reason — only present on the LAST chunk
	if finishReason := getString(cm, "finishReason"); finishReason != "" {
		// Emit stop events for all started parts
		for i := 0; i <= st.lastPartIdx; i++ {
			if st.partsStarted[i] {
				events = append(events, &IRStreamEvent{
					Type:  IRStreamContentStop,
					Index: i,
				})
			}
		}

		// Emit usage if available
		var usage *IRUsage
		if um, ok := raw["usageMetadata"].(map[string]interface{}); ok {
			usage = &IRUsage{
				InputTokens:  getIntDefault(um, "promptTokenCount"),
				OutputTokens: getIntDefault(um, "candidatesTokenCount"),
				TotalTokens:  getIntDefault(um, "totalTokenCount"),
			}
		}

		// Message delta with stop reason and usage
		events = append(events, &IRStreamEvent{
			Type:       IRStreamMessageDelta,
			StopReason: geminiFinishReasonToIR(finishReason),
			Usage:      usage,
		})

		// Done event
		events = append(events, &IRStreamEvent{
			Type:       IRStreamDone,
			ResponseID: st.responseID,
			Usage:      usage,
		})

		st.done = true
	}

	return events, nil
}

// ---------------------------------------------------------------------------
// EncodeStreamEvent: IR stream event → Gemini SSE payloads
// ---------------------------------------------------------------------------

func (a *geminiAdapter) EncodeStreamEvent(ir *IRStreamEvent, state interface{}) ([]map[string]interface{}, error) {
	st, ok := state.(*geminiStreamState)
	if !ok {
		return nil, fmt.Errorf("gemini: invalid stream state type %T", state)
	}

	switch ir.Type {
	case IRStreamMessageStart:
		st.responseID = ir.ResponseID
		st.model = ir.ResponseModel
		return nil, nil // Gemini doesn't have an explicit message_start event in streaming

	case IRStreamContentStart:
		if ir.Part != nil && ir.Part.Type == IRPartText {
			return []map[string]interface{}{
				{
					"candidates": []interface{}{
						map[string]interface{}{
							"content": map[string]interface{}{
								"role": "model",
								"parts": []interface{}{
									map[string]interface{}{
										"text": "",
									},
								},
							},
							"finishReason": nil,
						},
					},
				},
			}, nil
		}
		return nil, nil

	case IRStreamContentDelta:
		if ir.DeltaText != "" {
			return []map[string]interface{}{
				{
					"candidates": []interface{}{
						map[string]interface{}{
							"content": map[string]interface{}{
								"role": "model",
								"parts": []interface{}{
									map[string]interface{}{
										"text": ir.DeltaText,
									},
								},
							},
							"finishReason": nil,
						},
					},
				},
			}, nil
		}
		return nil, nil

	case IRStreamContentStop:
		return nil, nil // Gemini doesn't have explicit part-end events

	case IRStreamMessageDelta:
		finishReason := irStopReasonToGemini(ir.StopReason)
		payload := map[string]interface{}{
			"candidates": []interface{}{
				map[string]interface{}{
					"content":      map[string]interface{}{"role": "model", "parts": []interface{}{}},
					"finishReason": finishReason,
				},
			},
		}
		if ir.Usage != nil {
			payload["usageMetadata"] = map[string]interface{}{
				"promptTokenCount":     ir.Usage.InputTokens,
				"candidatesTokenCount": ir.Usage.OutputTokens,
				"totalTokenCount":      ir.Usage.TotalTokens,
			}
		}
		return []map[string]interface{}{payload}, nil

	case IRStreamDone:
		return nil, nil // Gemini won't receive this — handled by MessageDelta

	case IRStreamError:
		return nil, nil

	default:
		return nil, nil
	}
}
