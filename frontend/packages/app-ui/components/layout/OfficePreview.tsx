"use client";

import { Button } from "@leros/ui/components/ui/button";
import { ChevronLeft, ChevronRight, LoaderCircle } from "lucide-react";
import { useEffect, useRef, useState } from "react";

export type OfficeOpenXmlFormat = "docx" | "xlsx" | "pptx";

type NavigationState = {
	current: number;
	total: number;
};

const PPTX_DEFAULT_ASPECT_RATIO = 16 / 9;
const PPTX_DESKTOP_MAX_WIDTH = 1180;
const PPTX_TABLET_MAX_WIDTH = 960;
const PPTX_MIN_WIDTH = 320;

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

	return <PagedOfficePreview buffer={buffer} fileName={fileName} format={format} />;
}

function PagedOfficePreview({
	buffer,
	fileName,
	format,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	format: "docx" | "pptx";
}) {
	const canvasHostRef = useRef<HTMLDivElement>(null);
	const viewerRef = useRef<PagedViewer | null>(null);
	const [navigation, setNavigation] = useState<NavigationState>({ current: 0, total: 0 });
	const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
	const [error, setError] = useState("");

	useEffect(() => {
		const canvasHost = canvasHostRef.current;
		if (!canvasHost) return;
		const hostElement = canvasHost;
		const canvasElement = document.createElement("canvas");
		canvasElement.setAttribute("aria-label", `${fileName} 预览`);
		canvasElement.className =
			format === "pptx"
				? "max-w-full rounded-sm bg-white shadow-[0_22px_70px_rgba(15,23,42,0.22)] ring-1 ring-black/10"
				: "max-w-full bg-white shadow-lg";
		canvasElement.style.visibility = "hidden";
		if (format === "pptx") {
			setPptxCanvasWidth(canvasElement, getPptxRenderWidth(hostElement, canvasElement));
		}
		hostElement.replaceChildren(canvasElement);

		let cancelled = false;
		let resizeFrame = 0;
		let resizeObserver: ResizeObserver | undefined;
		setStatus("loading");
		setError("");
		setNavigation({ current: 0, total: 0 });

		async function loadViewer() {
			try {
				const viewer =
					format === "docx"
						? await createDocxViewer(canvasElement, (state) => {
								if (!cancelled) setNavigation(state);
							})
						: await createPptxViewer(
								canvasElement,
								(state) => {
									if (!cancelled) setNavigation(state);
								},
								getPptxRenderWidth(hostElement, canvasElement),
							);
				if (cancelled) {
					viewer.destroy();
					return;
				}

				viewerRef.current = viewer;
				await viewer.load(buffer);
				if (cancelled) return;

				if (format === "pptx") {
					const renderWidth = getPptxRenderWidth(hostElement, canvasElement);
					viewer.setViewportWidth?.(renderWidth);
					await viewer.renderCurrent();
					if (cancelled) return;
				}

				canvasElement.style.visibility = "visible";
				setStatus("ready");
				resizeObserver = new ResizeObserver(() => {
					cancelAnimationFrame(resizeFrame);
					resizeFrame = requestAnimationFrame(() => {
						if (format === "pptx") {
							viewer.setViewportWidth?.(getPptxRenderWidth(hostElement, canvasElement));
						}
						void viewer.renderCurrent().catch(handleRenderError);
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

		void loadViewer();

		return () => {
			cancelled = true;
			cancelAnimationFrame(resizeFrame);
			resizeObserver?.disconnect();
			viewerRef.current?.destroy();
			viewerRef.current = null;
			hostElement.replaceChildren();
		};
	}, [buffer, fileName, format]);

	const navigate = async (direction: "previous" | "next") => {
		const viewer = viewerRef.current;
		if (!viewer) return;
		try {
			await (direction === "previous" ? viewer.previous() : viewer.next());
		} catch (err) {
			setError(err instanceof Error ? err.message : "页面切换失败");
			setStatus("error");
		}
	};

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
							? "flex min-h-full items-center justify-center py-8"
							: "flex min-h-full items-start justify-center"
					}
				/>
				{status === "loading" && <PreviewStatus label={`正在渲染 ${format.toUpperCase()}`} />}
				{status === "error" && <PreviewError format={format} message={error} />}
			</div>

			{status === "ready" && navigation.total > 0 && (
				<div
					className={
						format === "pptx"
							? "absolute bottom-4 left-1/2 flex -translate-x-1/2 items-center justify-center gap-3 rounded-full border border-[var(--leros-control-border)] bg-white/90 px-3 py-1.5 shadow-lg backdrop-blur"
							: "flex shrink-0 items-center justify-center gap-3 border-t border-[var(--leros-control-border)] bg-white px-4 py-2"
					}
				>
					<Button
						type="button"
						variant="ghost"
						size="icon-sm"
						disabled={navigation.current <= 0}
						onClick={() => void navigate("previous")}
						title={format === "docx" ? "上一页" : "上一张"}
					>
						<ChevronLeft className="size-4" />
					</Button>
					<span className="min-w-20 text-center text-xs tabular-nums text-[var(--leros-text-muted)]">
						{navigation.current + 1} / {navigation.total}
					</span>
					<Button
						type="button"
						variant="ghost"
						size="icon-sm"
						disabled={navigation.current >= navigation.total - 1}
						onClick={() => void navigate("next")}
						title={format === "docx" ? "下一页" : "下一张"}
					>
						<ChevronRight className="size-4" />
					</Button>
				</div>
			)}
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
				await viewer.load(buffer);
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

type PagedViewer = {
	load(source: ArrayBuffer): Promise<void>;
	previous(): Promise<void>;
	next(): Promise<void>;
	renderCurrent(): Promise<void>;
	setViewportWidth?(width: number): void;
	destroy(): void;
};

async function createDocxViewer(
	canvas: HTMLCanvasElement,
	onChange: (state: NavigationState) => void,
): Promise<PagedViewer> {
	const { DocxViewer } = await import("@silurus/ooxml/docx");
	const viewer = new DocxViewer(canvas, {
		enableTextSelection: true,
		onPageChange: (current, total) => onChange({ current, total }),
	});

	return {
		load: (source) => viewer.load(source),
		previous: () => viewer.prevPage(),
		next: () => viewer.nextPage(),
		renderCurrent: () => viewer.goToPage(viewer.currentPage),
		destroy: () => viewer.destroy(),
	};
}

async function createPptxViewer(
	canvas: HTMLCanvasElement,
	onChange: (state: NavigationState) => void,
	initialWidth: number,
): Promise<PagedViewer> {
	const { PptxViewer } = await import("@silurus/ooxml/pptx");
	const viewerOptions = {
		enableTextSelection: true,
		onSlideChange: (current: number, total: number) => onChange({ current, total }),
		width: initialWidth,
	};
	const viewer = new PptxViewer(canvas, viewerOptions);

	return {
		load: (source) => viewer.load(source),
		previous: () => viewer.prevSlide(),
		next: () => viewer.nextSlide(),
		renderCurrent: () => viewer.goToSlide(viewer.slideIndex),
		setViewportWidth: (width) => {
			viewerOptions.width = width;
			setPptxCanvasWidth(canvas, width);
		},
		destroy: () => viewer.destroy(),
	};
}

function getPptxRenderWidth(hostElement: HTMLElement, canvasElement: HTMLCanvasElement): number {
	const viewportElement = hostElement.parentElement ?? hostElement;
	const availableWidth = Math.max(hostElement.clientWidth, viewportElement.clientWidth);
	const availableHeight = Math.max(hostElement.clientHeight, viewportElement.clientHeight);
	const aspectRatio = getCanvasAspectRatio(canvasElement);
	const horizontalInset = availableWidth >= 768 ? 64 : 24;
	const verticalInset = availableHeight >= 560 ? 96 : 40;
	const widthCap = availableWidth >= 1120 ? PPTX_DESKTOP_MAX_WIDTH : PPTX_TABLET_MAX_WIDTH;
	const widthFromContainer = Math.max(PPTX_MIN_WIDTH, availableWidth - horizontalInset);
	const widthFromHeight = Math.max(PPTX_MIN_WIDTH, (availableHeight - verticalInset) * aspectRatio);

	return Math.round(Math.min(widthCap, widthFromContainer, widthFromHeight));
}

function setPptxCanvasWidth(canvasElement: HTMLCanvasElement, width: number): void {
	canvasElement.style.width = `${Math.round(width)}px`;
	canvasElement.style.height = "auto";
}

function getCanvasAspectRatio(canvasElement: HTMLCanvasElement): number {
	const width = canvasElement.offsetWidth;
	const height = canvasElement.offsetHeight;

	if (width > 0 && height > 0) {
		return width / height;
	}

	return PPTX_DEFAULT_ASPECT_RATIO;
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
