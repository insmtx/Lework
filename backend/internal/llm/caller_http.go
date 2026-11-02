package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"

	"github.com/insmtx/Lework/backend/pkg/llmprotocol"
	"github.com/ygpkg/yg-go/logs"
)

const (
	defaultSSEBufferSize  = 64 * 1024
	defaultSSEMaxScanSize = 1024 * 1024
)

type UpstreamError struct {
	StatusCode int
	Body       []byte
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream returned status %d: %s", e.StatusCode, string(e.Body))
}

type CallerHTTP struct {
	httpClient *http.Client
	recorder   Recorder
}

func NewCallerHTTP(client *http.Client, recorder Recorder) *CallerHTTP {
	if client == nil {
		client = &http.Client{}
	}
	return &CallerHTTP{httpClient: client, recorder: recorder}
}

var _ Caller = (*CallerHTTP)(nil)

func (c *CallerHTTP) callerTypeFromCtx(ctx context.Context) string {
	if ct := GetCtxString(ctx, CtxCallerType); ct != "" {
		return ct
	}
	return CallerTypeHTTPProxy
}

func (c *CallerHTTP) Call(ctx context.Context, orgID uint, req *CallRequest) (*CallResult, error) {
	return nil, fmt.Errorf("CallerHTTP does not support Call, use CallRaw")
}

func (c *CallerHTTP) Stream(ctx context.Context, orgID uint, req *CallRequest, sink StreamSink) (*CallResult, error) {
	return nil, fmt.Errorf("CallerHTTP does not support Stream, use StreamRaw")
}

func (c *CallerHTTP) CallRaw(ctx context.Context, orgID uint, cfg *ModelConfig, body []byte) (*CallResult, error) {
	startedAt := time.Now()

	proto := protocolFromProvider(cfg.Provider)
	apiPath := llmprotocol.UpstreamAPIPath(proto, false)
	endpointURL := BuildLLMEndpointURL(cfg.BaseURL, cfg.BaseURLHasV1) + apiPath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(body))
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, false, startedAt,
			fmt.Errorf("create request: %w", err), string(body))
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, cfg)

	logs.InfoContextf(ctx, "[LLM-CALL] org=%d model=%s provider=%s url=%s body=%s",
		orgID, cfg.ModelName, cfg.Provider, endpointURL, truncateForLog(string(body), 500))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, false, startedAt,
			fmt.Errorf("upstream request failed: %w", err), string(body))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, false, startedAt,
			fmt.Errorf("read upstream response: %w", err), string(body))
	}

	if resp.StatusCode >= 400 {
		c.recordHTTPErrorWithCode(ctx, orgID, cfg, false, startedAt, resp.StatusCode,
			fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(respBody)),
			string(body), string(respBody))
		return &CallResult{
			RawResponseBody: respBody,
		}, &UpstreamError{StatusCode: resp.StatusCode, Body: respBody}
	}

	usage := extractUsageFromResponse(respBody)

	finishedAt := time.Now()
	latencyMS := finishedAt.Sub(startedAt).Milliseconds()

	record := &CallRecord{
		OrgID:         orgID,
		ModelID:       cfg.ID,
		Provider:      cfg.Provider,
		ModelName:     cfg.ModelName,
		EntryProtocol: string(proto),
		IsStream:      false,
		LatencyMS:     latencyMS,
		StatusCode:    resp.StatusCode,
		Success:       true,
		CallerType:    c.callerTypeFromCtx(ctx),
		ReqID:         GetCtxString(ctx, CtxReqID),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		ProjectID:     GetCtxUint(ctx, CtxProjectID),
		SessionID:     GetCtxUint(ctx, CtxSessionID),
		MessageID:     GetCtxUint(ctx, CtxMessageID),
		AssistantID:   GetCtxUint(ctx, CtxAssistantID),
		Uin:           GetCtxUint(ctx, CtxUin),
		TraceID:       GetCtxString(ctx, CtxTraceID),
		ClientIP:      GetCtxString(ctx, CtxClientIP),
		Input:         string(body),
		Output:        string(respBody),
		InputLen:      len(body),
		OutputLen:     len(respBody),
	}
	if usage != nil {
		record.InputTokens = usage.InputTokens
		record.OutputTokens = usage.OutputTokens
		record.TotalTokens = usage.TotalTokens
		record.PromptTokens = usage.PromptTokens
		record.CacheHitTokens = usage.CacheHitTokens
		record.CacheMissTokens = usage.CacheMissTokens
	}

	c.recordCall(ctx, record)

	return &CallResult{
		Usage:           usage,
		Record:          record,
		RawResponseBody: respBody,
	}, nil
}

func (c *CallerHTTP) StreamRaw(ctx context.Context, orgID uint, cfg *ModelConfig, body []byte, rawChunkSink RawChunkSink) (*CallResult, error) {
	startedAt := time.Now()

	proto := protocolFromProvider(cfg.Provider)
	apiPath := llmprotocol.UpstreamAPIPath(proto, false)
	endpointURL := BuildLLMEndpointURL(cfg.BaseURL, cfg.BaseURLHasV1) + apiPath

	streamBody, err := ensureStreamField(body)
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, true, startedAt,
			fmt.Errorf("ensure stream field: %w", err), string(body))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(streamBody))
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, true, startedAt,
			fmt.Errorf("create request: %w", err), string(streamBody))
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, cfg)

	logs.InfoContextf(ctx, "[LLM-STREAM] org=%d model=%s provider=%s url=%s body=%s",
		orgID, cfg.ModelName, cfg.Provider, endpointURL, truncateForLog(string(streamBody), 500))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.recordHTTPError(ctx, orgID, cfg, true, startedAt,
			fmt.Errorf("upstream stream request failed: %w", err), string(streamBody))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		c.recordHTTPErrorWithCode(ctx, orgID, cfg, true, startedAt, resp.StatusCode,
			fmt.Errorf("upstream returned status %d: %s", resp.StatusCode, string(respBody)),
			string(streamBody), string(respBody))
		return &CallResult{
			RawResponseBody: respBody,
		}, &UpstreamError{StatusCode: resp.StatusCode, Body: respBody}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, defaultSSEBufferSize), defaultSSEMaxScanSize)

	var currentData strings.Builder
	var lastUsage *Usage
	acc := &streamOutputAccumulator{protocol: proto}

	flushData := func(data string) {
		if u := extractUsageFromSSEData(data); u != nil {
			lastUsage = u
		}
		acc.feed(data)
	}

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				flushData(currentData.String())
				currentData.Reset()
				if rawChunkSink != nil {
					_ = rawChunkSink.EmitRawChunk(ctx, []byte("data: [DONE]\n\n"))
				}
				continue
			}
			currentData.WriteString(data)
			continue
		}

		if line == "" && currentData.Len() > 0 {
			dataStr := currentData.String()
			currentData.Reset()
			flushData(dataStr)

			chunk := []byte("data: " + dataStr + "\n\n")
			if rawChunkSink != nil {
				if err := rawChunkSink.EmitRawChunk(ctx, chunk); err != nil {
					break
				}
			}
		}
	}

	if currentData.Len() > 0 {
		dataStr := currentData.String()
		flushData(dataStr)
		chunk := []byte("data: " + dataStr + "\n\n")
		if rawChunkSink != nil {
			_ = rawChunkSink.EmitRawChunk(ctx, chunk)
		}
	}

	finishedAt := time.Now()
	latencyMS := finishedAt.Sub(startedAt).Milliseconds()

	record := &CallRecord{
		OrgID:         orgID,
		ModelID:       cfg.ID,
		Provider:      cfg.Provider,
		ModelName:     cfg.ModelName,
		EntryProtocol: string(proto),
		IsStream:      true,
		LatencyMS:     latencyMS,
		StatusCode:    resp.StatusCode,
		Success:       true,
		CallerType:    c.callerTypeFromCtx(ctx),
		ReqID:         GetCtxString(ctx, CtxReqID),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		ProjectID:     GetCtxUint(ctx, CtxProjectID),
		SessionID:     GetCtxUint(ctx, CtxSessionID),
		MessageID:     GetCtxUint(ctx, CtxMessageID),
		AssistantID:   GetCtxUint(ctx, CtxAssistantID),
		Uin:           GetCtxUint(ctx, CtxUin),
		TraceID:       GetCtxString(ctx, CtxTraceID),
		ClientIP:      GetCtxString(ctx, CtxClientIP),
		Input:         string(streamBody),
		Output:        acc.String(),
		InputLen:      len(streamBody),
		OutputLen:     len(acc.String()),
	}
	if lastUsage != nil {
		record.InputTokens = lastUsage.InputTokens
		record.OutputTokens = lastUsage.OutputTokens
		record.TotalTokens = lastUsage.TotalTokens
		record.PromptTokens = lastUsage.PromptTokens
		record.CacheHitTokens = lastUsage.CacheHitTokens
		record.CacheMissTokens = lastUsage.CacheMissTokens
	}

	c.recordCall(ctx, record)

	return &CallResult{
		Usage:  lastUsage,
		Record: record,
	}, nil
}

func (c *CallerHTTP) recordCall(ctx context.Context, record *CallRecord) {
	if c.recorder == nil {
		return
	}
	if err := c.recorder.RecordCall(ctx, record); err != nil {
		logs.ErrorContextf(ctx, "[ERROR] failed to record llm call: %v", err)
	}
}

func (c *CallerHTTP) recordHTTPError(ctx context.Context, orgID uint, cfg *ModelConfig, isStream bool, startedAt time.Time, err error, input string) (*CallResult, error) {
	return c.recordHTTPErrorWithCode(ctx, orgID, cfg, isStream, startedAt, 502, err, input, "")
}

func (c *CallerHTTP) recordHTTPErrorWithCode(ctx context.Context, orgID uint, cfg *ModelConfig, isStream bool, startedAt time.Time, statusCode int, err error, input string, output string) (*CallResult, error) {
	finishedAt := time.Now()
	latencyMS := finishedAt.Sub(startedAt).Milliseconds()

	record := &CallRecord{
		OrgID:         orgID,
		ModelID:       cfg.ID,
		Provider:      cfg.Provider,
		ModelName:     cfg.ModelName,
		EntryProtocol: string(protocolFromProvider(cfg.Provider)),
		IsStream:      isStream,
		LatencyMS:     latencyMS,
		StatusCode:    statusCode,
		Success:       false,
		Message:       err.Error(),
		CallerType:    c.callerTypeFromCtx(ctx),
		ReqID:         GetCtxString(ctx, CtxReqID),
		StartedAt:     startedAt,
		FinishedAt:    finishedAt,
		ProjectID:     GetCtxUint(ctx, CtxProjectID),
		SessionID:     GetCtxUint(ctx, CtxSessionID),
		MessageID:     GetCtxUint(ctx, CtxMessageID),
		AssistantID:   GetCtxUint(ctx, CtxAssistantID),
		Uin:           GetCtxUint(ctx, CtxUin),
		TraceID:       GetCtxString(ctx, CtxTraceID),
		ClientIP:      GetCtxString(ctx, CtxClientIP),
		Input:         input,
		Output:        output,
		InputLen:      len(input),
		OutputLen:     len(output),
	}
	c.recordCall(ctx, record)
	return nil, err
}

func setAuthHeader(req *http.Request, cfg *ModelConfig) {
	switch strings.ToLower(cfg.Provider) {
	case "anthropic":
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
}

func protocolFromProvider(provider string) llmprotocol.Protocol {
	switch strings.ToLower(provider) {
	case "anthropic":
		return llmprotocol.ProtocolAnthropicMessages
	case "gemini":
		return llmprotocol.ProtocolGemini
	default:
		return llmprotocol.ProtocolOpenAIChat
	}
}

func ensureStreamField(body []byte) ([]byte, error) {
	var raw map[string]interface{}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse body: %w", err)
	}
	raw["stream"] = true
	return sonic.Marshal(raw)
}

func extractUsageFromResponse(body []byte) *Usage {
	var raw map[string]interface{}
	if err := sonic.Unmarshal(body, &raw); err != nil {
		return nil
	}
	usageMap, _ := raw["usage"].(map[string]interface{})
	if usageMap == nil {
		return nil
	}
	return buildUsageFromMap(usageMap)
}

func extractUsageFromSSEData(data string) *Usage {
	var raw map[string]interface{}
	if err := sonic.Unmarshal([]byte(data), &raw); err != nil {
		return nil
	}
	usageMap, _ := raw["usage"].(map[string]interface{})
	if usageMap == nil {
		return nil
	}
	return buildUsageFromMap(usageMap)
}

func buildUsageFromMap(usageMap map[string]interface{}) *Usage {
	promptTokens := getIntFromMap(usageMap, "prompt_tokens")
	completionTokens := getIntFromMap(usageMap, "completion_tokens")
	totalTokens := getIntFromMap(usageMap, "total_tokens")

	var cachedTokens int
	if details, ok := usageMap["prompt_tokens_details"].(map[string]interface{}); ok {
		cachedTokens = getIntFromMap(details, "cached_tokens")
	}

	return &Usage{
		InputTokens:    promptTokens,
		OutputTokens:   completionTokens,
		TotalTokens:    totalTokens,
		PromptTokens:   int64(promptTokens),
		CacheHitTokens: int64(cachedTokens),
	}
}

func getIntFromMap(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		val, _ := n.Int64()
		return int(val)
	default:
		return 0
	}
}

type streamOutputAccumulator struct {
	protocol       llmprotocol.Protocol
	parts          []string
	reasoningParts []string
	toolCall       *streamToolCallAccumulator
}

type streamToolCallAccumulator struct {
	id      string
	name    string
	args    strings.Builder
	started bool
}

func (a *streamOutputAccumulator) feed(data string) {
	content, reasoning := extractStreamDelta(data, a.protocol)
	if content != "" {
		a.parts = append(a.parts, content)
	}
	if reasoning != "" {
		a.reasoningParts = append(a.reasoningParts, reasoning)
	}

	a.feedToolCall(data)
}

func (a *streamOutputAccumulator) feedToolCall(data string) {
	switch a.protocol {
	case llmprotocol.ProtocolOpenAIChat:
		a.feedOpenAIToolCall(data)
	case llmprotocol.ProtocolAnthropicMessages:
		a.feedAnthropicToolUse(data)
	}
}

func (a *streamOutputAccumulator) feedOpenAIToolCall(data string) {
	var raw map[string]interface{}
	if err := sonic.Unmarshal([]byte(data), &raw); err != nil {
		return
	}
	choices, _ := raw["choices"].([]interface{})
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]interface{})
	if choice == nil {
		return
	}
	delta, _ := choice["delta"].(map[string]interface{})
	if delta == nil {
		return
	}
	toolCalls, _ := delta["tool_calls"].([]interface{})
	if len(toolCalls) == 0 {
		return
	}
	tc, _ := toolCalls[0].(map[string]interface{})
	if tc == nil {
		return
	}

	if a.toolCall == nil {
		a.toolCall = &streamToolCallAccumulator{}
	}
	tcAcc := a.toolCall

	if id, _ := tc["id"].(string); id != "" {
		tcAcc.id = id
	}
	if fn, _ := tc["function"].(map[string]interface{}); fn != nil {
		if name, _ := fn["name"].(string); name != "" {
			tcAcc.name = name
		}
		if args, _ := fn["arguments"].(string); args != "" {
			tcAcc.args.WriteString(args)
		}
	}
	if !tcAcc.started && tcAcc.name != "" {
		tcAcc.started = true
	}
}

func (a *streamOutputAccumulator) feedAnthropicToolUse(data string) {
	var raw map[string]interface{}
	if err := sonic.Unmarshal([]byte(data), &raw); err != nil {
		return
	}
	eventType, _ := raw["type"].(string)

	switch eventType {
	case "content_block_start":
		block, _ := raw["content_block"].(map[string]interface{})
		if block == nil {
			return
		}
		blockType, _ := block["type"].(string)
		if blockType != "tool_use" {
			return
		}
		if a.toolCall == nil {
			a.toolCall = &streamToolCallAccumulator{}
		}
		a.toolCall.id, _ = block["id"].(string)
		a.toolCall.name, _ = block["name"].(string)
		a.toolCall.started = true

	case "content_block_delta":
		delta, _ := raw["delta"].(map[string]interface{})
		if delta == nil {
			return
		}
		deltaType, _ := delta["type"].(string)
		if deltaType != "input_json_delta" {
			return
		}
		if a.toolCall == nil {
			return
		}
		if json, _ := delta["partial_json"].(string); json != "" {
			a.toolCall.args.WriteString(json)
		}
	}
}

func (a *streamOutputAccumulator) String() string {
	tc := a.toolCall
	if tc != nil && tc.name != "" {
		label := "tool_call"
		switch a.protocol {
		case llmprotocol.ProtocolAnthropicMessages:
			label = "tool_use"
		}
		a.parts = append(a.parts, fmt.Sprintf("[%s %s: %s]", label, tc.name, tc.args.String()))
	}
	result := strings.Join(a.parts, "")
	if len(a.reasoningParts) > 0 {
		result += "\n[reasoning] " + strings.Join(a.reasoningParts, "")
	}
	return result
}

func extractStreamDelta(data string, proto llmprotocol.Protocol) (content string, reasoning string) {
	var raw map[string]interface{}
	if err := sonic.Unmarshal([]byte(data), &raw); err != nil {
		return "", ""
	}

	switch proto {
	case llmprotocol.ProtocolAnthropicMessages:
		return extractAnthropicTextDelta(raw), ""
	default:
		return extractOpenAIDelta(raw)
	}
}

func extractOpenAIDelta(raw map[string]interface{}) (content string, reasoning string) {
	choices, ok := raw["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", ""
	}
	choice, _ := choices[0].(map[string]interface{})
	if choice == nil {
		return "", ""
	}
	delta, _ := choice["delta"].(map[string]interface{})
	if delta == nil {
		return "", ""
	}
	content, _ = delta["content"].(string)
	reasoning, _ = delta["reasoning_content"].(string)
	return
}

func extractAnthropicTextDelta(raw map[string]interface{}) string {
	eventType, _ := raw["type"].(string)
	switch eventType {
	case "content_block_start":
		block, _ := raw["content_block"].(map[string]interface{})
		if block == nil {
			return ""
		}
		blockType, _ := block["type"].(string)
		if blockType == "text" {
			text, _ := block["text"].(string)
			return text
		}
	case "content_block_delta":
		delta, _ := raw["delta"].(map[string]interface{})
		if delta == nil {
			return ""
		}
		deltaType, _ := delta["type"].(string)
		if deltaType == "text_delta" {
			text, _ := delta["text"].(string)
			return text
		}
	}
	return ""
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
