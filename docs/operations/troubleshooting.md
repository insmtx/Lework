# Lework 故障排除指南

## 常见问题

### 问题: "Failed to initialize database: unsupported database driver: "

**原因**: 数据库配置中的driver字段为空或未配置。

**解决方案**:
1. 如果你不需要数据库功能，可以忽略此警告 - 系统会继续运行，只是数据库功能不可用。
2. 如果你需要数据库，请确保配置文件中有完整的数据库配置：

```yaml
database:
  driver: postgres
  url: "host=localhost user=lework password=lework dbname=lework port=5432 sslmode=disable"
  debug: false
```

**注意**: GitHub认证功能可以在没有数据库的情况下运行，只是不会持久化用户数据。

### 问题: 上传图片提问后返回「抱歉，本次未生成有效回复」

**现象**: 用户上传图片提问后接口返回兜底提示「抱歉，本次未生成有效回复…」，但模型实际上已生成了完整、正确的回复。

**原因**: 最终 assistant 文本 `lastTextEnded` 原本仅靠 SSE 事件 `message.part.updated`(text) 回填，而该事件与 `SendMessage` 同步返回（`msgDone`）之间存在竞态——`msgDone` 触发后 `cancelSSE()` 会丢弃尚未消费的 assistant text part 事件，导致 `lastTextEnded` 停留在用户输入上被误判为「回声」。叠加次要问题：text part 事件未区分 user/assistant，user 文本也会写入 `lastTextEnded`。

**解决方案**:
1. 主修复：`SendMessage` 同步返回后，直接从响应体 `msgResp.Parts` 提取 assistant 文本回填 `lastTextEnded`（新增 `assistantTextFromParts`），不再依赖可能被丢弃的 SSE 事件。
2. 辅助修复：SSE 事件中仅 assistant text part（匹配 `st.messageID`）更新 `lastTextEnded`，user 文本不再覆盖。

**相关文件**: `backend/agent/runtime/opencode/invoker.go`、`events.go`

### 问题: 大图（高分辨率/大字节）导致回复丢失或回声

**现象**: 上传分辨率较大或字节数较大的图片时，模型可能不输出文本，或同样表现为「回声/未生成有效回复」。

**原因**: opencode 对超过阈值的图片会触发内部重编码（`Image.normalize`）：像素最长边超过 `MAX_WIDTH/MAX_HEIGHT=2000`、或 base64 超过 `MAX_BASE64_BYTES=5MiB` 时，会改用 photon 重编码，此路径下生成的文本不通过 SSE 流式上报，导致 worker 拿不到 assistant 文本。仅靠一个像素阈值无法覆盖「像素小但字节大」的图片。

**解决方案**: 在 preparer 层预先归一化，避免图片落入 opencode 的重编码路径（`backend/internal/worker/agentrun/preparer_impl.go`）：
1. 像素维度：最长边等比缩放到 `maxMultimodalSide=1600` 以内。
2. 字节维度：base64 超过 `maxMultimodalBase64Bytes=5MiB` 时，按 JPEG 质量阶梯（85/70/55/40）逐步降质重编码到阈值内。

**相关文件**: `backend/internal/worker/agentrun/preparer_impl.go`

## 运行模式

### 最小模式 (无需数据库)
使用 `minimal-config.yaml` 可以在没有PostgreSQL的情况下启动服务：

```bash
./lework --config minimal-config.yaml
```

### 完整模式 (使用数据库)
使用完整配置启动，包括数据库：

```bash
./lework --config example-config.yaml
```

## 启动检查清单

- [ ] 配置文件路径正确
- [ ] 如果使用数据库，PostgreSQL已启动且连接信息正确
- [ ] 如果使用NATS，NATS已启动
- [ ] GitHub配置正确（如果使用GitHub功能）
- [ ] 所有端口可用 (默认8080)