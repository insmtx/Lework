import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import { shellView } from "./shell";

function bash(state: unknown): ContentPart {
	return { type: "tool", tool: "bash", state };
}

describe("shellView", () => {
	it("renders a terminal block with command, workdir, output, exit and duration", () => {
		const view = shellView(
			bash({
				status: "completed",
				input: { command: "ls -la", workdir: "packages/core" },
				content: [{ type: "text", text: "a.txt\nb.txt" }],
				metadata: { exit: 0 },
				time: { start: 1000, end: 2500 },
			}),
		);

		expect(view.summary).toBe("ls -la");
		expect(view.tone).toBe("ok");
		expect(view.blocks[0]).toEqual({
			kind: "terminal",
			command: "ls -la",
			workdir: "packages/core",
			output: "a.txt\nb.txt",
			exit: 0,
			duration: "1.5s",
		});
	});

	it("strips terminal color codes", () => {
		const view = shellView(
			bash({
				status: "completed",
				input: { command: "ls" },
				output: "\u001b[32ma.txt\u001b[0m",
			}),
		);

		expect(view.blocks[0]).toMatchObject({ kind: "terminal", output: "a.txt" });
	});

	it("marks a non-zero exit code as an error", () => {
		const view = shellView(
			bash({
				status: "completed",
				input: { command: "false" },
				output: "",
				metadata: { exit: 1 },
			}),
		);

		expect(view.tone).toBe("error");
		expect(view.blocks[0]).toMatchObject({ kind: "terminal", exit: 1 });
	});

	it("reads output from metadata.output", () => {
		const view = shellView(
			bash({
				status: "completed",
				input: { command: "pwd" },
				metadata: { output: "/w\n", exit: 0 },
			}),
		);

		expect(view.blocks[0]).toMatchObject({ kind: "terminal", output: "/w" });
	});

	it("appends the structured error as its own block", () => {
		const view = shellView(
			bash({
				status: "error",
				input: { command: "rm -rf /nope" },
				error: { message: "permission denied" },
			}),
		);

		expect(view.tone).toBe("error");
		expect(view.blocks.at(-1)).toEqual({ kind: "text", text: "permission denied", tone: "error" });
	});

	it("falls back to a code block when the command is missing", () => {
		const view = shellView(bash({ status: "completed", output: "a\nb" }));

		expect(view.blocks).toEqual([{ kind: "code", text: "a\nb" }]);
	});
});
