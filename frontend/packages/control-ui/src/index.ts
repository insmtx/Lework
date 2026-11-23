/**
 * @lework/control-ui 是 OpenCode 控制面会话界面的共享实现。
 *
 * apps/control-plane（同源内嵌）与 apps/web（Next rewrite 代理）都通过
 * `ControlApiProvider` 注入自己的 API 客户端、SSE 地址与 PTY 地址，组件本身
 * 不依赖任何宿主环境。
 */

export { ControlAuthGate } from "./components/control-auth-gate";
export { Conversation } from "./components/conversation";
export { DiffView } from "./components/diff-view";
export { FileBrowser } from "./components/file-browser";
export { NodeSidebar } from "./components/node-sidebar";
export { PermissionCard } from "./components/permission-card";
export { SessionPanel } from "./components/session-panel";
export { TerminalPanel } from "./components/terminal-panel";
export { RawJson } from "./components/tool-call/raw-json";
export { SubagentDialog } from "./components/tool-call/subagent-dialog";
export { ToolBlocks } from "./components/tool-call/tool-blocks";
export { ToolCard, ToolCardList } from "./components/tool-call/tool-card";
export { ControlConsole, type ControlConsoleProps } from "./console";
export {
	type ControlApiContextValue,
	ControlApiProvider,
	defaultPtyUrl,
	useControlApi,
} from "./context";
export { type DiffRow, parsePatch } from "./diff";
export type { NodeColorStyle, NodeColorToken } from "./node-colors";
export { NODE_COLOR_STYLES, NODE_COLOR_TOKENS, nodeColorStyle } from "./node-colors";
export * from "./types";
export { useControlEvents } from "./use-control-events";
