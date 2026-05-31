# ModelRouter — 多协议 LLM 路由与转换引擎

## 概述

ModelRouter 是 Leros 的 LLM 请求路由层，负责在两套协议之间透明转换请求和响应。
它让客户端（如 Codex CLI、Leros Agent Runtime）使用任意一种 LLM 协议（OpenAI Chat
Completions / Responses / Anthropic Messages / Gemini），而实际调用另一套协议的上游
模型服务。

核心设计哲学：**Adapter 负责协议语法，StreamAggregator 负责流生命周期**。

## 架构

```
                         ┌──────────────────┐
                         │   handler.go     │  SSE 编排、[DONE] 处理、EOF 兜底
                         └────────┬─────────┘
                                  │
                    ┌─────────────┴─────────────┐
                    │    StreamAggregator       │  流事件生命周期保证
                    │  - 补齐缺失的 start/stop  │
                    │  - dangling cleanup       │
                    │  - 幂等 Finalize          │
                    └─────────────┬─────────────┘
                                  │
              ┌───────────────────┼───────────────────┐
              │                   │                   │
    ┌─────────┴─────────┐ ┌──────┴──────┐ ┌─────────┴─────────┐
    │  Chat Adapter     │ │ Responses   │ │ Anthropic/Gemini  │
    │  (decode+encode)  │ │ Adapter     │ │ Adapters          │
    └───────────────────┘ └─────────────┘ └───────────────────┘
```

### 协议转换流程

```
客户端请求 (协议 A)
  → Adapter A.DecodeRequest  →  IR (Intermediate Representation)
  → NormalizeRequest         →  按上游能力裁剪 IR
  → Adapter B.EncodeRequest  →  上游请求 (协议 B)
  → 上游返回
  → Adapter B.DecodeStreamEvent → IR 事件
  → StreamAggregator.Process    → 补齐生命周期
  → Adapter A.EncodeStreamEvent → 客户端响应 (协议 A)
```

### 三层职责

| 层 | 文件 | 职责 |
|---|---|---|
| **Coordinator** | `handler.go` | HTTP 路由、SSE 行解析、`[DONE]`/EOF 处理、流完成编排 |
| **StreamAggregator** | `stream_aggregator.go` | 保证 IR 事件流完整性：补齐缺失的 Start/Stop/Done，dangling cleanup，幂等 Finalize |
| **Adapter** | `protocol_*.go` | 协议语法翻译。DecodeStreamEvent 只管一个事件→一个 (或多个) IR 事件，不做补齐、不猜状态 |

## 目录结构

```
modelrouter/
├── adapter.go                      # ProtocolAdapter 接口定义 + 协议常量 + 适配器注册
├── capability.go                   # 协议能力声明 + NormalizeRequest 归一化
├── debug.go                        # DebugLogger：请求级 JSON Lines 调试日志
├── handler.go                      # HTTP 路由注册 + 请求处理 + SSE 流编排
├── ir.go                           # IR 类型定义 + IRStreamEvent 类型
├── protocol_anthropic.go           # Anthropic Messages ↔ IR
├── protocol_gemini.go              # Google Gemini ↔ IR
├── protocol_openai_chat.go         # OpenAI Chat Completions ↔ IR
├── protocol_openai_responses.go    # OpenAI Responses ↔ IR
├── stream_aggregator.go            # 流事件生命周期保证器
├── utils.go                        # JSON 辅助函数 + IR 枚举映射
│
├── testdata/
│   └── responses_stream_text.jsonl # Responses API 完整流事件黄金文件 (13 行)
│
├── *_test.go                       # 单元测试 + 集成测试 + 协议 roundtrip 测试
├── test_golden.go                  # golden file 测试基础工具
└── test_integration.go             # 跨协议转换集成测试
```

## 模块说明

### `adapter.go` (156 行)

定义 `ProtocolAdapter` 接口——所有协议适配器的公共契约，以及 4 种协议常量和适配器注册机制。

对外接口：
```go
type ProtocolAdapter interface {
    Protocol() Protocol
    DecodeRequest(raw map[string]any) (*IRRequest, error)
    EncodeRequest(ir *IRRequest) (map[string]any, error)
    DecodeResponse(raw map[string]any) (*IRResponse, error)
    EncodeResponse(ir *IRResponse) (map[string]any, error)
    NewStreamState() any
    DecodeStreamEvent(raw map[string]any, state any) ([]*IRStreamEvent, error)
    EncodeStreamEvent(ir *IRStreamEvent, state any) ([]map[string]any, error)
}
```

### `ir.go` (192 行)

IR 类型定义。所有协议之间的通信通过此中间表示。

- `IRRequest` / `IRResponse` — 请求/响应模型
- `IRMessage` — 消息 (system/user/assistant/tool)
- `IRContentPart` — 内容片段 (text/tool_call/tool_result/reasoning/image/audio/file)
- `IRStreamEvent` — 流事件 (message_start/content_start/content_delta/content_stop/message_delta/done/error)
- `IRStreamEventType` — 7 种流事件类型

### `capability.go` (223 行)

定义协议能力集 (`CapabilitySet`) 和请求归一化逻辑 (`NormalizeRequest`)。

- 4 种预定义能力集：OpenAI Chat / Responses / Anthropic / Gemini
- `NormalizeRequest` 按目标协议能力裁剪 IR：移除不支持的内容类型、规范化 tool 消息结构
- `normalizeToolMessages` 防御层：合并孤儿 text part 到 ToolResult.Content

### `debug.go` (259 行)

请求级调试日志器。通过环境变量 `LEROS_MODELROUTER_DEBUG=true` 启用。

输出 JSON Lines 格式日志，每条数据行前有中文阶段说明行。记录完整请求链路：

`原始请求 → 元数据 → IR 解码/归一化 → 上游请求 → 上游/入口流数据 → 错误 → 完成`

### `handler.go` (683 行)

HTTP 路由注册和请求处理入口。

- `RegisterRoutes` 注册 4 个端点：`/v1/chat/completions`, `/v1/messages`, `/v1/responses`, `/v1/models/*`
- `handleModelRoute` 统一请求处理：解析 → IR 解码 → 归一化 → 上游编码 → 调用 → 响应编码
- `pipeConvertedSSE` 流式协议转换管道：SSE 解析 → Adapter Decode → Aggregator Process → Adapter Encode → SSE 写入
- `pipeRawSSE` 同协议直通（无需转换）
- `finalizeStream` 统一流完成逻辑：生成 completed 事件 + 按入口协议发终止标记

### `stream_aggregator.go` (417 行)

流事件生命周期保证器。位于 Adapter Decode 和 Adapter Encode 之间。

- `ProcessIREvent` 处理每个 IR 事件，自动补齐缺失的前置事件
- `Finalize` 在流结束时生成完成事件序列 (ContentStop → MessageDelta → Done)
- 幂等保护：第二次及后续 Finalize 调用为 no-op
- 多 output_index 并行隔离：每个 index 独立的 item 生命周期
- 空响应兜底：即使没有任何事件，Finalize 也生成有效的 completion

### `protocol_openai_chat.go` (1040 行)

OpenAI Chat Completions API ↔ IR。

- Decode: `messages[]` → `IRMessage[]`，多段 tool_calls 分片去重，chat index → IR global index 映射
- Encode: IR → `messages[]` + `tools[]`，tool_result.Content → content 字符串
- Stream: `chat.completion.chunk` ↔ IRStreamEvent，`[DONE]` 由 coordinator 处理

### `protocol_openai_responses.go` (1175 行)

OpenAI Responses API ↔ IR。

- Decode: `input[]` (message/function_call/function_call_output/reasoning) → `IRMessage[]`
- Encode: IR → `input[]` + `tools[]`，IRPartToolResult → function_call_output
- Stream: `response.created`/`output_item.added`/`output_text.delta`/`output_item.done`/`response.completed` ↔ IRStreamEvent

### `protocol_anthropic.go` (923 行)

Anthropic Messages API ↔ IR。

- Decode: `system`/`messages[]`/`tools[]` → IR，支持 `tool_use`/`tool_result`/`thinking` block
- Encode: IR → Anthropic 格式，`IRRoleTool` → `"user"` role (Anthropic 无独立 tool role)
- Stream: `message_start`/`content_block_start`/`content_block_delta`/`content_block_stop`/`message_stop` ↔ IRStreamEvent

### `protocol_gemini.go` (749 行)

Google Gemini API ↔ IR。

- Decode: `contents[]`/`systemInstruction`/`tools[]` → IR，`functionCall`/`functionResponse` 映射
- Encode: IR → `contents[]` + `functionDeclarations[]`，`IRRoleTool` → `"user"` role
- Stream: `candidates[].content.parts[].text` ↔ IRStreamEvent，`finishReason` 触发 Done

### `utils.go` (186 行)

内部工具函数。JSON 字段提取（getString/getInt/getFloat/getList）、JSON 解析、协议角色/状态映射、MarshalJSON。

## 外部接口

ModelRouter 通过以下方式暴露给外部：

- `DefaultStore()` — 获取模型配置存储单例
- `RegisterRoutes(r gin.IRouter)` — 注册 HTTP 端点
- `UpstreamConfig` — 模型上游配置结构体，由调用方填充
- `DebugLogger` — 调试日志器，通过 `LEROS_MODELROUTER_DEBUG=true` 启用

## 调试

```bash
# 启用调试日志
export LEROS_MODELROUTER_DEBUG=true

# 查看日志
cat logs/modelrouter/<uuid>.jsonl

# 只查看阶段摘要
grep '^\[' logs/modelrouter/<uuid>.jsonl
```
