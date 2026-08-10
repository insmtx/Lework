# OpenCode Runtime 多模态（图片/PDF/音视频）支持设计

> 日期：2026-08-06（随实现同步修订）
> 分支：`feat/opencode-multimodal`
> 目标 opencode 版本：**v1.18.13**

## 背景与现状

Leros 通过 `backend/agent/runtime/opencode/` 包以 `serve` 子进程模式驱动 opencode CLI，两者通过 HTTP REST + SSE 通信。当前该 runtime 只支持纯文本：

- 附件在 `preparer` 层被下载并注入工作区，但注入给 runtime 的 prompt 只有 `agent.Message{Role,Content}` 字符串与单个 text part（`preparer_impl.go:356-362`、`invoker.go:196-234`）。
- 用户上传的图片/PDF/音视频只能被模型作为文件用工具读取，**无法作为多模态输入**直接传给模型。

本设计的目标是打通"DB 附件 → run 请求 → agent runtime → opencode message part 转 `data:` base64 的 file part"这条链路，让支持视觉能力的模型能直接"看"图片。**当模型声明视觉能力（`vision=true`）时，适配器仅声明图片输入能力（`modalities.input:[text,image]`、`output:[text]`）**，opencode 据此将图片附件原样传给模型；PDF/音视频已退出多模态管线，统一按工作区路径读取。未声明视觉能力的模型不写 `modalities`，对图片附件走 opencode 的优雅降级。图片是主路径：`llmprotocol` 代理层以 `image_url` 内容块编码图片。

## 关键前提（已从 v1.18.13 源码核实）

为了让方案建立在真实接口之上而非猜测，我们对 opencode **v1.18.13** 源码进行了核对（本地 checkout 于 tag `a105350812`）：

| 事实 | 位置 |
|---|---|
| `opencode serve` 的 instance API 根路径为 `/session`，发送端点是 **`POST /session/:sessionID/message`** | `packages/opencode/src/server/routes/instance/httpapi/groups/session.ts:29,82` |
| 请求体 schema `PromptInput` 含 `parts[TextPartInput \| FilePartInput \| AgentPartInput \| SubtaskPartInput]`，字段与 Leros 的 `messageRequest` 完全对齐 | `packages/opencode/src/session/prompt.ts:1499-1521` |
| 图片以 **`FilePartInput`** 传入：`{id?, type:"file", mime, filename?, url, source?}` | `packages/schema/src/v1/session.ts:413-421` |
| `url` 支持 `data:` base64 内联（`data:<mime>;base64,<bytes>`） | `session/prompt.ts:787` |
| `mime` 以 `image/` 开头时走图片路径，并触发 `image.normalize`（photon-wasm 自动 resize，缩略失败则原样返回） | `session/prompt.ts:1005-1014` |
| 模型无视觉能力时，opencode 依据 `capabilities.input.image` 自动降级/擦除图片 part，无需调用方按模型门控 | `transform.ts:10,408-465` |
| 模型 config `attachment` 能力开关与 `modalities.input` 合并 | `provider/provider.ts:1463-1468` |

> 结论：Leros 适配器已符合 v1.18.13 的 `/session/:sessionID/message` 协议。只需在 `parts[]` 里追加 `{type:"file", mime, filename, url:"data:..."}` 即可实现多模态输入，且上游 `modelrouter`/`llmprotocol` 代理层已原生支持图片 IR（`capability.go:14`、`ir.go`），后续转发无需改动。

## 目标

1. 用户上传的 `image/*` 附件在 opencode runtime 中作为多模态输入传给模型；PDF/音视频附件统一按工作区路径读取，不注入多模态输入。
2. 保持纯文本附件、以及 claude/codex/native 等其它 runtime 的行为完全不变（最小侵入）。
3. 视觉能力按模型声明（`llm_model.config.vision`）决定，**对配置为无视觉的模型优雅降级**，不做前端盲目门控。

## 决策要点（合理性与健壮性优先）

- **存储位置**：视觉标志放在 `llm_model.config`（JSONB，`types/llm_model.go:70`）的简化布尔键 `vision`，**不新增 DB 列**。理由：
  - `config` 注释定位即为"能力标记、扩展配置"（`llm_model.go:69`）。
  - 加 JSONB 键**零 DB 迁移**，新增列则触发 AutoMigrate schema 变更。
  - 多模态是能力集，长期可扩展为 `modalities`；用 JSONB 承载避免"每加一种模态加一列"。
- **默认值**：`vision` 缺省即 **`false`**（未声明 → 视为无视觉）。理由（健壮性）：
  - `leros-provider` 是 config-only provider，opencode 侧 `parsed.models = existing?.models ?? {}`（`provider.ts:1436`），`existingModel` 为空，**完全采信我们声明的 `modalities`**，无内置目录覆盖。
  - `false` → 不写 `modalities` → opencode 判定各 `capabilities.input.*=false` → 走 `unsupportedParts` **优雅降级**（图被替换为提示文本，模型正常回答其余内容），**绝不会让整轮 400 失败**。
  - `true` → 仅声明图片能力 `modalities:{input:[text,image], output:[text]}` → 图片附件原样传模型，PDF/音视频不声明、按路径读取。
- **取值路径（方案 A）**：从 `llm_model.config` 解包的类型化 `Vision bool` 沿既有模型下发链路透传，**只在解析处解包一次、下游传类型化值，不跨层传 `map[string]interface{}`**（符合 AGENTS.md 硬规则）。

## 数据流总览

消息附件链路：

```
消息附件(DB MessageAttachment)
  → RunRequest.Input.Attachments (messaging.Attachment → domain.Attachment)
  → preparer: multimodalAttachmentsForRuntime 按 MIME 过滤 + 按 URL 下载字节
      多模态字节 → agent.Attachment{MIME, Name, Data(≤100MiB 内联)}
      大文件 → Data 为空，落盘于 RepoDir/UploadRelDir
  → agent.ExecutionRequest.Attachments（携带 FilesystemContext.UploadRelDir）
  → cli.InvocationRequest.Attachments
  → opencode invoker buildMessageParts(prompt, UploadRelDir, attachments):
      生成 {type:"file", mime, filename: UploadRelDir/Name, url:"data:..."} part
  → POST /session/:sessionID/message
  → opencode image.normalize → AI SDK → 模型 image_url content block
```

视觉能力（`vision`）下发链路：

```
config.yaml LLMConfig.Vision / llm_model.config["vision"] (JSONB, default false)
  → seedLLM 写入 Config["vision"] / llm.VisionFromConfig 解包为 llm.ModelConfig.Vision
  → messaging.ModelOptions.Vision (NATS JSON 透传)
  → domain.ModelOptions.Vision (mapper.go 透传)
  → agent.ModelConfig.Vision (preparer 组装)
  → opencode buildConfigContent 决定是否写
      modalities:{input:["text","image"], output:["text"]}
  → 模型 content block 上的图片部分在 llmprotocol 代理侧
     encodeOpenAIChatMessages 中编码为 image_url 内容块
```

## 改动设计

### 1. runtime 契约层 —— `backend/agent/runtime.go`

新增多模态附件类型，并挂到 `ExecutionRequest`：

```go
// Attachment 是随一次执行携带的多模态文件（图片/PDF/音视频等）。
// Data 持有原始字节用于内联（如 base64 data URL）。runtime 决定如何附加。
// 仅用于多模态输入——纯文本附件仍应留在 prompt 中。非内联（大文件/空数据）时，
// 文件已落盘于 FilesystemContext.UploadRelDir，runtime 用其与 Name 拼接定位。
type Attachment struct {
    MIME string
    Name string // 原始文件名，非路径（如 "头像.jpeg"）
    Data []byte
}
```

`ExecutionRequest` 同样挂上落盘目录，供非内联附件按路径定位：

```go
type ExecutionRequest struct {
    ...
    Attachments []Attachment   // 新增
    Model       ModelConfig
    Filesystem  FilesystemContext
}

type FilesystemContext struct {
    ...
    UploadRelDir string // 附件落盘的 workspace 相对目录（如 "uploads"），空则未提供
}
```

设计要点：
- `Messages`/`Prompt` 保持 `string` 不变，不破坏现有 text-only 路径。
- 新增 `Attachments` 切片的含义是"额外多模态内容"，非多模态文件不放入。
- 文档早先版本中 `Attachment.Path` 字段已移除：非内联路径定位下沉到 `FilesystemContext.UploadRelDir` + `Name` 组合，由 runtime 自行拼接。

### 2. CLI 适配层 —— `agent/runtime/internal/cli/`

- `invocation.go`：`InvocationRequest` 增加 `Attachments []agent.Attachment`。
- `runner.go`：`RunInvocation` 中从 `request.Attachments` 拷贝到 `InvocationRequest.Attachments`（维持不可变传递）。

### 3. 运行态领域层 —— `agentrun/domain/request.go`

`Attachment` 增加进程内字节字段：

```go
type Attachment struct {
    ID       string
    Name     string
    MimeType string
    URL      string
    Data []byte `json:"-"`   // preparer 进程内回填，不序列化
}
```

### 4. opencode runtime —— `opencode/types.go` + `invoker.go`

- `messagePart` 增加 opencode `FilePartInput` 对应字段（对齐 schema 的 `{type,mime,filename,url}`）：

```go
type messagePart struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
    MIME     string `json:"mime,omitempty"`
    Filename string `json:"filename,omitempty"`
    URL      string `json:"url,omitempty"`
}
```

- 新增 `buildMessageParts`，把**携带内联数据（`len(Data)>0`）的多模态附件**转成 file part。**不做 MIME 前缀过滤**（过滤在 preparer 层完成）：凡是 `Data` 非空的附件一律注入；`Filename` 取 `UploadRelDir + "/" + Name`（与落盘路径一致带动 `uploads/` 前缀），`UploadRelDir` 为空时回退为仅 `Name`：

```go
func buildMessageParts(prompt string, uploadRelDir string, attachments []agent.Attachment) []messagePart {
    parts := []messagePart{{Type: "text", Text: prompt}}
    for _, att := range attachments {
        if len(att.Data) == 0 { // 大文件/空数据不内联，由上层以文本提示其路径
            continue
        }
        name := strings.TrimSpace(att.Name)
        relDir := strings.TrimSpace(uploadRelDir)
        filename := name
        if relDir != "" && name != "" {
            filename = relDir + "/" + name
        }
        mime := strings.TrimSpace(att.MIME)
        parts = append(parts, messagePart{
            Type:     "file",
            MIME:     mime,
            Filename: filename,
            URL:      "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(att.Data),
        })
    }
    return parts
}
```

- `sendAndProcessMessage` 改用 `buildMessageParts(req.Prompt, req.UploadRelDir, req.Attachments)`。

### 5. 组装层 —— `agentrun/preparer_impl.go`

新增 helper，从输入附件过滤出多模态文件（仅图片；PDF/音视频已退出多模态管线）并下载字节：

```go
func multimodalAttachmentsForRuntime(ctx, attachments) []agent.Attachment
func downloadAttachmentBytes(ctx, url) ([]byte, error)
// IsVisualMime 共享判定：仅 image/*
```

- 判定 `IsVisualMime(MimeType)` 通过且 `Name`、`URL` 均非空 → `GET` 下载字节填入 `agent.Attachment{MIME, Name, Data}`。
- 附件按原始文件名落盘于 `FilesystemContext.UploadRelDir`（`consts.RepoDirUploads`），与 `IngestAttachments` 落盘一致；非内联时由 runtime 用 `UploadRelDir + "/" + Name` 定位。
- **内联大小上限**：`maxMultimodalInlineBytes = 100MiB`。下载字节超限时**不填入 `Data`**（保持为空），由 runtime 侧按工作区路径读取，避免单条消息字节膨胀。
- 下载失败/超限**仅告警并跳过**（非致命，与现有 `IngestAttachments` 的 best-effort 语义一致）。
- 在组装 `ExecutionRequest` 时调用并填入 `Attachments`。
- 组装 `agent.ModelConfig` 时填入 `Vision: cloned.Model.Vision`（方案 A 链路终点）。

### 6. 视觉能力（Vision）—— 模型解析链

`vision` 从 `llm_model.config` 解包后沿既有模型下发链路透传，涉及：

| 文件 | 改动 |
|---|---|
| `backend/internal/llm/model.go:39-44` | `ModelConfig` 增加 `Vision bool` |
| `backend/internal/llm/manager_db.go` | 新增 `VisionFromConfig(config) bool` 解包 `Config["vision"]`（缺省 false），`modelConfigFromEntity` 填入 `Vision` |
| `backend/internal/seed/llm.go` | `seedLLM` 建默认模型时按 `llmCfg.Vision` 写 `Config["vision"]` |
| `backend/config/config.go` | `LLMConfig` 增加顶层 `Vision bool`（yaml `vision`），供 seed 使用 |
| `backend/pkg/messaging/command.go:388` | `ModelOptions` 增加 `Vision bool \`json:"vision,omitempty"\``（过 NATS） |
| `backend/internal/service/message_poster.go` | `resolveWorkerTaskModel` 填 `Vision: llm.VisionFromConfig(model.Config)` |
| `backend/internal/worker/command/run/mapper.go:57` | `domain.ModelOptions` 增加 `Vision` 并透传 |
| `backend/internal/worker/agentrun/domain/request.go:157` | `ModelOptions` 增加 `Vision bool` |
| `backend/agent/runtime.go:39-60` | `ModelConfig` 增加 `Vision bool`；新增 `Attachment`（`MIME`/`Name`/`Data`）；新增 `FilesystemContext.UploadRelDir` |
| `backend/internal/worker/agentrun/preparer_impl.go:371` | 填 `Vision: cloned.Model.Vision` |

解包逻辑仅此一处碰 map，下游传类型化值（`VisionFromConfig` 内联 `manager_db.go`）：

```go
func VisionFromConfig(config types.LLMModelConfig) bool {
    if len(config) == 0 {
        return false
    }
    v, ok := config["vision"].(bool)
    return ok && v
}
```

### 7. opencode config 能力声明 —— `opencode/`

- `opencode/types.go`：`modelConfig` 增加 `Modalities`（对齐 opencode config `modalities:{input:[...], output:[...]}`），`output` 缺省省略：

```go
type modalityConfig struct {
    Input  []string `json:"input"`
    Output []string `json:"output,omitempty"`
}
type modelConfig struct {
    ...
    Modalities *modalityConfig `json:"modalities,omitempty"`
}
```

- `opencode/config.go` `buildConfigContent`：当 `modelCfg.Vision` 时声明**仅模型真正支持的模态**（`input:[text,image]`，`output:[text]`）；`Vision==false`（含缺省）时**不写**，opencode 据此判 `input.* = false` 走优雅降级。未声明的模态（PDF/音视频）由 opencode 降级为提示文本，避免声明过宽导致 AI SDK 对不支持的 file part 返回硬错误：

```go
if modelCfg.Vision {
    modelEntry.Modalities = &modalityConfig{
        Input:  []string{"text", "image"},
        Output: []string{"text"},
    }
}
```

- `Vision=true` 仅声明图片输入；PDF/音视频不声明、也不由上游注入字节（见组装层，"仅图片内联"），故多模态链路实际只承载图片。`output` 影响模型输出能力声明，目前 opencode 源码**无运行时门控**（仅声明）。保留 `attachment:true`（利于 UI/上层展示）。

### 8. 代理层图片编码 —— `backend/pkg/llmprotocol/protocol_openai_chat.go`

`encodeOpenAIChatMessages` 新增对图片 IR 的支持，使模型回复中的多模态 content 能以 OpenAI chat 格式正确往返：

- 新增 `IRPartImage` 分支：遇到图片 part 时，将该 part 编码为 `{type:"image_url", image_url:{url}}`，并把本轮消息的 `content` 切换为**有序数组**（text part 与 image part 按原顺序拼接）。
- 纯文本（无图片）消息的 `content` 仍为字符串，保持既有行为与兼容性（有图片时 `content` 才升格为数组）。
- 与文档此前"上游 `modelrouter`/`llmprotocol` 代理层已原生支持图片 IR（`capability.go:14`、`ir.go`）"的结论衔接：图片 IR 的解码/声明早已支持，本次补齐了**编码**方向的实现。

## 边界与兜底

- **纯文本/非多模态附件**：不进多模态 `Attachments`，走 `BuildAttachmentText` 纯文本注入（`request.go:192-232`）。该函数现按 `IsVisualMime` 分流：
  - 视觉类（图片）→ 提示"视觉内容已随消息内联、可直接查看，**勿调用 read 工具**"（避免模型对图片 read 拿到 `Image read successfully` 后产生幻觉），不附 `Location`。
  - 非视觉类（文本/PDF/音视频）→ 提示 `Location: uploads/<name>` 按路径落盘读取，行为与原实现一致。
- **非多模态类型**（MIME 不是 image/\*）：`IsVisualMime` 判定为 false，不进 `Attachments`。
- **空/失败字节**：`buildMessageParts` 里 `len(att.Data)==0` 跳过；`multimodalAttachmentsForRuntime` 下载失败/超限跳过。
- **超大文件（>100MiB）**：不内联 `Data`（保持为空），runtime 按 `UploadRelDir + "/" + Name` 读取工作区路径；内联 base64 的图片若已超视觉阈值，由 opencode 端 `image.normalize` 自动 resize 缓解。
- **无视觉模型**：`vision=false` → 不写 `modalities` → `input.*=false` → opencode `unsupportedParts` 优雅降级（图→提示文本），不整轮失败。图片是否被模型消费由 opencode/local server 按模型 `modalities` 能力决定，不存在调用方错误门控。
- **声明视觉但上游仍拒收**：opencode 把上游 4xx 转为 session error 透传给用户，Leros 不吞错、不重试。
- **PDF/音视频统一走 read**：`preparer` 不再将 `application/pdf` / `audio/*` / `video/*` 注入多模态 `Attachments`，亦不下载其字节，一律经 `BuildAttachmentText` 提示按工作区路径读取；opencode 侧 `modalities.input` 仅声明 `image`，二者一致，无多余内联、无被丢弃的 file part。
- **判定已下沉**：`domain/request.go` 导出 `IsVisualMime`（仅 image/\*），`preparer_impl.go` 与其共享，不再有各写一份的重复判定。
- **其它 runtime**：claude/codex/native 不读取新增 `Attachments`/`Vision` 字段，行为不受影响。

## 兼容性与安全

- 所有信号均为**追加式**（新增字段/切片），向后兼容。
- `vision` 缺省即 `false`，旧数据行为不变（非视觉降级）。
- **无 DB Schema 变更**（仅用既有 JSONB 键），符合 AGENTS.md 迁移约束最小化。
- **不跨层传 `map[string]interface{}`**：`vision` 只在解析处解包为类型化 `bool`，后续传递均为强类型。
- 建议（本次未改动）后续修复 `IngestAttachments` 中 `att.Name` 直接拼接路径的潜在路径穿越问题，改用 `workspace.SafeJoin`。

## 验证方案

1. 单元测试
   - `opencode/invoker_test.go`：`buildMessageParts` 对所有带 `Data` 的附件（图/PDF/音视频）生成 file part、`Filename` 为 `uploadRelDir/Name`（`UploadRelDir` 为空时回退为仅 `Name`）、空 `Data` 跳过。
   - `agentrun/preparer_impl_test.go`：`multimodalAttachmentsForRuntime` 下载多模态、跳过非多模态 MIME/不可达/空 URL、超限不内联。
   - `agentrun/request_test.go`：`BuildAttachmentText` 对图片等视觉附件不附 `Location`/不提示 read、对文本附件保留 `Location` 与 read 提示；`TestBuildAttachmentText_ImageDoesNotPromptRead` 覆盖该分流。
   - `opencode/config_test.go`：`buildConfigContent` 在 `Vision=true` 时含 `modalities.input`（仅 image）且 `output` 非空、不含 pdf/audio/video，缺省/`false` 时不含 `modalities`。
   - `llmprotocol/protocol_openai_chat_test.go`：`TestOpenAIChatEncodeRequest_WithImage` 覆盖图片 round-trip、text+image 数组 content、纯文本仍为字符串。
   - `llm/manager_db` / `message_poster`：`VisionFromConfig` 解包（缺省 false、`true` 生效）。
   - `seed/llm_test.go`：`seedLLM` 按 `LLMConfig.Vision` 写入/不写 `Config["vision"]`。
2. 静态检查：`gofmt -s`、`go vet ./...`、`go build ./...`。
3. 回归：改动相关包 `opencode`、`internal/cli`、`agentrun`、`command/run`、`internal/service`、`internal/llm`、`internal/seed` 测试全绿；claude/codex/native 测试不受影响。
4. 端到端（手工）：配一个支持视觉的 OpenAI-compatible 模型（`vision=true` 或 `config.yaml` `llm.vision: true`），`leros chat` 上传图片，校验模型识别图片内容；再验证文本模型（缺省 `false`）发图时优雅降级、PDF/音视频附件分别按各自模型能力验证、纯文本附件行为不变。

## 后续

- 存储未来可扩展为 `config` 内的 `modalities` 数组（一次性支持 image/audio/pdf/video），读取处解包时同步扩展。
