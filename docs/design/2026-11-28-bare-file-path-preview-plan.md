# 裸文件路径预览 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 助手回复里的裸文件路径（`docs/x.md`）在移动端点一下就能打开现有文件预览页。

**Architecture:** 纯函数 `findFilePathSpans` 在文本里找出路径片段；`withFilePathLinks` 把 Markdown 的 `text` / `code_inline` 规则产出的文本拆成「普通文本 + 可点内层 Text」，点击复用 `fileRoute` 打开预览。代码块、链接标签、用户消息不参与。

**Tech Stack:** Expo React Native、`react-native-markdown-display`、vitest。

**Spec:** `docs/design/2026-11-28-bare-file-path-preview-design.md`

> 计划文件放 `docs/design/`：本仓库 `.gitignore` 忽略了 `docs/superpowers`。

## Global Constraints

- 只识别含 `/` 或 `\` 且最后一段带扩展名（1–10 位字母数字）的片段；`README.md` 这类无斜杠文件名不识别。
- 代码块（fence / code_block）、链接标签、用户消息、思考过程、工具输出不识别。
- 识别规则必须排除 `http(s)://`、`//host/x.md`、首段像域名的 token（`example.com/x.md`）。
- 路径片段两侧的标点（`（`、`，`、`。`、`)`、行尾 `.`）不属于路径。
- 相对路径按会话目录解析，复用 `resolveFileLink` 与 `fileRoute`；不新增后端接口。
- 含路径的文本节点不得加 `selectable`（会吞掉 onPress）。

## Review Focus

- URL 恰好以文件扩展名结尾（`https://example.com/a.md`）时不能被当成项目文件。
- 中文标点紧贴路径（「写好了：docs/x.md」「（docs/x.md）」）时只链接路径本身。
- 行内代码可点，围栏代码块不可点。
- `[docs/x.md](https://example.com)` 的显示文字仍是外链，不抢跳转。
- 句尾 `.` `)` `。` 被排除在路径之外。

---

### Task 1: 路径片段识别

**Files:**
- Create: `frontend/apps/mobile/src/session/file-paths.ts`
- Test: `frontend/apps/mobile/src/session/file-paths.test.ts`

**Interfaces:**
- Produces:
  - `interface FilePathSpan { start: number; end: number; path: string }`
  - `findFilePathSpans(content: string): FilePathSpan[]`
  - `start` / `end` 是 `content` 的 UTF-16 下标；`path === content.slice(start, end)`。

- [ ] **Step 1: 写失败测试**

`file-paths.test.ts` 至少覆盖：

```ts
findFilePathSpans("设计文档写好了：docs/superpowers/specs/2026-11-28-a-design.md")
// 恰好一个 span，path 为 docs/...-design.md，不含「设计文档写好了：」
findFilePathSpans("见 docs/a.md 和 src/b.go")
// 两个 span，path 分别为 docs/a.md、src/b.go
findFilePathSpans("（docs/a.md），另见 ./b.md。")
// path 为 docs/a.md、./b.md；不含括号和句号
findFilePathSpans("见 docs/a.md.")
// path 为 docs/a.md（行尾句号被去掉）
findFilePathSpans("/repo/docs/a.md")
findFilePathSpans("../docs/a.md")
findFilePathSpans("C:\\repo\\docs\\a.md")
findFilePathSpans("\\\\box\\share\\a.md")
findFilePathSpans("docs/My%20Plan.md")
// 以上各自返回一个 span
findFilePathSpans("https://example.com/a.md")        // []
findFilePathSpans("//cdn.example.com/a.png")         // []
findFilePathSpans("example.com/a.md")                // []
findFilePathSpans("见 and/or 与 24/7")                // []
findFilePathSpans("README.md")                       // []
findFilePathSpans("docs/README")                     // []
findFilePathSpans("1.2.3/README.md")                 // []
```

- [ ] **Step 2: 跑测试确认失败**

Run: `pnpm --filter @lework/mobile test -- src/session/file-paths`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现 `findFilePathSpans`**

扫描方式：

- 用 `[A-Za-z0-9._~%+\-/\\:]+` 找出连续片段（中文字符不在其中，天然成为边界）。
- 片段内按 `:` 切分，但 `^[A-Za-z]:[\\/]` 的盘符冒号不切；每个子片段沿用原始下标。
- 子片段起始字符前若是字母/数字则丢弃（避免把「见C:\repo\x.md」误切成 `\repo\x.md`）。
- 去掉末尾连续 `.`。
- 校验：含 `/` 或 `\`；不以 `//` 开头；不以 `~` 或单个 `\` 开头；
  非绝对路径（`/`、`\\`、盘符）且首段含 `.` 且不以 `.` 开头则视为域名丢弃；
  最后一段匹配 `\.[A-Za-z0-9]{1,10}$`。

- [ ] **Step 4: 跑测试确认通过**

Run: `pnpm --filter @lework/mobile test -- src/session/file-paths`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/src/session/file-paths.ts frontend/apps/mobile/src/session/file-paths.test.ts
git commit -m "feat(mobile): 识别回复里的裸文件路径"
```

---

### Task 2: Markdown 文本规则与接线

**Files:**
- Create: `frontend/apps/mobile/src/session/path-link-rules.tsx`
- Test: `frontend/apps/mobile/src/session/path-link-rules.test.ts`
- Modify: `frontend/apps/mobile/src/session/markdown.tsx`

**Interfaces:**
- Consumes: Task 1 的 `findFilePathSpans`。
- Produces:
  - `withFilePathLinks(rules: RenderRules, resolve: (token: string) => string | null, open: (route: string) => void): RenderRules`：只替换 `text` 与 `code_inline`，其余规则原样返回。
  - `MarkdownBody` 不新增 props。

- [ ] **Step 1: 写失败测试**

`path-link-rules.test.ts`（参考 `selectable-rules.test.ts`，用 `vi.mock("react-native", () => ({ Text: "Text" }))` 与 `createElement`）：

```ts
// 1. text 节点命中路径：返回的 Text 子节点里有一个Text，其 props.onPress 触发 open(route)
//    resolve 返回 "/file/n1?...", open 收到同一 route。
// 2. 节点在 link 父节点内（parent 含 { type: "link" }）：输出与原始规则一致，open 不被调用。
// 3. 节点内容不含路径：输出与原始规则一致。
// 4. rules.fence 与 rules.code_block 保持原函数引用（代码块不包装）。
// 5. code_inline 节点命中路径：同样可点。
```

- [ ] **Step 2: 跑测试确认失败**

Run: `pnpm --filter @lework/mobile test -- src/session/path-link-rules`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现 `withFilePathLinks` 与 `MarkdownBody`**

- 规则包装：调用原规则得到元素；`isValidElement` 且节点内容有 span 时，按 span 拆 `children`，命中片段换成 `<Text key={...} style={styles.link} onPress={() => open(route)}>{path}</Text>`；`resolve` 返回 null 的片段保留普通文本。
- `markdown.tsx`：`const rules = useMemo(() => withSelectableText(nodeID ? withFilePathLinks(renderRules, resolveRoute, router.push) : renderRules), [nodeID, directory])`，`resolveRoute` 用 `resolveFileLink(token, directory ?? "")` + `fileRoute`。
- 先包路径、再套 `withSelectableText`，保证含路径的节点不加 `selectable`。

- [ ] **Step 4: 跑测试与类型检查**

Run: `pnpm --filter @lework/mobile test && pnpm --filter @lework/mobile typecheck`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/apps/mobile/src/session/path-link-rules.tsx frontend/apps/mobile/src/session/path-link-rules.test.ts frontend/apps/mobile/src/session/markdown.tsx
git commit -m "feat(mobile): 裸文件路径可点开预览"
```

---

### Task 3: 整体验证

**Files:** 无新增，修复验证中发现的问题。

- [ ] **Step 1: 前端验证**

```bash
cd frontend && pnpm typecheck && pnpm --filter @lework/mobile test
```

Expected: PASS。

- [ ] **Step 2: 自查**

- 代码块里的路径没有变成链接。
- 外链、`mailto:`、锚点行为不变。
- `.gitignore` 覆盖的产物没有被暂存（`git status`）。

- [ ] **Step 3: 提交修复（如有）**

```bash
git add -p
git commit -m "test: 补齐裸路径预览验证"
```
