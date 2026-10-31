"use client";

import type { CellRange, XlsxViewer } from "@silurus/ooxml/xlsx";
import { LoaderCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { WorkBook } from "xlsx";
import {
	buildXlsxSelectionText,
	clearOfficeBrowserSelection,
	mapViewportRectToSurface,
	normalizeXlsxSelectionRange,
	type OfficeRect,
	type OfficeTextSelection,
	observeOfficeTextSelection,
	type XlsxOneBasedRange,
} from "./office-selection";
import { buildOfficeTextLayer, type OfficeTextRun } from "./office-text-layer";

export type { OfficeTextSelection } from "./office-selection";

export type OfficeOpenXmlFormat = "docx" | "xlsx" | "pptx";

const PPTX_DESKTOP_MAX_WIDTH = 1180;
const PPTX_TABLET_MAX_WIDTH = 960;
const PPTX_MIN_WIDTH = 320;
const DOCX_DESKTOP_MAX_WIDTH = 1120;
const DOCX_TABLET_MAX_WIDTH = 920;
const DOCX_MIN_WIDTH = 320;

export function OfficePreview({
	buffer,
	fileName,
	format,
	onTextSelectionChange,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	format: OfficeOpenXmlFormat;
	onTextSelectionChange?: (selection: OfficeTextSelection | null) => void;
}) {
	if (format === "xlsx") {
		return (
			<XlsxPreview
				buffer={buffer}
				fileName={fileName}
				onTextSelectionChange={onTextSelectionChange}
			/>
		);
	}

	return (
		<ScrollOfficePreview
			buffer={buffer}
			fileName={fileName}
			format={format}
			onTextSelectionChange={onTextSelectionChange}
		/>
	);
}

function ScrollOfficePreview({
	buffer,
	fileName,
	format,
	onTextSelectionChange,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	format: "docx" | "pptx";
	onTextSelectionChange?: (selection: OfficeTextSelection | null) => void;
}) {
	const canvasHostRef = useRef<HTMLDivElement>(null);
	const scrollViewportRef = useRef<HTMLDivElement>(null);
	const selectionChangeRef = useRef(onTextSelectionChange);
	const tracksTextSelection = Boolean(onTextSelectionChange);
	const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
	const [error, setError] = useState("");

	useEffect(() => {
		selectionChangeRef.current = onTextSelectionChange;
	}, [onTextSelectionChange]);

	useEffect(() => {
		const host = canvasHostRef.current;
		if (!host || !tracksTextSelection) return;
		return observeOfficeTextSelection({
			host,
			format,
			onChange: (selection) => selectionChangeRef.current?.(selection),
		});
	}, [format, tracksTextSelection]);

	const clearCurrentSelection = (event: React.PointerEvent<HTMLDivElement>) => {
		const target = event.target;
		if (target instanceof Element && target.closest("[data-docx-selection-toolbar]")) return;
		const host = canvasHostRef.current;
		if (!host) return;
		clearOfficeBrowserSelection(host);
		selectionChangeRef.current?.(null);
	};

	useEffect(() => {
		const canvasHost = canvasHostRef.current;
		if (!canvasHost) return;
		const hostElement = canvasHost;
		const sourceBuffer = copyArrayBuffer(buffer);

		let cancelled = false;
		let resizeFrame = 0;
		let resizeObserver: ResizeObserver | undefined;
		let documentRenderer: ScrollDocumentRenderer | null = null;
		setStatus("loading");
		setError("");
		clearOfficeBrowserSelection(hostElement);
		selectionChangeRef.current?.(null);
		hostElement.replaceChildren();

		async function loadDocument() {
			try {
				documentRenderer =
					format === "docx"
						? await loadDocxDocument(sourceBuffer)
						: await loadPptxDocument(sourceBuffer);
				if (cancelled) return;

				await renderAllSurfaces({
					documentRenderer,
					fileName,
					format,
					hostElement,
				});
				if (cancelled) return;

				setStatus("ready");
				resizeObserver = new ResizeObserver(() => {
					cancelAnimationFrame(resizeFrame);
					resizeFrame = requestAnimationFrame(() => {
						if (!documentRenderer) return;
						clearOfficeBrowserSelection(hostElement);
						selectionChangeRef.current?.(null);
						void renderAllSurfaces({
							documentRenderer,
							fileName,
							format,
							hostElement,
						}).catch(handleRenderError);
					});
				});
				resizeObserver.observe(hostElement);
				if (hostElement.parentElement) {
					resizeObserver.observe(hostElement.parentElement);
				}
			} catch (err) {
				handleRenderError(err);
			}
		}

		function handleRenderError(err: unknown) {
			if (cancelled) return;
			setError(err instanceof Error ? err.message : `${format.toUpperCase()} 预览失败`);
			setStatus("error");
		}

		void loadDocument();

		return () => {
			cancelled = true;
			cancelAnimationFrame(resizeFrame);
			resizeObserver?.disconnect();
			documentRenderer?.destroy();
			clearOfficeBrowserSelection(hostElement);
			hostElement.replaceChildren();
		};
	}, [buffer, fileName, format]);

	return (
		<div
			className={`relative flex h-full min-h-[320px] flex-col overflow-hidden ${
				format === "pptx"
					? "bg-[radial-gradient(circle_at_top,#f8fafc_0%,#eef1f6_42%,#e4e9f1_100%)]"
					: "bg-[#eef1f6]"
			}`}
		>
			<div
				ref={scrollViewportRef}
				data-office-scroll-viewport
				className="relative min-h-0 flex-1 overflow-auto p-4"
				onPointerDownCapture={clearCurrentSelection}
			>
				<div
					ref={canvasHostRef}
					className={
						format === "pptx"
							? "flex min-h-full flex-col items-center gap-8 py-8"
							: "flex min-h-full flex-col items-center gap-5 py-3"
					}
				/>
				{status === "loading" && <PreviewStatus label={`正在渲染 ${format.toUpperCase()}`} />}
				{status === "error" && <PreviewError format={format} message={error} />}
			</div>
		</div>
	);
}

function XlsxPreview({
	buffer,
	fileName,
	onTextSelectionChange,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	onTextSelectionChange?: (selection: OfficeTextSelection | null) => void;
}) {
	const containerRef = useRef<HTMLDivElement>(null);
	const selectionChangeRef = useRef(onTextSelectionChange);
	const tracksTextSelection = Boolean(onTextSelectionChange);
	const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
	const [error, setError] = useState("");

	useEffect(() => {
		selectionChangeRef.current = onTextSelectionChange;
	}, [onTextSelectionChange]);

	useEffect(() => {
		const container = containerRef.current;
		if (!container) return;
		const containerElement = container;
		const viewerBuffer = copyArrayBuffer(buffer);

		let cancelled = false;
		let viewer: XlsxViewer | undefined;
		let workbook: WorkBook | undefined;
		let xlsxModule: typeof import("xlsx") | undefined;
		let activeSheetIndex = 0;
		let activeSelection: CellRange | null = null;
		let selectionFrame = 0;
		setStatus("loading");
		setError("");
		selectionChangeRef.current?.(null);

		const emitSelection = () => {
			selectionFrame = 0;
			if (!activeSelection || !workbook || !xlsxModule) {
				selectionChangeRef.current?.(null);
				return;
			}
			selectionChangeRef.current?.(
				createXlsxTextSelection({
					container: containerElement,
					selection: activeSelection,
					sheetIndex: activeSheetIndex,
					workbook,
					xlsx: xlsxModule,
				}),
			);
		};

		const scheduleSelection = () => {
			cancelAnimationFrame(selectionFrame);
			selectionFrame = requestAnimationFrame(emitSelection);
		};

		containerElement.addEventListener("scroll", scheduleSelection, true);
		window.addEventListener("resize", scheduleSelection);

		async function loadViewer() {
			try {
				const [{ XlsxViewer }, loadedXlsxModule] = await Promise.all([
					import("@silurus/ooxml/xlsx"),
					import("xlsx"),
				]);
				if (cancelled) return;
				xlsxModule = loadedXlsxModule;
				const { hydrateXlsxFormulaCache } = await import("./xlsx-formula-preview");
				const previewBuffer = await hydrateXlsxFormulaCache(viewerBuffer);
				if (cancelled) return;
				if (tracksTextSelection) {
					workbook = loadedXlsxModule.read(previewBuffer, {
						type: "array",
						cellDates: true,
					});
				}

				viewer = new XlsxViewer(containerElement, {
					showZoomSlider: true,
					onSheetChange: (index) => {
						activeSheetIndex = index;
						activeSelection = null;
						selectionChangeRef.current?.(null);
					},
					onSelectionChange: (selection) => {
						activeSelection = selection;
						if (!selection || !viewer) {
							selectionChangeRef.current?.(null);
							return;
						}
						emitSelection();
					},
				});
				await viewer.load(previewBuffer);
				if (!cancelled) setStatus("ready");
			} catch (err) {
				if (cancelled) return;
				setError(err instanceof Error ? err.message : "XLSX 预览失败");
				setStatus("error");
			}
		}

		void loadViewer();

		return () => {
			cancelled = true;
			cancelAnimationFrame(selectionFrame);
			containerElement.removeEventListener("scroll", scheduleSelection, true);
			window.removeEventListener("resize", scheduleSelection);
			viewer?.destroy();
			selectionChangeRef.current?.(null);
			containerElement.replaceChildren();
		};
	}, [buffer, tracksTextSelection]);

	return (
		<div className="relative h-full min-h-[320px] overflow-hidden bg-white">
			<section
				ref={containerRef}
				aria-label={`${fileName} 预览`}
				className={`h-full w-full ${status === "error" ? "invisible" : ""}`}
			/>
			{status === "loading" && <PreviewStatus label="正在渲染 XLSX" />}
			{status === "error" && <PreviewError format="xlsx" message={error} />}
		</div>
	);
}

type ScrollDocumentRenderer = {
	count: number;
	render(
		canvas: HTMLCanvasElement,
		index: number,
		width: number,
		onTextRun: (run: OfficeTextRun) => void,
	): Promise<void>;
	destroy(): void;
};

type PreviewSurface = {
	element: HTMLDivElement;
	canvas: HTMLCanvasElement;
	textLayer: HTMLDivElement;
};

function copyArrayBuffer(buffer: ArrayBuffer): ArrayBuffer {
	return buffer.slice(0);
}

async function loadDocxDocument(buffer: ArrayBuffer): Promise<ScrollDocumentRenderer> {
	const { DocxDocument } = await import("@silurus/ooxml/docx");
	const document = await DocxDocument.load(buffer);

	return {
		count: document.pageCount,
		render: (canvas, index, width, onTextRun) =>
			document.renderPage(canvas, index, { width, onTextRun }),
		destroy: () => document.destroy(),
	};
}

async function loadPptxDocument(buffer: ArrayBuffer): Promise<ScrollDocumentRenderer> {
	const { PptxPresentation } = await import("@silurus/ooxml/pptx");
	const presentation = await PptxPresentation.load(buffer);

	return {
		count: presentation.slideCount,
		render: (canvas, index, width, onTextRun) =>
			presentation.renderSlide(canvas, index, { width, onTextRun }),
		destroy: () => presentation.destroy(),
	};
}

async function renderAllSurfaces({
	documentRenderer,
	fileName,
	format,
	hostElement,
}: {
	documentRenderer: ScrollDocumentRenderer;
	fileName: string;
	format: "docx" | "pptx";
	hostElement: HTMLElement;
}) {
	const renderWidth =
		format === "pptx" ? getPptxRenderWidth(hostElement) : getDocxRenderWidth(hostElement);
	const surfaces = Array.from({ length: documentRenderer.count }, (_, index) =>
		createPreviewSurface({ fileName, format, index }),
	);

	hostElement.replaceChildren(...surfaces.map((surface) => surface.element));

	for (const [index, surface] of surfaces.entries()) {
		const runs: OfficeTextRun[] = [];
		await documentRenderer.render(surface.canvas, index, renderWidth, (run) => runs.push(run));
		buildOfficeTextLayer({
			canvas: surface.canvas,
			format,
			surfaceIndex: index,
			textLayer: surface.textLayer,
			runs,
		});
		surface.element.style.width = surface.canvas.style.width || `${surface.canvas.width}px`;
		surface.element.style.height = surface.canvas.style.height || `${surface.canvas.height}px`;
		surface.canvas.style.visibility = "visible";
	}
}

function createPreviewSurface({
	fileName,
	format,
	index,
}: {
	fileName: string;
	format: "docx" | "pptx";
	index: number;
}): PreviewSurface {
	const element = document.createElement("div");
	element.dataset.officeSurfaceIndex = String(index);
	element.dataset.officeSurfaceKind = format === "docx" ? "page" : "slide";
	element.className = "relative inline-block max-w-full shrink-0 align-top";

	const canvas = createPreviewCanvas({ fileName, format, index });
	const textLayer = document.createElement("div");
	textLayer.setAttribute("aria-hidden", "true");
	textLayer.className = "absolute left-0 top-0 overflow-hidden select-text";
	textLayer.style.pointerEvents = "none";
	textLayer.style.userSelect = "text";
	textLayer.style.setProperty("-webkit-user-select", "text");

	element.append(canvas, textLayer);
	return { element, canvas, textLayer };
}

function createPreviewCanvas({
	fileName,
	format,
	index,
}: {
	fileName: string;
	format: "docx" | "pptx";
	index: number;
}) {
	const canvas = document.createElement("canvas");
	canvas.setAttribute(
		"aria-label",
		`${fileName} 第 ${index + 1} ${format === "docx" ? "页" : "张"}预览`,
	);
	canvas.className =
		format === "pptx"
			? "max-w-full rounded-sm bg-white shadow-[0_22px_70px_rgba(15,23,42,0.22)] ring-1 ring-black/10"
			: "max-w-full bg-white shadow-lg";
	canvas.style.visibility = "hidden";

	return canvas;
}

function createXlsxTextSelection({
	container,
	selection,
	sheetIndex,
	workbook,
	xlsx,
}: {
	container: HTMLElement;
	selection: CellRange;
	sheetIndex: number;
	workbook: WorkBook;
	xlsx: typeof import("xlsx");
}): OfficeTextSelection | null {
	const sheetName = workbook.SheetNames[sheetIndex];
	const worksheet = sheetName ? workbook.Sheets[sheetName] : undefined;
	if (!sheetName || !worksheet) return null;

	const usedRange = getXlsxUsedRange(worksheet["!ref"], xlsx);
	const range = normalizeXlsxSelectionRange(selection, usedRange);
	const text = buildXlsxSelectionText(range, (row, col) => {
		const address = xlsx.utils.encode_cell({ r: row - 1, c: col - 1 });
		const cell = worksheet[address];
		return cell ? String(cell.w ?? xlsx.utils.format_cell(cell)) : "";
	});
	const startCell = xlsx.utils.encode_cell({ r: range.startRow - 1, c: range.startCol - 1 });
	const endCell = xlsx.utils.encode_cell({ r: range.endRow - 1, c: range.endCol - 1 });
	const selectionBox = findXlsxSelectionBox(container);
	const viewportRect = selectionBox ? elementRect(selectionBox) : null;
	const selectionSurface = selectionBox?.parentElement ?? container;
	const surfaceRect = elementRect(selectionSurface);

	return {
		format: "xlsx",
		text,
		contextBefore: "",
		contextAfter: "",
		surfaceKind: "sheet",
		surfaceIndex: sheetIndex,
		boundingRect: viewportRect,
		rects: viewportRect ? [mapViewportRectToSurface(viewportRect, sheetIndex, surfaceRect)] : [],
		segments: [
			{
				format: "xlsx",
				surfaceIndex: sheetIndex,
				sheetName,
				startCell,
				endCell,
				mode: selection.mode,
			},
		],
	};
}

function getXlsxUsedRange(ref: string | undefined, xlsx: typeof import("xlsx")): XlsxOneBasedRange {
	if (!ref) return { startRow: 1, endRow: 1, startCol: 1, endCol: 1 };
	const range = xlsx.utils.decode_range(ref);
	return {
		startRow: range.s.r + 1,
		endRow: range.e.r + 1,
		startCol: range.s.c + 1,
		endCol: range.e.c + 1,
	};
}

function findXlsxSelectionBox(container: HTMLElement): HTMLElement | null {
	for (const element of container.querySelectorAll<HTMLElement>("div")) {
		if (element.style.zIndex !== "1" || element.style.pointerEvents !== "none") continue;
		const selectionBox = element.firstElementChild;
		if (selectionBox instanceof HTMLElement && selectionBox.style.borderWidth === "2px") {
			return selectionBox;
		}
	}
	return null;
}

function elementRect(element: Element): OfficeRect {
	const rect = element.getBoundingClientRect();
	return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
}

function getPptxRenderWidth(hostElement: HTMLElement): number {
	const viewportElement = hostElement.parentElement ?? hostElement;
	const availableWidth = Math.max(hostElement.clientWidth, viewportElement.clientWidth);
	const horizontalInset = availableWidth >= 768 ? 64 : 24;
	const widthCap = availableWidth >= 1120 ? PPTX_DESKTOP_MAX_WIDTH : PPTX_TABLET_MAX_WIDTH;
	const widthFromContainer = Math.max(PPTX_MIN_WIDTH, availableWidth - horizontalInset);

	return Math.round(Math.min(widthCap, widthFromContainer));
}

function getDocxRenderWidth(hostElement: HTMLElement): number {
	const viewportElement = hostElement.parentElement ?? hostElement;
	const availableWidth = Math.max(hostElement.clientWidth, viewportElement.clientWidth);
	const horizontalInset = availableWidth >= 768 ? 56 : 24;
	const widthCap = availableWidth >= 1120 ? DOCX_DESKTOP_MAX_WIDTH : DOCX_TABLET_MAX_WIDTH;
	const widthFromContainer = Math.max(DOCX_MIN_WIDTH, availableWidth - horizontalInset);

	return Math.round(Math.min(widthCap, widthFromContainer));
}

function PreviewStatus({ label }: { label: string }) {
	return (
		<div className="absolute inset-0 flex items-center justify-center text-sm text-[var(--lework-text-muted)]">
			<LoaderCircle className="mr-2 size-4 animate-spin" />
			{label}
		</div>
	);
}

function PreviewError({ format, message }: { format: OfficeOpenXmlFormat; message: string }) {
	return (
		<div className="absolute inset-0 flex items-center justify-center px-8 text-center text-sm text-[var(--lework-text-muted)]">
			<div>
				<p>无法加载 {format.toUpperCase()} 预览</p>
				<p className="mt-1 text-xs">{message}</p>
			</div>
		</div>
	);
}

export function getOfficeOpenXmlFormat(
	fileName: string,
	mimeType = "",
): OfficeOpenXmlFormat | null {
	const normalizedName = fileName.toLowerCase();
	const normalizedMimeType = mimeType.toLowerCase();

	if (
		normalizedName.endsWith(".docx") ||
		normalizedMimeType === "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	) {
		return "docx";
	}
	if (
		normalizedName.endsWith(".xlsx") ||
		normalizedMimeType === "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	) {
		return "xlsx";
	}
	if (
		normalizedName.endsWith(".pptx") ||
		normalizedMimeType ===
			"application/vnd.openxmlformats-officedocument.presentationml.presentation"
	) {
		return "pptx";
	}

	return null;
}
