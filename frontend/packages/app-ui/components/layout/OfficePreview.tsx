"use client";

import { LoaderCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";

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
}: {
	buffer: ArrayBuffer;
	fileName: string;
	format: OfficeOpenXmlFormat;
}) {
	if (format === "xlsx") {
		return <XlsxPreview buffer={buffer} fileName={fileName} />;
	}

	return <ScrollOfficePreview buffer={buffer} fileName={fileName} format={format} />;
}

function ScrollOfficePreview({
	buffer,
	fileName,
	format,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	format: "docx" | "pptx";
}) {
	const canvasHostRef = useRef<HTMLDivElement>(null);
	const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
	const [error, setError] = useState("");

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
		hostElement.replaceChildren();

		async function loadDocument() {
			try {
				documentRenderer =
					format === "docx"
						? await loadDocxDocument(sourceBuffer)
						: await loadPptxDocument(sourceBuffer);
				if (cancelled) return;

				await renderAllCanvases({
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
						void renderAllCanvases({
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
			<div className="relative min-h-0 flex-1 overflow-auto p-4">
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

function XlsxPreview({ buffer, fileName }: { buffer: ArrayBuffer; fileName: string }) {
	const containerRef = useRef<HTMLDivElement>(null);
	const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
	const [error, setError] = useState("");

	useEffect(() => {
		const container = containerRef.current;
		if (!container) return;
		const containerElement = container;
		const sourceBuffer = copyArrayBuffer(buffer);

		let cancelled = false;
		let viewer: { load(source: ArrayBuffer): Promise<void>; destroy(): void } | undefined;
		setStatus("loading");
		setError("");

		async function loadViewer() {
			try {
				const { XlsxViewer } = await import("@silurus/ooxml/xlsx");
				if (cancelled) return;

				viewer = new XlsxViewer(containerElement, {
					showZoomSlider: true,
				});
				await viewer.load(sourceBuffer);
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
			viewer?.destroy();
			containerElement.replaceChildren();
		};
	}, [buffer]);

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
	render(canvas: HTMLCanvasElement, index: number, width: number): Promise<void>;
	destroy(): void;
};

function copyArrayBuffer(buffer: ArrayBuffer): ArrayBuffer {
	return buffer.slice(0);
}

async function loadDocxDocument(buffer: ArrayBuffer): Promise<ScrollDocumentRenderer> {
	const { DocxDocument } = await import("@silurus/ooxml/docx");
	const document = await DocxDocument.load(buffer);

	return {
		count: document.pageCount,
		render: (canvas, index, width) => document.renderPage(canvas, index, { width }),
		destroy: () => document.destroy(),
	};
}

async function loadPptxDocument(buffer: ArrayBuffer): Promise<ScrollDocumentRenderer> {
	const { PptxPresentation } = await import("@silurus/ooxml/pptx");
	const presentation = await PptxPresentation.load(buffer);

	return {
		count: presentation.slideCount,
		render: (canvas, index, width) => presentation.renderSlide(canvas, index, { width }),
		destroy: () => presentation.destroy(),
	};
}

async function renderAllCanvases({
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
	const canvases = Array.from({ length: documentRenderer.count }, (_, index) =>
		createPreviewCanvas({ fileName, format, index }),
	);

	hostElement.replaceChildren(...canvases);

	for (const [index, canvas] of canvases.entries()) {
		await documentRenderer.render(canvas, index, renderWidth);
		canvas.style.visibility = "visible";
	}
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
		<div className="absolute inset-0 flex items-center justify-center text-sm text-[var(--leros-text-muted)]">
			<LoaderCircle className="mr-2 size-4 animate-spin" />
			{label}
		</div>
	);
}

function PreviewError({ format, message }: { format: OfficeOpenXmlFormat; message: string }) {
	return (
		<div className="absolute inset-0 flex items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
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
