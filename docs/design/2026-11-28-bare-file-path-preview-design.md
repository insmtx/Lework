# 裸文件路径预览设计

## 背景

`docs/design/2026-11-27-mobile-file-link-preview-design.md` 只拦截 Markdown 链接，
助手回复里的裸路径（例如「设计文档写好了：docs/superpowers/specs/x-design.md」）
仍然只是一段文本，手机上点不开。本次把这个非目标补上。

## 目标

助手回复正文里的裸文件路径可以点击，直接打开现有文件预览页
（`app/file/[node]`）。指向一个文件即可，不改变预览页本身。

## 非目标

- 不带斜杠的裸文件名（`README.md`、`config.yaml`）不识别：正文里太容易误伤。
- 代码块（fence / code_block）里的内容不识别。
- 用户消息、思考过程、工具输出不识别（它们本来就不走 Markdown 正文渲染）。
- 不识别目录；不新增后端接口。

## 识别规则

新增纯函数 `findFilePathSpans(content)`，在文本节点里找出路径片段：

- 片段必须含 `/` 或 `\`，且最后一段带扩展名（`.md`、`.go`、`.png` 等，1–10 位字母数字）。
- 路径只吃 ASCII 字母数字与 `._~%+-/\:` 这些字符；中文字符当作正文边界，
  「详见docs/x.md」只链接 `docs/x.md`，不会把「详见」吞进去。
- 支持相对路径（`docs/x.md`、`../docs/x.md`、`./docs/x.md`）、POSIX 绝对路径、
  Windows 盘符（`C:\repo\x.md`）与 UNC（`\\box\share\x.md`）。
- 排除 URL：`http(s)://`、`//host/x.md`，以及首段像域名的 token
  （`example.com/x.md`）。首段带点又确实想引用的本地目录会被漏掉，这是有意的取舍。
- 片段两侧的标点（`（`、`，`、`。`、`)`、行尾 `.`）不属于路径。
- 相对路径按当前消息的会话目录解析，复用 `resolveFileLink`；拿不到会话目录时
  只保留绝对路径可点。

已知取舍：`v1.2/README.md` 这类「首段带点」的相对路径按域名处理，不识别。
中文文件名（`docs/设计.md`）也不识别——中文字符是正文边界，这条取舍换来
「详见docs/x.md」不会把前缀吞进路径；需要时可改用百分号编码。
`~/x.md` 与单前导反斜杠的路径同样不识别，因为节点侧解析不出可用的绝对目录。

## 渲染接线

`MarkdownBody` 给 `text` 与 `code_inline` 两条规则套一层包装：命中路径片段的
文本拆成「普通文本 + 带 `onPress` 的内层 `Text`」，样式复用 `styles.link`。
顺序是先包路径、再套 `withSelectableText`，这样含路径的节点不会加 `selectable`
（selectable 会吞掉 onPress，和现有链接同样的约束）。

点击时 `router.push(fileRoute(nodeID, target.directory, target.path))`；解析失败
（外链、目录不可知）保持普通文本。链接标签（`[]()` 的显示文字）不参与识别，
避免抢掉链接本身的跳转。

## 文件

| 文件 | 改动 |
| --- | --- |
| `src/session/file-paths.ts` | 新增 `findFilePathSpans` |
| `src/session/file-paths.test.ts` | 新增单测 |
| `src/session/markdown.tsx` | 组合路径规则，点击打开预览 |

## 验证

- 单测覆盖：裸路径、绝对/Windows/UNC、行内代码内容、URL/域名、`and/or`、`24/7`、
  无扩展名、无斜杠文件名、中英文标点边界、多个路径。
- `pnpm --filter @lework/mobile typecheck`、`pnpm --filter @lework/mobile test`。
- 手工：会话里点裸路径（含中文冒号/括号包裹）；点 Markdown 链接仍按原行为；
  代码块里的路径不变成链接。
