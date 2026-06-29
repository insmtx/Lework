import { describe, expect, it } from "vitest";
import { getOfficeOpenXmlFormat } from "./OfficePreview";

describe("getOfficeOpenXmlFormat", () => {
	it.each([
		["report.docx", "", "docx"],
		["budget.XLSX", "", "xlsx"],
		["slides.pptx", "", "pptx"],
		[
			"download",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation",
			"pptx",
		],
	])("detects %s as %s", (fileName, mimeType, expected) => {
		expect(getOfficeOpenXmlFormat(fileName, mimeType)).toBe(expected);
	});

	it("does not classify legacy Office formats as OOXML", () => {
		expect(getOfficeOpenXmlFormat("legacy.xls", "application/vnd.ms-excel")).toBeNull();
		expect(getOfficeOpenXmlFormat("legacy.doc", "application/msword")).toBeNull();
	});
});
