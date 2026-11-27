# 移动端会话文件链接预览设计

## 背景与目标

助手回复里经常给出开发计划、设计文档、代码文件的路径（例如
`[计划](docs/plans/2026-11-27-x.md)`）。在手机上这些链接点不开，用户只能
复制路径回电脑查看。

目标：在移动端会话里点击指向项目文件的链接，直接在 App 内打开预览。支持的
内容类型：

- Markdown / 纯文本 / 代码：应用内直接阅读。
- 图片（png/jpg/gif/webp 等）：应用内直接显示。
- PDF、Word、Excel、PPT、压缩包等二进制：下载后交给系统查看器打开。
- 无法识别的文件：先按文本尝试；二进制按上一行处理。

非目标：

- 文件编辑、目录浏览、文件树。
- HTML 脚本执行（只按源码显示）。
- 正文里裸路径、行内代码路径的识别（只处理 Markdown 链接）。
- 桌面端与 Web 控制面。

## 现状

- 后端已有 `GET /api/nodes/{id}/files/content?directory=&path=`：
  - `directory` 必须是绝对路径，且位于节点 `workspace_roots` 内（已配置时）；
  - `path` 被限制在 `directory` 内，符号链接也会校验；
  - 单次最多 512 KiB，返回 `utf-8` 文本或 `base64`，带 `size` / `truncated`。
- 节点侧 OpenCode 的读文件接口原生支持 HTTP Range，但 agentd 固定从 0 开始读，
  没有暴露偏移量。
- agent ↔ 控制面单帧上限 8 MiB（`protocol.MaxFrameBytes`），所以大文件不能
  一次读完，只能分块。
- 移动端 `@lework/api` 已封装 `readFile`，但没有页面使用。
- 助手回复由 `react-native-markdown-display` 渲染，其 `onLinkPress(url)` 回调
  返回 `true` 时走系统浏览器，返回 `false` 时由调用方自行处理。
- 会话页能拿到 `nodeID` / `sessionID`；会话目录来自会话列表的
  `current.directory`，项目目录可作兜底。

## 方案

### 链接识别

只看 Markdown 链接的 `href`：

| 输入 | 处理 |
| --- | --- |
| `http:` / `https:` / `mailto:` / 其它 scheme | 保持现状，交给系统浏览器 |
| `file:///repo/docs/x.md` | 去掉 `file://` 后按绝对路径处理 |
| `/repo/docs/x.md`、`C:\repo\docs\x.md` | `directory` = 文件所在目录，`path` = 文件名 |
| `docs/x.md`、`./docs/x.md` | `directory` = 当前会话目录（项目目录兜底） |
| `docs/x.md#L10`、`docs/x.md?plain=1` | 去掉 `#fragment` 与 `?query` |
| `docs/My%20Plan.md` | 做百分号解码 |
| 相对路径且拿不到会话目录 | 不拦截，按外部链接处理 |

`../` 等越界路径不在这里拦截，交给节点侧的 workspace 校验，错误原样展示。

### 文件打开方式

| 类型 | 方式 |
| --- | --- |
| `.md` / `.markdown` / `.mdx` | 现有 `MarkdownBody` 渲染，页内链接可继续点 |
| 其它 UTF-8 文本 / 代码 | 等宽、可选中的 `Text` |
| 图片扩展名 | 下载到缓存，应用内 `<Image>` 显示 |
| PDF / Office / 压缩包等二进制 | 下载到缓存，`expo-sharing` 交给系统应用打开 |
| 未知扩展名 | 先 `file.read` 判断编码：utf-8 按文本，base64 按二进制 |
| 文本超过 512 KiB | 显示前 512 KiB 并提示截断，可下载完整文件 |

新增 `expo-sharing`（Expo 官方模块，版本与 SDK 58 对齐），用于把下载好的
PDF / Office 文件交给系统查看器。图片仍用 React Native 内置 `Image`。

### 大文件下载

新增控制面接口 `GET /api/nodes/{id}/files/download?directory=&path=`，把节点
文件以原始字节流返回给浏览器/App：

1. 控制面按 1 MiB 分块调用 agent 的 `file.read`，每次都带 `offset`；
2. 第一块拿到总大小后设置 `Content-Length`，移动端可显示下载进度；
3. 边收边写 HTTP 响应，不在服务端拼出整个文件；
4. 节点错误在开始写响应前仍按 JSON 错误返回；响应开始后再失败只能中断连接。

节点侧改动：`file.read` 增加 `offset` 参数，OpenCode 客户端把
`Range: bytes=<offset>-<offset+max-1>` 发出去；单块上限从 512 KiB 提升到
1 MiB（文本最坏 JSON 转义约 6 MiB，仍低于 8 MiB 帧上限）。

单文件下载上限 100 MiB，超出返回 `file_too_large`，避免误点日志文件把手机
流量和磁盘打满。

移动端下载走 `expo-file-system` 的 `DownloadResumable`，带
`Authorization` 头，落在缓存目录；同名文件覆盖，不做额外清理。

### 预览页

- 新路由 `app/file/[node].tsx`，`node` 在路径段，`directory` / `path` 走 query。
- 顶部显示完整路径与大小；页面标题固定“文件预览”，侧边栏标题用文件名。
- 状态：加载 / 错误 / 文本 / Markdown / 图片 / 二进制文件卡片。
- 二进制卡片显示文件名、大小、类型，以及“用系统应用打开”按钮。
- 下载中显示百分比；失败展示可读错误。
- 不缓存文件内容到 State 之外的持久存储（下载文件放缓存目录，交给系统清理）。

## 接线

| 文件 | 改动 |
| --- | --- |
| `src/session/file-links.ts` | 新增 `resolveFileLink(href, directory)` 纯函数 |
| `src/config.ts` | 新增 `fileRoute(node, directory, path)` |
| `src/session/markdown.tsx` | `MarkdownBody` 新增可选 `nodeID` / `directory`，用 `onLinkPress` 拦截 |
| `src/session/message-row.tsx` | 新增 `directory`，传给正文与工具卡片 |
| `src/ui/tool-call/tool-call-card.tsx`、`subagent-sheet.tsx` | 透传 `directory`，子代理消息同样可点 |
| `app/session/[node]/[id].tsx` | 传 `current.directory`，缺省用 `projectDirectoryOnNode` |
| `app/file/[node].tsx` | 新增预览页 |
| `app/_layout.tsx` | 注册预览路由 |
| `src/files/download.ts` | 下载 URL、缓存路径、带鉴权下载与进度 |
| `src/files/viewer.ts` | 文件类型判断、MIME、系统打开 |
| `backend/internal/agentd/handler/handler.go` | `file.read` 支持 `offset`，提高单块上限 |
| `backend/internal/agentd/opencode/client.go` | `ReadFile` 支持偏移量 |
| `backend/internal/controlplane/api/files.go` | 新增流式下载接口 |
| `backend/internal/controlplane/api/api.go` | 注册下载路由 |

## 安全与边界

- 预览只读，不写文件、不改内容。
- 路径校验、symlink 校验、workspace 限制仍由节点 agent 负责，移动端不复制一套。
- HTML 只显示源码，不在 WebView 里执行。
- 下载接口与现有文件接口共用同一套鉴权与节点归属校验。
- 节点离线、文件不存在、目录越界、超过 100 MiB 都展示明确错误。

## 验证

- Go：
  - `file.read` 偏移量参数透传到 OpenCode Range；
  - 流式下载接口按分块拼出完整内容、设置 `Content-Length`、超限返回 413；
  - 既有文件接口行为不变。
- TypeScript 单测：
  - `resolveFileLink`：相对路径、绝对路径、`file://`、Windows 盘符、
    `#fragment` / `?query`、百分号编码、外链、无会话目录；
  - 下载 URL / 缓存路径拼接、文件类型判断。
- `pnpm --filter @lework/mobile typecheck`、`pnpm --filter @lework/mobile test`。
- 手工：点 Markdown / 代码 / 图片 / PDF / 未知文本 / 外链；离线节点；不存在
  文件；大文件截断；超限文件。

## 已按判断先定的点

- 只按 Markdown 链接识别。若实际输出是裸路径或行内代码，需要另做一轮。
- 预览用独立全屏页，不做底部弹出层。
- PDF / Office 等本次交给系统查看器；App 内嵌 PDF 渲染留作后续独立需求。
