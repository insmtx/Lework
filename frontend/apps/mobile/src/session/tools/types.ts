import type { ContentPart } from "@lework/api";

import type { DiffLine } from "./diff";

/** ToolTone 决定状态文字和边框的语气，错误工具的 exit code 也会覆盖它。 */
export type ToolTone = "ok" | "running" | "error";

export interface ToolField {
	label: string;
	value: string;
}

/**
 * ToolBlock 是展开区的最小渲染单元。
 *
 * 所有工具共用这几种块，视图层只负责把 state 翻译成块，组件不需要知道
 * 具体是哪个工具，新工具只要产出块就能获得一致的排版。
 */
export type ToolBlock =
	| {
			kind: "terminal";
			label?: string;
			command: string;
			workdir?: string;
			output: string;
			exit?: number;
			duration?: string;
	  }
	| { kind: "code"; label?: string; text: string }
	| { kind: "diff"; label?: string; lines: DiffLine[] }
	| { kind: "list"; label?: string; items: string[] }
	| { kind: "checklist"; label?: string; items: { text: string; done: boolean }[] }
	| { kind: "fields"; label?: string; fields: ToolField[] }
	| { kind: "text"; label?: string; text: string; tone?: "muted" | "error" };

/** SubagentView 是子代理专用数据；手机端用全屏页展示，避免挤在气泡里。 */
export interface SubagentView {
	agent: string;
	description: string;
	prompt: string;
	result: string;
	sessionID?: string;
	duration?: string;
	background: boolean;
}

export interface ToolView {
	tool: string;
	status: string;
	tone: ToolTone;
	/** 折叠状态下的一行摘要，让用户不展开也知道这一步在做什么。 */
	summary: string;
	blocks: ToolBlock[];
	subagent?: SubagentView;
	/** 原始 state 的 JSON，保留为排查入口。 */
	raw: string;
}

export type ToolPart = ContentPart;
