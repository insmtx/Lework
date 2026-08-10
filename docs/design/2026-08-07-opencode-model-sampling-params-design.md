# opencode 按模型注入采样参数设计文档

- 日期：2026-08-07
- 状态：已确认
- 分支：`feat/opencode-model-sampling-params`

## 背景与目标

Leros 通过 `OPENCODE_CONFIG_CONTENT` 注入 opencode 子进程的 provider/model/agent 配置。
当前 `buildConfigContent`（`backend/agent/runtime/opencode/config.go`）硬编码了
`model.limit = { context: 200000, output: 16384 }`，且不注入任何采样参数
（topP / frequencyPenalty / presencePenalty）。

目标：让 opencode runtime 生成的配置能按模型注入这三个采样参数，并可覆盖
`limit.context` / `limit.output`。

参数键名使用**驼峰**（`topP` / `frequencyPenalty` / `presencePenalty`），经测试验证有效，
所有采样参数统一放在 opencode 的 `model.options` 中透传（见下方"源码确认"）。

## 决策（已与需求方确认）

| 项 | 决策 |
|---|---|
| 生效范围 | 仅 opencode runtime，不影响 modelrouter / 其他 runtime |
| 采样参数注入位置 | 全部放进 opencode `model.options`（驼峰键，已验证） |
| 采样参数数值来源 | config.yaml 预置（seed 写入 DB）→ DB `LLMModel.config`(jsonb) |
| limit 未设置 | 回退现有默认 `{context: 200000, output: 16384}` |
| 采样参数未设置 | 不注入（按需配置，C） |
| 数值类型 | TopP/FrequencyPenalty/PresencePenalty 用 `*float64` 以区分未设置 |

## 源码确认（opencode 侧识别什么）

- opencode 的 `streamText` 调用（`packages/opencode/src/session/llm.ts:313-320`）
  仅直接传 `temperature` / `topP` / `topK` / `maxOutputTokens`，**不会**直接传
  `frequencyPenalty` / `presencePenalty`。
- 采样参数合并发生在 `request.ts:91`：
  `options = base ⊕ model.options ⊕ agent.options`，再经
  `ProviderTransform.providerOptions(model, options)` 变成
  `streamText({ providerOptions: { <providerKey>: options } })`。
- `@ai-sdk/openai-compatible@2.0.41` 的 `getArgs` 将 `providerOptions` 中除
  `user / reasoningEffort / textVerbosity / strictJsonSchema` 外的键**原样展开**进请求体，
  不做 camelCase→snake_case 转换。因此 `model.options` 的键名就是请求体字段名。
- 最终确认：在 `model.options` 写入驼峰键 `topP` / `frequencyPenalty` / `presencePenalty`，
  会被原样放进请求体。该用法经需求方实测验证有效。

## 数据流（贯穿链路）

```
config.yaml LLMConfig(扩展)
  → seed/llm.go seedLLM → DB LLMModel.config(jsonb)
  → service/message_poster.go resolveWorkerTaskModel  读出
  → pkg/messaging.ModelOptions
  → worker/command/run/mapper.go → agentrun/domain.ModelOptions
  → worker/agentrun/preparer_impl.go → agent.ModelConfig
  → agent/runtime/opencode/config.go buildConfigContent → OPENCODE_CONFIG_CONTENT
  → agent/runtime/opencode/config.go buildServerEnv
```

## 配置示例

config.yaml：

```yaml
llm:
  provider: custom
  api_key: xxx
  model: Qwen3.6-27B
  base_url: https://xxx
  vision: false
  top_p: 0.95
  frequency_penalty: 0.1
  presence_penalty: 0
  limit:
    context: 82144
    output: 42768
```

写入 DB `LLMModel.config` 后的 jsonb：

```json
{
  "topP": 0.95,
  "frequencyPenalty": 0.1,
  "presencePenalty": 0,
  "limit": { "context": 82144, "output": 42768 }
}
```

（若 `vision: true` 则额外合并 `"vision": true`。）

最终 `OPENCODE_CONFIG_CONTENT` 中 model 条目形如：

```json
{
  "id": "Qwen3.6-27B",
  "name": "custom/Qwen3.6-27B",
  "limit": { "context": 82144, "output": 42768 },
  "options": { "topP": 0.95, "frequencyPenalty": 0.1, "presencePenalty": 0 }
}
```

## 改动范围

### 第 1 梯队：config.yaml 扩展 + seed

1. `backend/config/config.go`：`LLMConfig` 增采样参数字段。
2. `backend/internal/seed/llm.go`：`seedLLM` 合并采样参数进 `Config` map；
   `limit.output` 覆盖 `MaxTokens`。

### 第 2 梯队：类型 + 统一读取

3. `backend/types/llm_model.go`：增 `SamplingConfig` 结构 + 解析 helper
   `SamplingConfigFromLLMModelConfig`（对齐 `VisionFromConfig`）。
4. `backend/internal/llm/model.go`、`manager_db.go`：`ModelConfig` 增采样字段/helper。

### 第 3 梯队：贯穿传递

5. `backend/pkg/messaging/command.go`：`ModelOptions` 增 5 字段。
6. `backend/internal/worker/agentrun/domain/request.go`：`ModelOptions` 同增。
7. `backend/internal/service/message_poster.go`：`resolveWorkerTaskModel` 读出写入。
8. `backend/internal/worker/command/run/mapper.go`：透传。

### 第 4 梯队：agent 层 + opencode 注入

9. `backend/agent/runtime.go`：`ModelConfig` 增 5 字段。
10. `backend/internal/worker/agentrun/preparer_impl.go`：组装。
11. `backend/agent/runtime/opencode/types.go`：`modelConfig` 增 `Options map[string]any`。
12. `backend/agent/runtime/opencode/config.go`：`Limit` 覆盖/回退；`Options` 注驼峰采样参数。

## 测试要点

- `opencode/config_test.go`：options 驼峰键注入、limit 覆盖与回退。
- `seed/llm_test.go`：config.yaml 预置 → Config map 合并正确。
- `manager_db` / `mapper` / `message_poster` 相关测试随字段更新。
- config yaml 解析测试。

## 收尾

`gofmt -s` → `go vet` → `go build ./...` → `go test ./...`。
提交信息用中文，conventional commits，scope 用 `agent`。
