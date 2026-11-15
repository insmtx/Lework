import type { ContentPart } from "@lework/api";
import { describe, expect, it } from "vitest";

import {
	rawToolState,
	toolDuration,
	toolErrorText,
	toolOutputText,
	toolState,
	toolStatus,
	toolTone,
} from "./state";

function part(state: unknown): ContentPart {
	return { type: "tool", tool: "bash", state };
}

describe("toolState", () => {
	it("keeps the provider state as-is", () => {
		expect(toolState(part({ status: "completed", output: "ok" }))).toEqual({
			status: "completed",
			output: "ok",
		});
	});

	it("returns an empty object when state is missing", () => {
		expect(toolState({ type: "tool", tool: "bash" })).toEqual({});
	});
});

describe("toolStatus", () => {
	it("defaults to running", () => {
		expect(toolStatus({})).toBe("running");
		expect(toolStatus({ status: "streaming" })).toBe("streaming");
	});
});

describe("toolTone", () => {
	it("maps completed to ok", () => {
		expect(toolTone({ status: "completed" })).toBe("ok");
	});

	it("keeps running states neutral", () => {
		expect(toolTone({ status: "running" })).toBe("running");
		expect(toolTone({ status: "streaming" })).toBe("running");
		expect(toolTone({})).toBe("running");
	});

	it("treats an error payload as failure even when status says completed", () => {
		expect(toolTone({ status: "error" })).toBe("error");
		expect(toolTone({ status: "completed", error: { message: "boom" } })).toBe("error");
	});
});

describe("toolOutputText", () => {
	it("prefers output", () => {
		expect(toolOutputText({ output: "out", content: [{ type: "text", text: "c" }] })).toBe("out");
	});

	it("falls back to content blocks", () => {
		expect(toolOutputText({ content: [{ type: "text", text: "a" }] })).toBe("a");
	});

	it("falls back to metadata.output", () => {
		expect(toolOutputText({ metadata: { output: "meta" } })).toBe("meta");
	});

	it("returns an empty string when nothing carries text", () => {
		expect(toolOutputText({})).toBe("");
	});
});

describe("toolErrorText", () => {
	it("reads strings and structured errors", () => {
		expect(toolErrorText({ error: "boom" })).toBe("boom");
		expect(toolErrorText({ error: { message: "boom" } })).toBe("boom");
	});

	it("returns an empty string when there is no error", () => {
		expect(toolErrorText({})).toBe("");
	});
});

describe("toolDuration", () => {
	it("formats the time window", () => {
		expect(toolDuration({ time: { start: 1000, end: 2500 } })).toBe("1.5s");
	});

	it("returns undefined while running", () => {
		expect(toolDuration({ time: { start: 1000 } })).toBeUndefined();
	});
});

describe("rawToolState", () => {
	it("pretty prints the raw state", () => {
		expect(rawToolState(part({ status: "completed" }))).toBe(
			JSON.stringify({ status: "completed" }, null, 2),
		);
	});

	it("is empty without state", () => {
		expect(rawToolState({ type: "tool" })).toBe("");
	});
});
