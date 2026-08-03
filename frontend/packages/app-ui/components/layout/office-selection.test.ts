import { afterEach, describe, expect, it } from "vitest";
import {
	buildXlsxSelectionText,
	clearOfficeBrowserSelection,
	mapViewportRectToSurface,
	normalizeXlsxSelectionRange,
	readOfficeTextSelection,
} from "./office-selection";
import { buildOfficeTextLayer } from "./office-text-layer";

afterEach(() => {
	window.getSelection()?.removeAllRanges();
	document.body.replaceChildren();
});

describe("office text selection", () => {
	it("returns selected run offsets and render-relative rectangles", () => {
		const host = document.createElement("div");
		const surface = document.createElement("div");
		surface.dataset.officeSurfaceIndex = "2";
		const first = createRun("Hello", 2, 0);
		const second = createRun("World", 2, 1);
		surface.append(first, second);
		host.appendChild(surface);
		document.body.appendChild(host);

		Object.defineProperty(surface, "getBoundingClientRect", {
			value: () => new DOMRect(100, 200, 400, 600),
		});

		const range = document.createRange();
		range.setStart(first.firstChild as Text, 1);
		range.setEnd(second.firstChild as Text, 3);
		Object.defineProperty(range, "getClientRects", {
			value: () => [new DOMRect(120, 220, 80, 18)],
		});

		const browserSelection = window.getSelection();
		browserSelection?.addRange(range);
		const result = readOfficeTextSelection(host, "docx", browserSelection);

		expect(result).toMatchObject({
			format: "docx",
			text: "elloWor",
			contextBefore: "H",
			contextAfter: "ld",
			surfaceKind: "page",
			surfaceIndex: 2,
			boundingRect: { x: 120, y: 220, width: 80, height: 18 },
			segments: [
				{ runIndex: 0, startOffset: 1, endOffset: 5, text: "ello" },
				{ runIndex: 1, startOffset: 0, endOffset: 3, text: "Wor" },
			],
		});
		expect(result?.rects[0]).toMatchObject({
			surfaceIndex: 2,
			surface: { x: 20, y: 20, width: 80, height: 18 },
			normalized: { x: 0.05, width: 0.2 },
		});
	});

	it("clears a browser selection owned by the preview", () => {
		const host = document.createElement("div");
		const run = createRun("Selected", 0, 0);
		host.appendChild(run);
		document.body.appendChild(host);
		const range = document.createRange();
		range.selectNodeContents(run);
		window.getSelection()?.addRange(range);

		clearOfficeBrowserSelection(host);

		expect(window.getSelection()?.rangeCount).toBe(0);
	});
});

describe("office text layer", () => {
	it("builds selectable DOCX runs at renderer coordinates", () => {
		const canvas = document.createElement("canvas");
		canvas.style.width = "800px";
		canvas.style.height = "1000px";
		const layer = document.createElement("div");

		buildOfficeTextLayer({
			canvas,
			format: "docx",
			surfaceIndex: 1,
			textLayer: layer,
			runs: [
				{
					text: "Document",
					x: 40,
					y: 60,
					w: 120,
					h: 24,
					fontSize: 16,
					font: "16px Arial",
					letterSpacingPx: 4,
				},
			],
		});

		const span = layer.querySelector("span");
		expect(layer.style.width).toBe("800px");
		expect(span?.textContent).toBe("Document");
		expect(span?.dataset.officeSurfaceIndex).toBe("1");
		expect(span?.dataset.officeRunIndex).toBe("0");
		expect(span?.style.left).toBe("40px");
		expect(span?.style.top).toBe("60px");
		expect(span?.style.width).toBe("760px");
		expect(span?.style.height).toBe("24px");
		expect(span?.style.letterSpacing).toBe("4px");
	});

	it("extends only the rightmost DOCX run on each line to the page edge", () => {
		const canvas = document.createElement("canvas");
		canvas.style.width = "800px";
		canvas.style.height = "1000px";
		const layer = document.createElement("div");

		buildOfficeTextLayer({
			canvas,
			format: "docx",
			surfaceIndex: 1,
			textLayer: layer,
			runs: [
				createDocxRun({ text: "First", x: 40, y: 60, w: 120 }),
				createDocxRun({ text: "Last", x: 200, y: 60, w: 100 }),
				createDocxRun({ text: "Next line", x: 40, y: 100, w: 120 }),
			],
		});

		const spans = layer.querySelectorAll("span");
		expect(Array.from(spans, (span) => span.style.width)).toEqual(["120px", "600px", "760px"]);
	});

	it("groups PPTX runs into a rotated shape", () => {
		const canvas = document.createElement("canvas");
		canvas.style.width = "960px";
		canvas.style.height = "540px";
		const layer = document.createElement("div");

		buildOfficeTextLayer({
			canvas,
			format: "pptx",
			surfaceIndex: 3,
			textLayer: layer,
			runs: [
				{
					text: "Slide",
					inShapeX: 10,
					inShapeY: 20,
					w: 80,
					h: 24,
					fontSize: 16,
					font: "16px Arial",
					shapeX: 100,
					shapeY: 120,
					shapeW: 300,
					shapeH: 100,
					rotation: 30,
					textBodyRotation: 90,
				},
			],
		});

		const shape = layer.firstElementChild as HTMLElement | null;
		const span = shape?.querySelector("span");
		expect(shape?.style.transform).toBe("rotate(120deg)");
		expect(span?.style.left).toBe("10px");
		expect(span?.style.top).toBe("20px");
		expect(span?.style.width).toBe("80px");
		expect(span?.style.height).toBe("24px");
		expect(span?.dataset.officeSurfaceIndex).toBe("3");
	});
});

function createDocxRun({ text, x, y, w }: { text: string; x: number; y: number; w: number }) {
	return {
		text,
		x,
		y,
		w,
		h: 24,
		fontSize: 16,
		font: "16px Arial",
	};
}

describe("xlsx selection", () => {
	it("normalizes row selections against the used sheet range", () => {
		expect(
			normalizeXlsxSelectionRange(
				{
					anchor: { row: 5, col: 1 },
					active: { row: 3, col: 1 },
					mode: "rows",
				},
				{ startRow: 1, endRow: 20, startCol: 2, endCol: 6 },
			),
		).toEqual({ startRow: 3, endRow: 5, startCol: 2, endCol: 6 });
	});

	it("serializes selected cells as TSV", () => {
		const values = new Map([
			["1:1", "A"],
			["1:2", "B"],
			["2:1", "C"],
			["2:2", "D"],
		]);
		const text = buildXlsxSelectionText(
			{ startRow: 1, endRow: 2, startCol: 1, endCol: 2 },
			(row, col) => values.get(`${row}:${col}`) ?? "",
		);
		expect(text).toBe("A\tB\nC\tD");
	});

	it("maps viewport coordinates to stable normalized surface coordinates", () => {
		expect(
			mapViewportRectToSurface({ x: 150, y: 260, width: 100, height: 40 }, 0, {
				x: 100,
				y: 200,
				width: 500,
				height: 400,
			}),
		).toEqual({
			surfaceIndex: 0,
			viewport: { x: 150, y: 260, width: 100, height: 40 },
			surface: { x: 50, y: 60, width: 100, height: 40 },
			normalized: { x: 0.1, y: 0.15, width: 0.2, height: 0.1 },
		});
	});
});

function createRun(text: string, surfaceIndex: number, runIndex: number): HTMLSpanElement {
	const span = document.createElement("span");
	span.textContent = text;
	span.dataset.officeSurfaceIndex = String(surfaceIndex);
	span.dataset.officeRunIndex = String(runIndex);
	return span;
}
