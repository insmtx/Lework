package modelrouter

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/uuid"

	"github.com/insmtx/Leros/backend/pkg/llmprotocol"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DebugLogger — 请求级调试日志，仅当环境变量 LEROS_MODELROUTER_DEBUG=true 时启用。
//
// 每次请求生成一个以 UUID 命名的 JSON Lines 文件，chunk 前后自动插入换行分隔。
// 请求结束时通过 stderr 输出日志文件的完整路径。
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const defaultDebugLogDir = "./logs/modelrouter"

// DebugLogger 记录一次完整的模型路由请求生命周期。
// 未启用时所有方法均为空操作，无性能开销。
type DebugLogger struct {
	enabled   bool
	requestID string
	filePath  string
	file      *os.File
	mu        sync.Mutex
	startTime time.Time
}

// NewDebugLogger 创建调试日志器。enabled 为 false 时返回空操作实例。
func NewDebugLogger(enabled bool) *DebugLogger {
	dl := &DebugLogger{enabled: enabled, startTime: time.Now()}
	if !enabled {
		return dl
	}

	dl.requestID = uuid.New().String()

	logDir := defaultDebugLogDir

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		dl.enabled = false
		return dl
	}

	dl.filePath = filepath.Join(logDir, dl.requestID+".jsonl")
	f, err := os.OpenFile(dl.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		dl.enabled = false
		return dl
	}

	dl.file = f
	return dl
}

// Close 关闭日志文件并输出完成信息。空操作实例安全调用。
func (dl *DebugLogger) Close() {
	if !dl.enabled {
		return
	}
	durationMs := time.Since(dl.startTime).Milliseconds()
	dl.writeEvent("done", map[string]interface{}{
		"duration_ms": durationMs,
		"file_path":   dl.filePath,
	})
	if dl.file != nil {
		dl.file.Close()
		dl.file = nil
	}
	// 输出日志文件完整路径到 stderr，方便定位
	fmt.Fprintf(os.Stderr, "[modelrouter-debug] 日志已写入: %s (耗时 %dms)\n", dl.filePath, durationMs)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 请求阶段 — 日志方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LogRequestMeta 记录请求元信息（入口协议、上游协议、模型名、是否流式）。
func (dl *DebugLogger) LogRequestMeta(entryProtocol, upstreamProtocol llmprotocol.Protocol, model string, stream bool) {
	dl.writeEvent("meta", map[string]interface{}{
		"entry_protocol":    entryProtocol,
		"upstream_protocol": upstreamProtocol,
		"model":             model,
		"stream":            stream,
	})
}

// LogOriginalRequest 记录原始请求体（第 1 步：客户端发送的原始 JSON）。
func (dl *DebugLogger) LogOriginalRequest(body []byte) {
	dl.writeJSONEvent("request_original", body)
}

// LogIRDecoded 记录从入口协议解码后的 IR（第 3 步）。
func (dl *DebugLogger) LogIRDecoded(ir *llmprotocol.IRRequest) {
	dl.writeStructEvent("ir_decoded", ir)
}

// LogIRNormalized 记录经过能力裁剪后的归一化 IR（第 4 步）。
func (dl *DebugLogger) LogIRNormalized(ir *llmprotocol.IRRequest) {
	dl.writeStructEvent("ir_normalized", ir)
}

// LogUpstreamRequest 记录发送给上游的请求体（第 5 步：转换后的上游协议 JSON）。
func (dl *DebugLogger) LogUpstreamRequest(body []byte) {
	dl.writeJSONEvent("request_upstream", body)
}

// LogUpstreamResponse 记录上游返回的原始响应体（非流式）。
func (dl *DebugLogger) LogUpstreamResponse(body []byte) {
	dl.writeJSONEvent("response_upstream", body)
}

// LogUpstreamErrorResponse 记录上游返回的错误响应体（非流式）。
func (dl *DebugLogger) LogUpstreamErrorResponse(body []byte) {
	dl.writeJSONEvent("response_upstream_error", body)
}

// LogEntryResponse 记录转换后返回给客户端的最终响应体（非流式）。
func (dl *DebugLogger) LogEntryResponse(body []byte) {
	dl.writeJSONEvent("response_entry", body)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 流式方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LogStreamChunkSeparator 写入空行，在 chunk 组之间形成视觉分隔。
func (dl *DebugLogger) LogStreamChunkSeparator() {
	if !dl.enabled || dl.file == nil {
		return
	}
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.file.WriteString("\n")
}

// LogUpstreamStreamChunk 记录上游原始 SSE chunk。
func (dl *DebugLogger) LogUpstreamStreamChunk(data []byte) {
	dl.writeEvent("stream_upstream_chunk", map[string]interface{}{
		"data": string(data),
	})
}

// LogEntryStreamChunk 记录转换后的入口协议 SSE chunk。
func (dl *DebugLogger) LogEntryStreamChunk(data []byte) {
	dl.writeEvent("stream_entry_chunk", map[string]interface{}{
		"data": string(data),
	})
}

// LogError 记录特定阶段的错误信息。
func (dl *DebugLogger) LogError(stage string, err error) {
	dl.writeEvent("error", map[string]interface{}{
		"stage":   stage,
		"message": err.Error(),
	})
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 内部辅助方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (dl *DebugLogger) writeEvent(event string, data map[string]interface{}) {
	if !dl.enabled || dl.file == nil {
		return
	}

	record := map[string]interface{}{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"event": event,
	}
	for k, v := range data {
		record[k] = v
	}

	summary := eventSummary(event, data)

	b, err := sonic.Marshal(record)
	if err != nil {
		return
	}

	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.file.Write(append([]byte(summary+" "), '\n'))
	dl.file.Write(append(b, '\n'))
}

func eventSummary(event string, data map[string]interface{}) string {
	switch event {
	case "meta":
		return fmt.Sprintf("[模型路由] 入口=%v → 上游=%v, 模型=%v, 流式=%v",
			data["entry_protocol"], data["upstream_protocol"], data["model"], data["stream"])
	case "request_original":
		return "[原始请求]"
	case "ir_decoded":
		return "[IR 解码]"
	case "ir_normalized":
		return "[IR 归一化]"
	case "request_upstream":
		return "[上游请求]"
	case "response_upstream":
		return "[上游响应]"
	case "response_upstream_error":
		return "[上游错误响应]"
	case "response_entry":
		return "[入口响应]"
	case "stream_upstream_chunk":
		return "[上游流数据]"
	case "stream_entry_chunk":
		return "[入口流数据]"
	case "error":
		return fmt.Sprintf("[错误] %v: %v", data["stage"], data["message"])
	case "done":
		return fmt.Sprintf("[完成] %vms, %v", data["duration_ms"], data["file_path"])
	default:
		return event
	}
}

// writeJSONEvent 将 body 解码为 JSON 后以内联对象写入日志记录。
func (dl *DebugLogger) writeJSONEvent(event string, body []byte) {
	if !dl.enabled || dl.file == nil {
		return
	}

	var parsed interface{}
	if err := sonic.Unmarshal(body, &parsed); err != nil {
		parsed = string(body)
	}

	dl.writeEvent(event, map[string]interface{}{
		"body": parsed,
	})
}

// writeStructEvent 将结构体序列化后以内联对象写入日志记录。
func (dl *DebugLogger) writeStructEvent(event string, v interface{}) {
	if !dl.enabled || dl.file == nil {
		return
	}

	b, err := sonic.Marshal(v)
	if err != nil {
		return
	}

	var parsed interface{}
	sonic.Unmarshal(b, &parsed)

	dl.writeEvent(event, map[string]interface{}{
		"body": parsed,
	})
}
