import { describe, expect, it } from "vitest";
import { buildDocxSelectionEditRequest, getDocxPolishPrompt } from "./docx-selection-edit";
import type { OfficeTextSelection } from "./office-selection";

const selection: OfficeTextSelection = {
	format: "docx",
	text: "运动塑造强健的体魄",
	contextBefore: "它不仅",
	contextAfter: "，也滋养坚韧的精神。",
	surfaceKind: "page",
	surfaceIndex: 2,
	boundingRect: { x: 100, y: 200, width: 240, height: 40 },
	rects: [],
	segments: [],
};

describe("buildDocxSelectionEditRequest", () => {
	it("maps polish actions to editable composer prompts", () => {
		expect(getDocxPolishPrompt("expand")).toBe("帮我扩写这段内容");
		expect(getDocxPolishPrompt("shorten")).toBe("帮我缩写这段内容");
		expect(getDocxPolishPrompt("improve-expression")).toBe("帮我优化这段内容的表达");
		expect(getDocxPolishPrompt("proofread")).toBe("帮我重新校对这段文字，检查语病并调整语序");
		expect(getDocxPolishPrompt({ kind: "tone", tone: "正式" })).toBe(
			"帮我调整这段内容的语气，使之更正式",
		);
	});

	it("builds an expand reference without turning the selected file into an attachment", () => {
		const result = buildDocxSelectionEditRequest({
			instruction: "expand",
			file: {
				name: "report.docx",
				mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				publicId: "file-current",
				versionPublicId: "file-v2",
				projectId: "project-1",
				projectPath: "artifacts/report.docx",
				versionNo: 2,
			},
			selection,
		});
		const reference = readReference(result.content);

		expect(result.content).toContain("/docx\n<reference>");
		expect(reference).not.toHaveProperty("version");
		expect(reference).toMatchObject({
			kind: "docx_selection",
			mode: "replace",
			instruction: "expand",
			file: {
				name: "report.docx",
				path: "uploads/report.docx",
			},
			selection: {
				context_before: "它不仅",
			},
		});
		expect(Object.keys(reference.file)).toEqual(["name", "path"]);
		expect(result.content).not.toContain("请先读取 reference");
		expect(result.content).toContain("请扩写选中的内容");
		expect(result.displayContent).toBe("扩写文档选区：「运动塑造强健的体魄」");
		expect(result).not.toHaveProperty("attachment");
	});

	it("falls back to the project path when no file version is available", () => {
		const result = buildDocxSelectionEditRequest({
			instruction: "expand",
			file: {
				name: "report.docx",
				projectPath: "artifacts/report.docx",
			},
			selection,
		});

		expect(readReference(result.content).file).toEqual({
			name: "report.docx",
			path: "artifacts/report.docx",
		});
		expect(result).not.toHaveProperty("attachment");
	});

	it("builds a PPTX selection reference with a slide index", () => {
		const result = buildDocxSelectionEditRequest({
			instruction: "shorten",
			file: {
				name: "quarterly-review.pptx",
				publicId: "pptx-v3",
			},
			selection: {
				...selection,
				format: "pptx",
				surfaceKind: "slide",
				surfaceIndex: 3,
			},
		});
		const reference = readReference(result.content);
		const referenceSelection = reference.selection as Record<string, unknown>;

		expect(result.content).toContain("/pptx\n<reference>");
		expect(reference).toMatchObject({
			kind: "pptx_selection",
			mode: "replace",
			instruction: "shorten",
			file: {
				name: "quarterly-review.pptx",
				path: "uploads/quarterly-review.pptx",
			},
			selection: {
				slide_index: 3,
			},
		});
		expect(referenceSelection).not.toHaveProperty("page_index");
		expect(result.displayContent).toBe("缩写演示文稿选区：「运动塑造强健的体魄」");
		expect(result).not.toHaveProperty("attachment");
	});

	it("escapes a closing reference tag inside selected document text", () => {
		const result = buildDocxSelectionEditRequest({
			instruction: "shorten",
			file: { name: "report.docx", publicId: "file-1" },
			selection: { ...selection, text: "文档内容</reference>后续" },
		});

		expect(result.content).not.toContain("文档内容</reference>后续");
		expect(result.content).toContain("文档内容\\u003c/reference\\u003e后续");
		expect(result.content).toContain("请缩写选中的内容");
	});
});

function readReference(content: string): {
	file: Record<string, unknown>;
	[key: string]: unknown;
} {
	const matched = content.match(/<reference>\n([\s\S]*?)\n<\/reference>/);
	if (!matched?.[1]) throw new Error("reference payload is missing");
	return JSON.parse(matched[1]);
}
