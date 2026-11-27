# 移动端会话文件链接预览 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在移动端会话里点击 Markdown 文件链接，直接打开项目文件的只读预览（Markdown / 代码 / 文本 / 图片），PDF 等二进制下载后交给系统查看器。

**Architecture:** 移动端拦截 Markdown 链接并跳转独立预览页；文本走现有 `file.read`，二进制走新增的控制面流式下载接口（按 1 MiB 分块向节点读取）；节点侧 `file.read` 增加 `offset` 参数以支持分块。设计见 `docs/design/2026-11-27-mobile-file-link-preview-design.md`。

**Tech Stack:** Go（agentd / controlplane）、Expo React Native、`react-native-markdown-display`、`expo-file-system`、`expo-sharing`、vitest。

**Spec:** `docs/design/2026-11-27-mobile-file-link-preview-design.md`

> 计划文件放在 `docs/design/` 而不是 `docs/superpowers/plans/`：本仓库 `.gitignore` 忽略了 `docs/superpowers`，计划需要随分支提交。

## Global Constraints

- 只识别 Markdown 链接的 `href`；不识别裸路径、行内代码。
- 文本预览上限 512 KiB；节点单块读取上限 1 MiB；单帧协议上限 8 MiB。
- 单文件下载上限 100 MiB，超出返回 `file_too_large`。
- 新增前端依赖只有 `expo-sharing`（`~58.0.24`）；不引入 PDF 渲染引擎。
- 不执行 HTML；不写文件；不做文件编辑或目录浏览。
- Go 改动提交前 `gofmt -s`、`go vet`；前端提交前 `pnpm typecheck`。

## Review Focus

- 外部链接（`https://...`、`mailto:`）不能被当成项目文件打开。
- 子目录里的 Markdown 再次点相对链接时，必须按该文件所在目录解析，不能退回会话根目录。
- Windows 绝对路径（`C:\repo\x.md`）必须通过控制面的 `directory must be absolute` 校验。
- 大文件分块循环必须终止：不能重复追加、不能漏最后一块、不能越过 100 MiB。
- 文本被截断时必须显示提示，不能让前 512 KiB 看起来像完整文件。

---

### Task 1: 节点侧 `file.read` 支持偏移量

**Files:**
- Modify: `backend/internal/agentd/opencode/client.go`（`ReadFile`）
- Modify: `backend/internal/agentd/handler/handler.go`（`OpenCodeAPI`、`FileReadParams`、`readFile`）
- Test: `backend/internal/agentd/opencode/client_test.go`
- Test: `backend/internal/agentd/handler/handler_test.go`

**Interfaces:**
- Consumes: OpenCode `GET /api/fs/read/<path>` 的 HTTP Range 支持。
- Produces:
  - `opencode.Client.ReadFile(ctx context.Context, directory, path string, offset, maxBytes int64) (opencode.FileContent, error)`
  - `handler.FileReadParams{Directory, Path string; MaxBytes, Offset int64}`
  - `OpenCodeAPI.ReadFile` 同上签名。

- [ ] **Step 1: 写失败测试**

`client_test.go`：把 `TestReadFileUsesRangeAndReportsTruncation` 的调用改成 offset=2、maxBytes=4，并断言请求头 `Range: bytes=2-5`；新增一个用例：offset>0 但服务端返回 200 时，期望报错（防止服务端忽略 Range 时控制面重复拼接数据）：

```go
got, err := New(srv.URL, srv.Client()).ReadFile(context.Background(), "/work", "a.txt", 2, 4)
// 断言 Range == "bytes=2-5"
```

`handler_test.go`：`fakeOpenCode.ReadFile` 记录 `readFileOffset`；把 `TestFileReadReturnsUTF8AndCapsSize` 的期望改为 `1 << 20`；新增 `TestFileReadPassesOffset`，传 `Offset: 4` 后断言 `oc.readFileOffset == 4`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/internal/agentd/opencode ./backend/internal/agentd/handler -run 'TestReadFile|TestFileRead'`
Expected: FAIL/编译失败（`ReadFile` 参数数量不匹配）。

- [ ] **Step 3: 实现**

`opencode.ReadFile`：

```go
func (c *Client) ReadFile(ctx context.Context, directory, path string, offset, maxBytes int64) (FileContent, error)
```

- `offset < 0` 归零；`maxBytes <= 0` 用默认 512 KiB。
- `Range: bytes=<offset>-<offset+maxBytes-1>`。
- `offset > 0` 且响应 `200 OK` 时返回 `errors.New("opencode read file: server ignored range request")`。
- 其余逻辑不变。

`handler.go`：

- `FileReadParams` 增加 `Offset int64 \`json:"offset,omitempty"\``。
- `OpenCodeAPI.ReadFile` 与 `fakeOpenCode` 同步签名。
- `readFile`：`Offset < 0` 返回错误；单块上限从 `512 << 10` 改为 `1 << 20`；调用 `h.opencode.ReadFile(ctx, root, filepath.ToSlash(rel), p.Offset, maxBytes)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./backend/internal/agentd/opencode ./backend/internal/agentd/handler`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
gofmt -s -w backend/internal/agentd/opencode/client.go backend/internal/agentd/handler/handler.go
git add backend/internal/agentd/opencode backend/internal/agentd/handler
git commit -m "feat(agentd): file.read 支持偏移量读取"
```

---

### Task 2: 控制面流式下载接口

**Files:**
- Modify: `backend/internal/controlplane/api/files.go`
- Modify: `backend/internal/controlplane/api/api.go:173-174`（新增路由）
- Test: `backend/internal/controlplane/api/api_test.go`

**Interfaces:**
- Consumes: Task 1 的 `file.read` 方法（`offset` / `max_bytes`）。
- Produces: `GET /api/nodes/{id}/files/download?directory=&path=`，返回 `application/octet-stream` 原始字节，`Content-Length` 为文件总大小（未知时不设置）。

- [ ] **Step 1: 写失败测试**

在 `api_test.go` 添加：

```go
func TestAPIFileDownloadStreamsChunks(t *testing.T)
```

- 节点两次响应 `file.read`：
  - 第一次 `{"content":"hello","encoding":"utf-8","size":10,"truncated":true}`，断言 `params.Offset == 0`、`params.MaxBytes == 1<<20`；
  - 第二次 `{"content":"world","encoding":"utf-8","size":10,"truncated":false}`，断言 `params.Offset == 5`。
- 断言 HTTP 200、body `helloworld`、`Content-Length: 10`、`Content-Type: application/octet-stream`。

```go
func TestAPIFileDownloadRejectsTooLarge(t *testing.T)
```

- 节点返回 `{"content":"","encoding":"utf-8","size":104857601,"truncated":true}`。
- 断言 413 且错误码 `file_too_large`。

```go
func TestAPIFileContentAcceptsWindowsDirectory(t *testing.T)
```

- `directory=C:\repo`（URL 编码），节点返回普通 `file.read` 结果。
- 断言 200 且 `params.Directory == "C:\\repo"`。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./backend/internal/controlplane/api -run 'TestAPIFile'`
Expected: FAIL（下载路由 404；Windows 目录 400）。

- [ ] **Step 3: 实现**

`files.go`：

```go
const (
    maxFileReadBytes       int64 = 512 << 10
    fileDownloadChunkBytes int64 = 1 << 20
    maxFileDownloadBytes   int64 = 100 << 20
)
```

- `fileReadParams` 增加 `Offset int64 \`json:"offset,omitempty"\``。
- 新增 `handleFileDownload`：
  - 复用 `fileQuery`、`requireNode`；`path` 为空返回 400。
  - 循环调用 `file.read`，`Offset` 从 0 开始，每次 `MaxBytes = fileDownloadChunkBytes`。
  - 解析 `{content, encoding, size, truncated}`；`base64` 用 `base64.StdEncoding` 解码，其余直接 `[]byte(content)`。
  - 第一块：`size > maxFileDownloadBytes` → `413 file_too_large`，不写响应头；否则设置 `Content-Type: application/octet-stream`、`size > 0` 时设置 `Content-Length`，`WriteHeader(200)`。
  - 逐块 `w.Write`，每次写完调用 `http.Flusher.Flush()`（可用时）。
  - 循环结束条件：`!truncated`、`len(chunk)==0`、`offset >= size`（size > 0 时）任一成立。
  - 第一块失败仍用 `writeAgentError`；已开始写响应后的失败只记 `logs.Warnf` 并返回（不能再写 JSON）。
- `fileQuery` 的绝对路径校验改为同时接受 POSIX 与 Windows：

```go
func absoluteDirectory(p string) bool {
    return filepath.IsAbs(p) || windowsAbsPath.MatchString(p)
}
// windowsAbsPath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
```

- `api.go` 注册 `GET /api/nodes/{id}/files/download`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./backend/internal/controlplane/api`
Expected: PASS（含既有 `TestAPIFileContent`）。

- [ ] **Step 5: 提交**

```bash
gofmt -s -w backend/internal/controlplane/api/files.go backend/internal/controlplane/api/api.go backend/internal/controlplane/api/api_test.go
go vet ./backend/internal/controlplane/api ./backend/internal/agentd/...
git add backend/internal/controlplane/api backend/internal/agentd
git commit -m "feat(controlplane): 新增节点文件流式下载接口"
```

---

### Task 3: 链接解析与预览路由

**Files:**
- Create: `frontend/apps/mobile/src/session/file-links.ts`
- Create: `frontend/apps/mobile/src/session/file-links.test.ts`
- Modify: `frontend/apps/mobile/src/config.ts`

**Interfaces:**
- Produces:
  - `interface FileLinkTarget { directory: string; path: string }`
  - `resolveFileLink(href: string, sessionDirectory: string): FileLinkTarget | null`
  - `fileBaseDirectory(directory: string, path: string): string`（子目录 Markdown 内相对链接的基准目录）
  - `fileRoute(nodeID: string, directory: string, path: string): string`

- [ ] **Step 1: 写失败测试**

`file-links.test.ts`：

```ts
resolveFileLink("docs/plan.md", "/repo") === { directory: "/repo", path: "docs/plan.md" }
resolveFileLink("./docs/My%20Plan.md?plain=1#L10", "/repo") === { directory: "/repo", path: "docs/My Plan.md" }
resolveFileLink("/repo/docs/plan.md", "/repo") === { directory: "/repo/docs/", path: "plan.md" }
resolveFileLink("file:///repo/docs/plan.md", "/repo") === { directory: "/repo/docs/", path: "plan.md" }
resolveFileLink("C:\\repo\\docs\\plan.md", "/repo") === { directory: "C:\\repo\\docs\\", path: "plan.md" }
resolveFileLink("https://example.com/plan.md", "/repo") === null
resolveFileLink("mailto:a@b.c", "/repo") === null
resolveFileLink("#section", "/repo") === null
resolveFileLink("docs/plan.md", "") === null
```

`fileBaseDirectory("/repo", "docs/plan.md") === "/repo/docs/"`；
`fileBaseDirectory("C:\\repo", "docs\\plan.md") === "C:\\repo\\docs\\"`。

- [ ] **Step 2: 跑测试确认失败**

Run: `pnpm --filter @lework/mobile test -- file-links`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现**

- 绝对路径先判断（`/`、`X:\`、`X:/`、`\\`），避免 Windows 盘符被当成 URL scheme。
- `file:` 前缀去掉；`file:///C:/x` 去掉多余前导 `/`。
- 去掉 `#fragment`、`?query`，再 `decodeURIComponent`（失败时保留原文）。
- 绝对路径：`directory` 为最后一个 `/` 或 `\` 之前的全部（含分隔符），`path` 为文件名。
- 相对路径：`directory` 用会话目录；会话目录为空返回 `null`。
- `fileBaseDirectory`：路径里有子目录时把该子目录拼到 `directory` 后面，分隔符跟随 `directory`；无子目录时原样返回。
- `fileRoute`：`/file/<encoded node>?directory=<encoded>&path=<encoded>`。

- [ ] **Step 4: 跑测试确认通过**

Run: `pnpm --filter @lework/mobile test -- file-links`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/src/session/file-links.ts frontend/apps/mobile/src/session/file-links.test.ts frontend/apps/mobile/src/config.ts
git commit -m "feat(mobile): 识别会话里的项目文件链接"
```

---

### Task 4: 下载与系统查看器适配

**Files:**
- Modify: `frontend/apps/mobile/package.json`
- Create: `frontend/apps/mobile/src/files/download.ts`
- Create: `frontend/apps/mobile/src/files/download.test.ts`
- Create: `frontend/apps/mobile/src/files/viewer.ts`
- Create: `frontend/apps/mobile/src/files/viewer.test.ts`

**Interfaces:**
- Produces:
  - `fileDownloadUrl(settings: Settings, nodeID: string, directory: string, path: string): string`
  - `previewCachePath(cacheDirectory: string, nodeID: string, path: string): string`
  - `downloadFile(options: { url: string; headers: Record<string, string>; destination: string; onProgress?: (percent: number) => void }): Promise<string>`
  - `type PreviewKind = "markdown" | "text" | "image" | "binary" | "auto"`
  - `previewKind(path: string): PreviewKind`
  - `mimeTypeFor(path: string): string | undefined`
  - `openWithSystemApp(uri: string, path: string): Promise<void>`
  - `truncationNotice(truncated: boolean): string | null`（被截断时返回“文件较大，仅显示前 512 KiB”）

- [ ] **Step 1: 写失败测试**

`download.test.ts`：

```ts
fileDownloadUrl(settings, "node 1", "/repo/docs", "a b.md")
  === "https://cp.example/api/nodes/node%201/files/download?directory=%2Frepo%2Fdocs&path=a%20b.md"
previewCachePath("/cache/", "n1", "docs/report.pdf") 以 "lework-preview-" 开头且以 "report.pdf" 结尾
previewCachePath("/cache/", "n1", "a/report.pdf") !== previewCachePath("/cache/", "n1", "b/report.pdf")
```

`viewer.test.ts`：

```ts
previewKind("plan.md") === "markdown"
previewKind("main.go") === "auto"
previewKind("photo.PNG") === "image"
previewKind("report.pdf") === "binary"
previewKind("Dockerfile") === "auto"
mimeTypeFor("report.pdf") === "application/pdf"
mimeTypeFor("a.unknown") === undefined
truncationNotice(true) === "文件较大，仅显示前 512 KiB"
truncationNotice(false) === null
```

- [ ] **Step 2: 跑测试确认失败**

Run: `pnpm --filter @lework/mobile test -- files/`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 安装依赖并实现**

```bash
cd frontend && pnpm --filter @lework/mobile add expo-sharing@~58.0.24
```

- `download.ts` 运行时动态 `import("expo-file-system/legacy")`，避免 vitest 在 node 环境加载原生模块；先删除同名旧文件；`createDownloadResumable(url, destination, { headers }, callback)`；非 2xx 或缺少 `uri` 抛 `下载文件失败：HTTP <status>`。
- `previewCachePath`：`cacheDirectory` 补尾部分隔符；文件名用路径 basename；`nodeID + path` 做 FNV-1a 十六进制指纹防同名覆盖。
- `viewer.ts`：`previewKind` 只把 `.md/.markdown/.mdx` 判为 markdown、图片扩展名判为 image、PDF/Office/压缩包等判为 binary，其余 `auto`；`mimeTypeFor` 覆盖常见扩展名；`openWithSystemApp` 动态 `import("expo-sharing")`，不可用时抛“当前设备无法调用系统应用打开文件”。

- [ ] **Step 4: 跑测试确认通过**

Run: `pnpm --filter @lework/mobile test -- files/`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/package.json frontend/pnpm-lock.yaml frontend/apps/mobile/src/files
git commit -m "feat(mobile): 增加文件下载与系统查看器适配"
```

---

### Task 5: 预览页与链接拦截接线

**Files:**
- Create: `frontend/apps/mobile/app/file/[node].tsx`
- Modify: `frontend/apps/mobile/app/_layout.tsx`
- Modify: `frontend/apps/mobile/src/session/markdown.tsx`
- Modify: `frontend/apps/mobile/src/session/message-row.tsx`
- Modify: `frontend/apps/mobile/src/ui/tool-call/tool-call-card.tsx`
- Modify: `frontend/apps/mobile/src/ui/tool-call/subagent-sheet.tsx`
- Modify: `frontend/apps/mobile/app/session/[node]/[id].tsx`

**Interfaces:**
- Consumes: Task 3 的 `resolveFileLink` / `fileBaseDirectory` / `fileRoute`；Task 4 的下载与查看器函数；`@lework/api` 的 `readFile`。
- Produces: `MarkdownBody` 新增可选 `nodeID?: string`、`directory?: string`；`MessageRow` / `ToolCallCard` / `SubagentSheet` 新增 `directory: string`。

- [ ] **Step 1: 预览页**

`app/file/[node].tsx`：

- `useLocalSearchParams<{ node; directory; path }>()`；空参数显示错误。
- 加载前按 `previewKind(path)` 分流：
  - `markdown` / `text` / `auto`：`client.readFile(node, directory, path)`；`encoding === "base64"` 转二进制下载，否则按文本 / Markdown 渲染。
  - `image` / `binary`：直接下载。
- 下载用 `previewCachePath(cacheDirectory, node, path)`，headers 带 `Authorization: Bearer <sessionToken>`；下载中显示百分比。
- 渲染：
  - Markdown：`<MarkdownBody content nodeID={node} directory={fileBaseDirectory(directory, path)} scheme={theme.scheme} />`。
  - 文本：`Text` 等宽、`selectable`，放在 `ScrollView` 里。
  - 图片：`<Image source={{ uri }} resizeMode="contain" style={{ width: "100%", height: 360 }} />`。
  - 二进制：文件名、大小、`用系统应用打开` 按钮，点击 `openWithSystemApp(uri, path)`。
  - `truncated`：显示 `truncationNotice(truncated)`，并提供“打开完整文件”（下载后 `openWithSystemApp`）。
  - 错误：显示 `ApiError.message`。
- 顶部显示 `path` 与格式化后的大小；`useScreenTitle(basename)`。

- [ ] **Step 2: 路由与接线**

- `_layout.tsx` 注册 `<Stack.Screen name="file/[node]" options={{ title: "文件预览" }} />`。
- `MarkdownBody`：新增 `nodeID` / `directory`；给 `<Markdown>` 传 `onLinkPress`：

```ts
const target = resolveFileLink(href, directory ?? "");
if (!target || !nodeID) return true; // 交给默认系统浏览器
router.push(fileRoute(nodeID, target.directory, target.path));
return false;
```

- `MessageRow` 新增 `directory: string`，传给 `MarkdownBody` 与 `ToolCallCard`。
- `ToolCallCard` → `SubagentSheet` → `MessageRow` 透传 `directory`。
- 会话页计算 `const directory = current?.directory || (project ? projectDirectoryOnNode(project, nodeID) : "");` 并传给 `MessageRow`。

- [ ] **Step 3: 类型检查**

Run: `cd frontend && pnpm --filter @lework/mobile typecheck`
Expected: PASS，无新增错误。

- [ ] **Step 4: 跑移动端单测**

Run: `pnpm --filter @lework/mobile test`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/app frontend/apps/mobile/src
git commit -m "feat(mobile): 会话内点击文件链接打开预览"
```

---

### Task 6: 整体验证与收尾

**Files:** 无新增，修复验证中发现的问题。

- [ ] **Step 1: Go 全量验证**

```bash
gofmt -l backend
go vet ./backend/...
go test -p 1 ./backend/internal/agentd/... ./backend/internal/controlplane/...
```

Expected: `gofmt -l` 无输出；测试 PASS。

- [ ] **Step 2: 前端验证**

```bash
cd frontend && pnpm typecheck
pnpm --filter @lework/mobile test
```

Expected: PASS。

- [ ] **Step 3: 自查**

- 外链、`mailto:`、`#anchor` 不被拦截。
- 子目录 Markdown 内相对链接按该文件目录解析。
- 分块下载在 `truncated=false` 时停止，body 长度等于 `Content-Length`。
- 文本截断提示可见。
- 未提交设计文档以外的忽略文件（`git status`）。

- [ ] **Step 4: 提交修复（如有）**

```bash
git add -p
git commit -m "test: 补齐文件预览验证"
```
