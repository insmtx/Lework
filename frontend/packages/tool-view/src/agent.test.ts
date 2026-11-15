import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { agentView } from "./agent";

function tool(name: string, state: unknown): ContentPart {
	return { type: "tool", tool: name, state };
}

describe("subagent", () => {
	it("builds a page model with prompt and result", () => {
		const view = agentView(
			tool("subagent", {
				status: "completed",
				input: {
					agent: "explore",
					description: "Map the auth flow",
					prompt: "Find the authentication entrypoints.",
				},
				output: "Found 3 files.",
				metadata: { sessionID: "ses_abc123" },
			}),
		);

		expect(view.summary).toBe("explore · Map the auth flow");
		expect(view.subagent).toEqual({
			agent: "explore",
			description: "Map the auth flow",
			prompt: "Find the authentication entrypoints.",
			result: "Found 3 files.",
			sessionID: "ses_abc123",
			background: false,
		});
		expect(view.blocks).toEqual([
			{ kind: "text", label: "prompt", text: "Find the authentication entrypoints." },
			{ kind: "code", label: "result", text: "Found 3 files." },
		]);
	});

	it("extracts the child session id from the result text", () => {
		const view = agentView(
			tool("subagent", {
				status: "completed",
				input: { agent: "general", prompt: "Do it" },
				output: "Started sessionID: ses_9f8e7d",
			}),
		);

		expect(view.summary).toBe("general");
		expect(view.subagent?.sessionID).toBe("ses_9f8e7d");
	});

	it("includes the run duration for the detail page", () => {
		const view = agentView(
			tool("subagent", {
				status: "completed",
				input: { agent: "explore", prompt: "Look" },
				output: "done",
				time: { start: 0, end: 2000 },
			}),
		);

		expect(view.subagent?.duration).toBe("2.0s");
	});

	it("accepts the legacy task tool name", () => {
		const view = agentView(
			tool("task", {
				status: "completed",
				input: { agent: "explore", description: "Scan", prompt: "Scan the repo" },
			}),
		);

		expect(view.summary).toBe("explore · Scan");
		expect(view.subagent?.agent).toBe("explore");
	});
});

describe("skill", () => {
	it("names the loaded skill and shows its body", () => {
		const view = agentView(
			tool("skill", {
				status: "completed",
				input: { id: "git-release" },
				output: "1. Tag the commit\n2. Push",
			}),
		);

		expect(view.summary).toBe("git-release");
		expect(view.blocks).toEqual([
			{ kind: "text", label: "content", text: "1. Tag the commit\n2. Push" },
		]);
	});
});

describe("todowrite", () => {
	it("renders the checklist and counts completed items", () => {
		const view = agentView(
			tool("todowrite", {
				status: "completed",
				input: {
					todos: [
						{ content: "write tests", status: "completed" },
						{ content: "ship it", status: "in_progress" },
					],
				},
			}),
		);

		expect(view.summary).toBe("1/2 完成");
		expect(view.blocks).toEqual([
			{
				kind: "checklist",
				items: [
					{ text: "write tests", done: true },
					{ text: "ship it", done: false },
				],
			},
		]);
	});
});

describe("execute", () => {
	it("shows the script and its output", () => {
		const view = agentView(
			tool("execute", {
				status: "completed",
				input: { code: "return await tools.read()" },
				output: "ok",
			}),
		);

		expect(view.summary).toBe("return await tools.read()");
		expect(view.blocks).toEqual([
			{ kind: "code", label: "code", text: "return await tools.read()" },
			{ kind: "code", label: "output", text: "ok" },
		]);
	});
});

describe("question", () => {
	it("renders each question with its options", () => {
		const view = agentView(
			tool("question", {
				status: "running",
				input: {
					questions: [
						{
							header: "API 变更",
							question: "选择破坏性变更还是兼容层？",
							options: [{ label: "破坏性" }, { label: "兼容层" }],
						},
					],
				},
			}),
		);

		expect(view.summary).toBe("选择破坏性变更还是兼容层？");
		expect(view.blocks).toEqual([
			{
				kind: "text",
				label: "API 变更",
				text: "选择破坏性变更还是兼容层？\n• 破坏性\n• 兼容层",
			},
		]);
	});
});
