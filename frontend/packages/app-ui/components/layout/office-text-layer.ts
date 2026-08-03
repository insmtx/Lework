import type { DocxTextRunInfo } from "@silurus/ooxml/docx";
import type { PptxTextRunInfo } from "@silurus/ooxml/pptx";

export type OfficeTextRun = DocxTextRunInfo | PptxTextRunInfo;

export function buildOfficeTextLayer({
	canvas,
	format,
	surfaceIndex,
	textLayer,
	runs,
}: {
	canvas: HTMLCanvasElement;
	format: "docx" | "pptx";
	surfaceIndex: number;
	textLayer: HTMLElement;
	runs: OfficeTextRun[];
}): void {
	textLayer.replaceChildren();
	textLayer.style.width = canvas.style.width || `${canvas.width}px`;
	textLayer.style.height = canvas.style.height || `${canvas.height}px`;

	if (format === "docx") {
		buildDocxTextLayer(
			textLayer,
			surfaceIndex,
			runs as DocxTextRunInfo[],
			readCanvasCssDimension(canvas.style.width, canvas.width),
		);
		return;
	}
	buildPptxTextLayer(textLayer, surfaceIndex, runs as PptxTextRunInfo[]);
}

function buildDocxTextLayer(
	textLayer: HTMLElement,
	surfaceIndex: number,
	runs: DocxTextRunInfo[],
	layerWidth: number,
): void {
	// 中文注释：只延伸每行末尾的命中框，避免整页空白起拖时 Chromium 把锚点落到上一段。
	const lineEndRunIndexes = findLineEndRunIndexes(runs);
	for (const [runIndex, run] of runs.entries()) {
		const span = createRunSpan(run.text, surfaceIndex, runIndex);
		const transform = run.transform ? `transform:${run.transform};transform-origin:top left;` : "";
		const hitWidth = lineEndRunIndexes.has(runIndex) ? Math.max(run.w, layerWidth - run.x) : run.w;
		span.style.cssText += `left:${run.x}px;top:${run.y}px;width:${Math.max(0, hitWidth)}px;height:${Math.max(0, run.h)}px;font:${run.font};line-height:${run.h}px;letter-spacing:${run.letterSpacingPx ?? 0}px;${transform}`;
		textLayer.appendChild(span);
	}
}

function findLineEndRunIndexes(runs: DocxTextRunInfo[]): Set<number> {
	const lineEndRunIndexes = new Set<number>();

	for (const [runIndex, run] of runs.entries()) {
		const runRight = run.x + run.w;
		const hasRunToRight = runs.some((candidate, candidateIndex) => {
			if (candidateIndex === runIndex) return false;
			const overlap =
				Math.min(run.y + run.h, candidate.y + candidate.h) - Math.max(run.y, candidate.y);
			const isSameLine = overlap > Math.min(run.h, candidate.h) / 2;
			return isSameLine && candidate.x + candidate.w > runRight;
		});
		if (!hasRunToRight) lineEndRunIndexes.add(runIndex);
	}

	return lineEndRunIndexes;
}

function buildPptxTextLayer(
	textLayer: HTMLElement,
	surfaceIndex: number,
	runs: PptxTextRunInfo[],
): void {
	const shapes = new Map<string, HTMLElement>();

	for (const [runIndex, run] of runs.entries()) {
		const rotation = run.rotation + (run.textBodyRotation ?? 0);
		const shapeKey = [run.shapeX, run.shapeY, run.shapeW, run.shapeH, rotation].join(":");
		let shape = shapes.get(shapeKey);
		if (!shape) {
			shape = document.createElement("div");
			shape.style.cssText = `position:absolute;left:${run.shapeX}px;top:${run.shapeY}px;width:${run.shapeW}px;height:${run.shapeH}px;pointer-events:all;overflow:hidden;`;
			if (rotation !== 0) {
				shape.style.transformOrigin = "center center";
				shape.style.transform = `rotate(${rotation}deg)`;
			}
			shapes.set(shapeKey, shape);
			textLayer.appendChild(shape);
		}

		const span = createRunSpan(run.text, surfaceIndex, runIndex);
		span.style.cssText += `left:${run.inShapeX}px;top:${run.inShapeY}px;width:${Math.max(0, run.w)}px;height:${Math.max(0, run.h)}px;font:${run.font};line-height:${run.h}px;`;
		shape.appendChild(span);
	}
}

function createRunSpan(text: string, surfaceIndex: number, runIndex: number): HTMLSpanElement {
	const span = document.createElement("span");
	span.textContent = text;
	span.dataset.officeSurfaceIndex = String(surfaceIndex);
	span.dataset.officeRunIndex = String(runIndex);
	span.style.cssText =
		"position:absolute;letter-spacing:0;white-space:pre;color:transparent;cursor:text;pointer-events:all;";
	return span;
}

function readCanvasCssDimension(value: string, fallback: number): number {
	const parsed = Number.parseFloat(value);
	return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
