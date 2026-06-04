# ModelRouter — Worker 内置 LLM 模型代理

## 概述

`backend/internal/modelrouter` 是 Worker 进程内的 LLM 模型代理层，负责注册 HTTP 端点、解析模型上游配置、转发请求、编排 SSE 流和记录调试日志。

LLM 协议转换内核已经拆分到公共包：

```go
github.com/insmtx/Leros/backend/pkg/llmprotocol
```

`llmprotocol` 负责 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages、Gemini 与统一 IR 之间的请求/响应/流事件转换。

## 职责边界

| 包 | 职责 |
|---|---|
| `backend/internal/modelrouter` | Worker 内部 HTTP 模型代理、上游配置存储、HTTP 转发、SSE 编排、debug log |
| `backend/pkg/llmprotocol` | 协议枚举、IR、协议 adapter、能力归一化、StreamAggregator、协议 golden tests |

## 当前文件

| 文件 | 说明 |
|---|---|
| `config.go` | 内部上游模型配置 `UpstreamConfig` |
| `handler.go` | Gin 路由注册、请求处理、上游 HTTP 调用、SSE 流转换编排 |
| `debug.go` | 请求级 JSON Lines 调试日志 |
| `handler_test.go` | 模型代理 HTTP 行为测试 |

## 外部接口

- `DefaultStore()` — 获取进程级模型配置存储单例。
- `RegisterRoutes(r gin.IRouter)` — 注册 Worker 模型代理端点。
- `UpstreamConfig` — Worker 内部上游模型配置，由 runtime lifecycle 写入。

## 调试

```bash
export LEROS_MODELROUTER_DEBUG=true
cat logs/modelrouter/<uuid>.jsonl
```
