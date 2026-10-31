import type { ComposerToken } from "@lework/store/types/chat";
import { describe, expect, it } from "vitest";
import {
	applyDocxSelectionDraftToComposer,
	type DocxSelectionComposerDraft,
} from "./docx-selection-composer-store";

function createDraft(
	id: string,
	referenceLabel: string,
	suggestedPrompt?: string,
): DocxSelectionComposerDraft {
	return {
		id,
		referenceId: `file-${id}`,
		referenceLabel,
		file: { name: "report.docx", publicId: `file-${id}` },
		selection: {
			format: "docx",
			text: referenceLabel,
			contextBefore: "",
			contextAfter: "",
			surfaceKind: "page",
			surfaceIndex: 0,
			boundingRect: { x: 0, y: 0, width: 100, height: 20 },
			rects: [],
			segments: [],
		},
		suggestedPrompt,
		selectedVersionPublicId: "",
	};
}

describe("applyDocxSelectionDraftToComposer", () => {
	it("replaces the previous reference while preserving the user's request", () => {
		const first = applyDocxSelectionDraftToComposer({
			value: "换个更清晰的说法",
			tokens: [],
			draft: createDraft("a", "第一处文字"),
		});
		const second = applyDocxSelectionDraftToComposer({
			value: first.value,
			tokens: first.tokens,
			draft: createDraft("b", "第二处文字"),
		});

		expect(second.value).toBe("第二处文字 换个更清晰的说法");
		expect(second.tokens.filter((token) => token.kind === "reference")).toEqual([
			expect.objectContaining({ id: "file-b", label: "第二处文字", start: 0 }),
		]);
	});

	it("replaces a previous shortcut prompt and keeps additional instructions", () => {
		const first = applyDocxSelectionDraftToComposer({
			value: "",
			tokens: [],
			draft: createDraft("a", "第一处文字", "帮我扩写这段内容"),
		});
		const value = `${first.value} 并保留专业术语`;
		const tokens: ComposerToken[] = first.tokens;
		const second = applyDocxSelectionDraftToComposer({
			value,
			tokens,
			draft: createDraft("b", "第二处文字", "帮我优化这段内容的表达"),
			previousSuggestedPrompt: "帮我扩写这段内容",
		});

		expect(second.value).toBe("帮我优化这段内容的表达 第二处文字 并保留专业术语");
		expect(second.tokens).toHaveLength(1);
		expect(second.tokens[0]).toMatchObject({
			kind: "reference",
			label: "第二处文字",
			start: "帮我优化这段内容的表达 ".length,
		});
	});

	it("restores the reference token after the composer remounts", () => {
		const draft = createDraft("a", "被引用的文字", "帮我扩写这段内容");
		const restored = applyDocxSelectionDraftToComposer({
			value: "帮我扩写这段内容 被引用的文字 并保留原语气",
			tokens: [],
			draft,
		});

		expect(restored.value).toBe("帮我扩写这段内容 被引用的文字 并保留原语气");
		expect(restored.tokens).toEqual([
			expect.objectContaining({
				kind: "reference",
				id: "file-a",
				label: "被引用的文字",
			}),
		]);
	});
});
