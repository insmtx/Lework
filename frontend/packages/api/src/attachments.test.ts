import { describe, expect, it } from "vitest";
import { attachmentNote } from "./attachments";

describe("attachmentNote", () => {
	it("空列表返回空串", () => {
		expect(attachmentNote([])).toBe("");
	});

	it("列出上传的文件", () => {
		expect(attachmentNote(["a.pdf", "b.png"])).toBe("\n\n已上传到项目根目录：a.pdf, b.png");
	});
});
