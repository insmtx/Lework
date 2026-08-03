import type { FilePreviewItem } from "./file-preview-utils";
import type { OfficeTextSelection } from "./office-selection";

export type DocxSelectionInstruction = "expand" | "shorten";
export type DocxTone = "正式" | "随意" | "亲切" | "简洁" | "生动" | "有说服力";
export type DocxPolishAction =
	| "expand"
	| "shorten"
	| "improve-expression"
	| "proofread"
	| { kind: "tone"; tone: DocxTone };

export type DocxSelectionEditRequest = {
	content: string;
	displayContent: string;
};

export const DOCX_SELECTION_TEXT_LIMIT = 20_000;

const instructionCopy: Record<DocxSelectionInstruction, { label: string; prompt: string }> = {
	expand: {
		label: "扩写",
		prompt:
			"请扩写选中的内容，在保持原意、事实准确性和上下文语气的前提下补充必要细节，并将修改写回原 DOCX 文件。不要修改选区之外的内容。",
	},
	shorten: {
		label: "缩写",
		prompt:
			"请缩写选中的内容，在保留核心信息、事实准确性和上下文语气的前提下删除冗余表达，并将修改写回原 DOCX 文件。不要修改选区之外的内容。",
	},
};

export const DOCX_TONES: DocxTone[] = ["正式", "随意", "亲切", "简洁", "生动", "有说服力"];

export function getDocxPolishPrompt(action: DocxPolishAction): string {
	if (typeof action === "object") {
		return `帮我调整这段内容的语气，使之更${action.tone}`;
	}
	return {
		expand: "帮我扩写这段内容",
		shorten: "帮我缩写这段内容",
		"improve-expression": "帮我优化这段内容的表达",
		proofread: "帮我重新校对这段文字，检查语病并调整语序",
	}[action];
}

export function buildDocxSelectionEditRequest({
	instruction,
	file,
	selection,
}: {
	instruction: DocxSelectionInstruction;
	file: FilePreviewItem;
	selection: OfficeTextSelection;
}): DocxSelectionEditRequest {
	const copy = instructionCopy[instruction];
	const formatLabel = selection.format === "pptx" ? "PPTX" : "DOCX";
	const selectionLabel = selection.format === "pptx" ? "演示文稿选区" : "文档选区";
	const previewText = selection.text.trim().replace(/\s+/g, " ").slice(0, 48);
	return buildDocxSelectionPromptRequest({
		prompt: copy.prompt.replace("DOCX", formatLabel),
		displayContent: `${copy.label}${selectionLabel}：「${previewText}${selection.text.trim().length > 48 ? "…" : ""}」`,
		instruction,
		file,
		selection,
	});
}

export function buildDocxSelectionPromptRequest({
	prompt,
	displayContent,
	instruction = "custom",
	file,
	selection,
}: {
	prompt: string;
	displayContent?: string;
	instruction?: string;
	file: FilePreviewItem;
	selection: OfficeTextSelection;
}): DocxSelectionEditRequest {
	const format = selection.format === "pptx" ? "pptx" : "docx";
	const formatConfig =
		format === "pptx"
			? {
					command: "/pptx",
					kind: "pptx_selection",
				}
			: {
					command: "/docx",
					kind: "docx_selection",
				};
	const filePublicId = file.versionPublicId?.trim() || file.publicId?.trim();
	const safeName = baseName(file.name);
	const filePath = filePublicId ? `uploads/${safeName}` : file.projectPath?.trim();
	const selectedText = selection.text;
	const reference = {
		kind: formatConfig.kind,
		mode: "replace",
		instruction,
		file: {
			name: safeName,
			path: filePath,
		},
		selection: {
			text: selectedText,
			context_before: selection.contextBefore,
			context_after: selection.contextAfter,
			...(format === "pptx"
				? { slide_index: selection.surfaceIndex }
				: { page_index: selection.surfaceIndex }),
			offset_encoding: "utf16",
		},
	};
	const serializedReference = JSON.stringify(reference, null, 2)
		.replaceAll("&", "\\u0026")
		.replaceAll("<", "\\u003c")
		.replaceAll(">", "\\u003e");
	const previewText = selection.text.trim().replace(/\s+/g, " ").slice(0, 48);

	return {
		content: `${formatConfig.command}\n<reference>\n${serializedReference}\n</reference>\n\n${prompt.trim()}`,
		displayContent:
			displayContent ??
			`${prompt.trim()}：「${previewText}${selection.text.trim().length > 48 ? "…" : ""}」`,
	};
}

function baseName(value: string): string {
	return value.split(/[\\/]/).filter(Boolean).pop() || "document.docx";
}
