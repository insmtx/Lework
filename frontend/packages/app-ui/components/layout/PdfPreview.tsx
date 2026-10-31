"use client";

import {
	Check,
	ChevronLeft,
	ChevronRight,
	LoaderCircle,
	Maximize2,
	MoveHorizontal,
	RefreshCw,
	ZoomIn,
	ZoomOut,
} from "lucide-react";
import type { PageViewport, PDFDocumentLoadingTask, PDFPageProxy, RenderTask } from "pdfjs-dist";
import {
	type CSSProperties,
	useCallback,
	useEffect,
	useLayoutEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import {
	clearPdfBrowserSelection,
	observePdfTextSelection,
	type PdfPageViewport,
	type PdfSelectionRect,
	type PdfSourceIdentity,
	type PdfTextSelection,
} from "./pdf-selection";

export type { PdfTextSelection } from "./pdf-selection";

const PDF_FALLBACK_WIDTH = 820;
const PDF_PAGE_GAP = 24;
const PDF_VIEWPORT_PADDING = 32;
const MIN_CUSTOM_SCALE = 0.5;
const MAX_SCALE = 3;
const ZOOM_STEPS = [0.5, 0.67, 0.75, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3];

type PdfJsModule = typeof import("pdfjs-dist/legacy/build/pdf.mjs");
type ViewMode = "fit-width" | "fit-page" | "custom";
type DocumentStatus = "loading" | "ready" | "error";
type PageRenderStatus = "idle" | "loading" | "ready" | "error";

type PdfPageEntry = {
	page: PDFPageProxy | null;
	baseWidth: number;
	baseHeight: number;
};

type PageLayout = {
	width: number;
	height: number;
	viewport: PageViewport | null;
};

type ScrollAnchor = {
	pageIndex: number;
	yRatio: number;
};

export function PdfPreview({
	buffer,
	fileName,
	onTextSelectionChange,
}: {
	buffer: ArrayBuffer;
	fileName: string;
	onTextSelectionChange?: (selection: PdfTextSelection | null) => void;
}) {
	const viewportRef = useRef<HTMLElement>(null);
	const hostRef = useRef<HTMLDivElement>(null);
	const pageViewportsRef = useRef<Map<number, PdfPageViewport>>(new Map());
	const sourceIdentityRef = useRef<PdfSourceIdentity>(emptySourceIdentity());
	const selectionChangeRef = useRef(onTextSelectionChange);
	const lastSelectionRef = useRef<PdfTextSelection | null>(null);
	const viewChangingRef = useRef(false);
	const currentPageRef = useRef(1);
	const pendingScrollAnchorRef = useRef<ScrollAnchor | null>(null);
	const copyTimerRef = useRef(0);
	const [status, setStatus] = useState<DocumentStatus>("loading");
	const [error, setError] = useState("");
	const [loadVersion, setLoadVersion] = useState(0);
	const [pages, setPages] = useState<PdfPageEntry[]>([]);
	const [visiblePages, setVisiblePages] = useState<Set<number>>(() => new Set([0]));
	const [renderedPages, setRenderedPages] = useState<Set<number>>(() => new Set());
	const [pageTextAvailability, setPageTextAvailability] = useState<Record<number, boolean>>({});
	const [currentPage, setCurrentPage] = useState(1);
	const [pageInput, setPageInput] = useState("1");
	const [viewMode, setViewMode] = useState<ViewMode>("fit-width");
	const [customScale, setCustomScale] = useState(1);
	const [viewportSize, setViewportSize] = useState({ width: 0, height: 0 });
	const [persistedSelection, setPersistedSelection] = useState<PdfTextSelection | null>(null);
	const [copyVisible, setCopyVisible] = useState(false);
	const tracksTextSelection = Boolean(onTextSelectionChange);

	useEffect(() => {
		selectionChangeRef.current = onTextSelectionChange;
	}, [onTextSelectionChange]);

	useEffect(() => {
		currentPageRef.current = currentPage;
		setPageInput(String(currentPage));
	}, [currentPage]);

	const captureScrollAnchor = useCallback(() => {
		const viewport = viewportRef.current;
		const host = hostRef.current;
		if (!viewport || !host) return;
		const pageIndex = Math.max(0, currentPageRef.current - 1);
		const page = host.querySelector<HTMLElement>(`[data-pdf-page-index="${pageIndex}"]`);
		if (!page || page.offsetHeight <= 0) return;
		const viewportCenter = viewport.scrollTop + viewport.clientHeight / 2;
		pendingScrollAnchorRef.current = {
			pageIndex,
			yRatio: clamp((viewportCenter - page.offsetTop) / page.offsetHeight, 0, 1),
		};
	}, []);

	const prepareViewChange = useCallback(() => {
		captureScrollAnchor();
		const host = hostRef.current;
		if (!host) return;
		viewChangingRef.current = true;
		clearPdfBrowserSelection(host);
		if (lastSelectionRef.current) setPersistedSelection(lastSelectionRef.current);
		requestAnimationFrame(() => {
			viewChangingRef.current = false;
		});
	}, [captureScrollAnchor]);

	useEffect(() => {
		const host = hostRef.current;
		if (!host || !tracksTextSelection) return;
		return observePdfTextSelection({
			host,
			getSourceIdentity: () => sourceIdentityRef.current,
			getPageViewports: () => pageViewportsRef.current,
			onChange: (selection) => {
				if (!selection && viewChangingRef.current) return;
				lastSelectionRef.current = selection;
				setPersistedSelection(null);
				selectionChangeRef.current?.(selection);
			},
			onCopy: () => {
				window.clearTimeout(copyTimerRef.current);
				setCopyVisible(true);
				copyTimerRef.current = window.setTimeout(() => setCopyVisible(false), 1400);
			},
		});
	}, [tracksTextSelection]);

	useEffect(() => () => window.clearTimeout(copyTimerRef.current), []);

	useEffect(() => {
		const viewport = viewportRef.current;
		if (!viewport) return;
		let previousWidth = viewport.clientWidth;
		let previousHeight = viewport.clientHeight;
		setViewportSize({ width: previousWidth, height: previousHeight });

		const observer = new ResizeObserver(() => {
			const width = viewport.clientWidth;
			const height = viewport.clientHeight;
			if (Math.abs(width - previousWidth) < 1 && Math.abs(height - previousHeight) < 1) return;
			if (viewMode !== "custom") prepareViewChange();
			previousWidth = width;
			previousHeight = height;
			setViewportSize({ width, height });
		});
		observer.observe(viewport);
		return () => observer.disconnect();
	}, [prepareViewChange, viewMode]);

	useEffect(() => {
		const pdfBuffer = buffer.slice(0);
		const hashBuffer = buffer.slice(0);
		let loadingTask: PDFDocumentLoadingTask | null = null;
		let cancelled = false;

		setStatus("loading");
		setError("");
		setPages([]);
		setCurrentPage(1);
		currentPageRef.current = 1;
		setViewMode("fit-width");
		setCustomScale(1);
		setVisiblePages(new Set([0]));
		setRenderedPages(new Set());
		setPageTextAvailability({});
		setPersistedSelection(null);
		lastSelectionRef.current = null;
		sourceIdentityRef.current = emptySourceIdentity();
		pageViewportsRef.current.clear();
		if (viewportRef.current) viewportRef.current.scrollTop = 0;

		async function loadDocument() {
			try {
				const pdfJs = await loadPdfJs();
				if (cancelled) return;
				loadingTask = pdfJs.getDocument({ data: pdfBuffer });
				const [loadedDocument, contentHash] = await Promise.all([
					loadingTask.promise,
					sha256Hex(hashBuffer),
				]);
				if (cancelled) return;

				sourceIdentityRef.current = {
					contentHash,
					documentFingerprint:
						loadedDocument.fingerprints
							.filter((value): value is string => Boolean(value))
							.join(":") || null,
				};

				const firstPage = await loadedDocument.getPage(1);
				if (cancelled) return;
				const firstViewport = firstPage.getViewport({ scale: 1 });
				const initialPages = Array.from({ length: loadedDocument.numPages }, (_, index) => ({
					page: index === 0 ? firstPage : null,
					baseWidth: firstViewport.width,
					baseHeight: firstViewport.height,
				}));
				setPages(initialPages);
				setStatus("ready");

				const remainingPages = await Promise.all(
					Array.from({ length: loadedDocument.numPages - 1 }, async (_, index) => {
						try {
							return await loadedDocument.getPage(index + 2);
						} catch {
							return null;
						}
					}),
				);
				if (cancelled) return;
				setPages((current) =>
					current.map((entry, index) => {
						const page = index === 0 ? firstPage : (remainingPages[index - 1] ?? null);
						if (!page) return entry;
						const pageViewport = page.getViewport({ scale: 1 });
						return { page, baseWidth: pageViewport.width, baseHeight: pageViewport.height };
					}),
				);
			} catch (err) {
				if (cancelled) return;
				setError(formatPdfError(err));
				setStatus("error");
			}
		}

		void loadDocument();
		return () => {
			cancelled = true;
			pageViewportsRef.current.clear();
			void loadingTask?.destroy();
		};
	}, [buffer, fileName, loadVersion]);

	const referenceDimensions = useMemo(() => {
		const first = pages[0];
		if (!first) return { width: 612, height: 792 };
		return { width: first.baseWidth, height: first.baseHeight };
	}, [pages]);

	const effectiveScale = useMemo(() => {
		if (viewMode === "custom") return clamp(customScale, MIN_CUSTOM_SCALE, MAX_SCALE);
		const availableWidth = Math.max(
			240,
			(viewportSize.width || PDF_FALLBACK_WIDTH) - PDF_VIEWPORT_PADDING,
		);
		const widthScale = availableWidth / referenceDimensions.width;
		if (viewMode === "fit-width") return clamp(widthScale, 0.25, MAX_SCALE);
		const availableHeight = Math.max(240, viewportSize.height - PDF_VIEWPORT_PADDING);
		return clamp(
			Math.min(widthScale, availableHeight / referenceDimensions.height),
			0.25,
			MAX_SCALE,
		);
	}, [customScale, referenceDimensions, viewMode, viewportSize]);

	const pageLayouts = useMemo<PageLayout[]>(
		() =>
			pages.map((entry) => {
				if (entry.page) {
					const viewport = entry.page.getViewport({ scale: effectiveScale });
					return { width: viewport.width, height: viewport.height, viewport };
				}
				return {
					width: entry.baseWidth * effectiveScale,
					height: entry.baseHeight * effectiveScale,
					viewport: null,
				};
			}),
		[effectiveScale, pages],
	);

	useLayoutEffect(() => {
		pageViewportsRef.current.clear();
		for (const [pageIndex, layout] of pageLayouts.entries()) {
			if (layout.viewport) pageViewportsRef.current.set(pageIndex, layout.viewport);
		}
	}, [pageLayouts]);

	useEffect(() => {
		setRenderedPages(new Set());
	}, [effectiveScale]);

	useLayoutEffect(() => {
		const anchor = pendingScrollAnchorRef.current;
		const viewport = viewportRef.current;
		const host = hostRef.current;
		if (!anchor || !viewport || !host) return;
		pendingScrollAnchorRef.current = null;
		requestAnimationFrame(() => {
			const page = host.querySelector<HTMLElement>(`[data-pdf-page-index="${anchor.pageIndex}"]`);
			if (!page) return;
			viewport.scrollTop =
				page.offsetTop + page.offsetHeight * anchor.yRatio - viewport.clientHeight / 2;
		});
	}, [effectiveScale]);

	useEffect(() => {
		const viewport = viewportRef.current;
		const host = hostRef.current;
		if (!viewport || !host || pages.length === 0) return;
		const surfaces = Array.from(host.querySelectorAll<HTMLElement>("[data-pdf-page-index]"));
		if (typeof IntersectionObserver === "undefined") {
			setVisiblePages(new Set(surfaces.map((surface) => Number(surface.dataset.pdfPageIndex))));
			return;
		}

		const observer = new IntersectionObserver(
			(entries) => {
				const entering = entries
					.filter((entry) => entry.isIntersecting)
					.map((entry) => Number((entry.target as HTMLElement).dataset.pdfPageIndex))
					.filter(Number.isInteger);
				if (entering.length === 0) return;
				setVisiblePages((current) => new Set([...current, ...entering]));
			},
			{ root: viewport, rootMargin: "1200px 0px", threshold: 0.01 },
		);
		for (const surface of surfaces) observer.observe(surface);
		return () => observer.disconnect();
	}, [pages.length]);

	useEffect(() => {
		const viewport = viewportRef.current;
		const host = hostRef.current;
		if (!viewport || !host || pages.length === 0) return;
		let frame = 0;
		const updateCurrentPage = () => {
			frame = 0;
			const viewportRect = viewport.getBoundingClientRect();
			let bestPage = currentPageRef.current;
			let bestVisibleHeight = -1;
			for (const surface of host.querySelectorAll<HTMLElement>("[data-pdf-page-index]")) {
				const rect = surface.getBoundingClientRect();
				const visibleHeight = Math.max(
					0,
					Math.min(rect.bottom, viewportRect.bottom) - Math.max(rect.top, viewportRect.top),
				);
				if (visibleHeight > bestVisibleHeight) {
					bestVisibleHeight = visibleHeight;
					bestPage = Number(surface.dataset.pdfPageIndex) + 1;
				}
			}
			if (Number.isInteger(bestPage) && bestPage !== currentPageRef.current) {
				currentPageRef.current = bestPage;
				setCurrentPage(bestPage);
			}
		};
		const schedule = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(updateCurrentPage);
		};
		viewport.addEventListener("scroll", schedule, { passive: true });
		window.addEventListener("resize", schedule);
		schedule();
		return () => {
			cancelAnimationFrame(frame);
			viewport.removeEventListener("scroll", schedule);
			window.removeEventListener("resize", schedule);
		};
	}, [pages.length]);

	const jumpToPage = useCallback(
		(pageNumber: number, behavior: ScrollBehavior = "smooth") => {
			const targetPage = clamp(Math.round(pageNumber), 1, Math.max(1, pages.length));
			const surface = hostRef.current?.querySelector<HTMLElement>(
				`[data-pdf-page-index="${targetPage - 1}"]`,
			);
			if (!surface) return;
			setVisiblePages(
				(current) =>
					new Set(
						[...current, targetPage - 2, targetPage - 1, targetPage].filter(
							(index) => index >= 0 && index < pages.length,
						),
					),
			);
			surface.scrollIntoView({ behavior, block: "start" });
			setCurrentPage(targetPage);
		},
		[pages.length],
	);

	const applyCustomScale = useCallback(
		(scale: number) => {
			prepareViewChange();
			setViewMode("custom");
			setCustomScale(clamp(scale, MIN_CUSTOM_SCALE, MAX_SCALE));
		},
		[prepareViewChange],
	);

	const zoomByStep = useCallback(
		(direction: -1 | 1) => {
			const current = effectiveScale;
			const next =
				direction > 0
					? (ZOOM_STEPS.find((step) => step > current + 0.01) ?? MAX_SCALE)
					: ([...ZOOM_STEPS].reverse().find((step) => step < current - 0.01) ?? MIN_CUSTOM_SCALE);
			applyCustomScale(next);
		},
		[applyCustomScale, effectiveScale],
	);

	const applyViewMode = useCallback(
		(mode: ViewMode) => {
			prepareViewChange();
			setViewMode(mode);
		},
		[prepareViewChange],
	);

	useEffect(() => {
		const viewport = viewportRef.current;
		if (!viewport) return;
		const onWheel = (event: WheelEvent) => {
			if (!event.ctrlKey && !event.metaKey) return;
			event.preventDefault();
			zoomByStep(event.deltaY < 0 ? 1 : -1);
		};
		const onKeyDown = (event: KeyboardEvent) => {
			const target = event.target;
			if (
				target instanceof HTMLInputElement ||
				target instanceof HTMLTextAreaElement ||
				(target instanceof HTMLElement && target.isContentEditable)
			) {
				return;
			}
			if (event.key === "PageDown") {
				event.preventDefault();
				jumpToPage(currentPageRef.current + 1);
				return;
			}
			if (event.key === "PageUp") {
				event.preventDefault();
				jumpToPage(currentPageRef.current - 1);
				return;
			}
			if (!event.ctrlKey && !event.metaKey) return;
			if (event.key === "+" || event.key === "=") {
				event.preventDefault();
				zoomByStep(1);
			} else if (event.key === "-") {
				event.preventDefault();
				zoomByStep(-1);
			} else if (event.key === "0") {
				event.preventDefault();
				applyCustomScale(1);
			}
		};
		viewport.addEventListener("wheel", onWheel, { passive: false });
		viewport.addEventListener("keydown", onKeyDown);
		return () => {
			viewport.removeEventListener("wheel", onWheel);
			viewport.removeEventListener("keydown", onKeyDown);
		};
	}, [applyCustomScale, jumpToPage, zoomByStep]);

	const handlePageRendered = useCallback((pageIndex: number, hasText: boolean) => {
		setRenderedPages((current) => new Set(current).add(pageIndex));
		setPageTextAvailability((current) =>
			current[pageIndex] === hasText ? current : { ...current, [pageIndex]: hasText },
		);
	}, []);

	const currentPageHasNoText = pageTextAvailability[currentPage - 1] === false;
	const isRendering = renderedPages.size < Math.min(visiblePages.size, pages.length);

	return (
		<div className="relative flex h-full min-h-[320px] flex-col overflow-hidden bg-[#e7eaf0]">
			<PdfToolbar
				currentPage={currentPage}
				isRendering={isRendering}
				noText={currentPageHasNoText}
				pageCount={pages.length}
				pageInput={pageInput}
				onFitPage={() => applyViewMode("fit-page")}
				onFitWidth={() => applyViewMode("fit-width")}
				onNextPage={() => jumpToPage(currentPage + 1)}
				onPageInputChange={setPageInput}
				onPageInputCommit={() => {
					const requestedPage = Number(pageInput);
					if (Number.isFinite(requestedPage)) jumpToPage(requestedPage);
					else setPageInput(String(currentPage));
				}}
				onPreviousPage={() => jumpToPage(currentPage - 1)}
				onZoomIn={() => zoomByStep(1)}
				onZoomOut={() => zoomByStep(-1)}
				viewMode={viewMode}
				zoomPercent={Math.round(effectiveScale * 100)}
			/>

			<section
				ref={viewportRef}
				aria-label="PDF 阅读区"
				tabIndex={-1}
				onPointerDown={(event) => event.currentTarget.focus({ preventScroll: true })}
				className="relative min-h-0 flex-1 overflow-auto px-4 py-5 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-[var(--lework-primary)]/30"
			>
				<div
					ref={hostRef}
					className="mx-auto flex min-h-full w-max min-w-full flex-col items-center"
					style={{ gap: PDF_PAGE_GAP }}
				>
					{status === "ready"
						? pages.map((entry, pageIndex) => {
								const layout = pageLayouts[pageIndex];
								if (!layout) return null;
								return (
									<PdfPageView
										key={pageIndex}
										entry={entry}
										fileName={fileName}
										highlightRects={
											persistedSelection?.rects.filter((rect) => rect.pageIndex === pageIndex) ?? []
										}
										layout={layout}
										onRendered={handlePageRendered}
										pageIndex={pageIndex}
										shouldRender={visiblePages.has(pageIndex)}
									/>
								);
							})
						: null}
				</div>
				{status === "loading" ? <DocumentLoadingState /> : null}
				{status === "error" ? (
					<DocumentErrorState
						message={error}
						onRetry={() => setLoadVersion((value) => value + 1)}
					/>
				) : null}
			</section>

			{copyVisible ? (
				<div className="pointer-events-none absolute bottom-5 left-1/2 z-30 flex -translate-x-1/2 items-center gap-2 rounded-md bg-slate-950 px-3 py-2 text-xs font-medium text-white shadow-lg">
					<Check className="size-3.5 text-emerald-400" />
					已复制
				</div>
			) : null}
			<style>{PDF_TEXT_LAYER_STYLES}</style>
		</div>
	);
}

function PdfToolbar({
	currentPage,
	isRendering,
	noText,
	pageCount,
	pageInput,
	onFitPage,
	onFitWidth,
	onNextPage,
	onPageInputChange,
	onPageInputCommit,
	onPreviousPage,
	onZoomIn,
	onZoomOut,
	viewMode,
	zoomPercent,
}: {
	currentPage: number;
	isRendering: boolean;
	noText: boolean;
	pageCount: number;
	pageInput: string;
	onFitPage: () => void;
	onFitWidth: () => void;
	onNextPage: () => void;
	onPageInputChange: (value: string) => void;
	onPageInputCommit: () => void;
	onPreviousPage: () => void;
	onZoomIn: () => void;
	onZoomOut: () => void;
	viewMode: ViewMode;
	zoomPercent: number;
}) {
	return (
		<div
			role="toolbar"
			aria-label="PDF 工具栏"
			className="flex h-10 shrink-0 items-center justify-between gap-2 border-b border-slate-200/80 bg-white px-2.5 text-slate-600"
		>
			<div className="flex min-w-0 items-center gap-0.5">
				<ToolbarIconButton
					disabled={currentPage <= 1 || pageCount === 0}
					label="上一页"
					onClick={onPreviousPage}
				>
					<ChevronLeft className="size-4" />
				</ToolbarIconButton>
				<div className="mx-0.5 flex h-7 items-center rounded-md bg-slate-100/80 text-xs tabular-nums">
					<input
						aria-label="当前页码"
						inputMode="numeric"
						value={pageInput}
						onBlur={onPageInputCommit}
						onChange={(event) => onPageInputChange(event.target.value.replace(/\D/g, ""))}
						onKeyDown={(event) => {
							if (event.key === "Enter") {
								onPageInputCommit();
								event.currentTarget.blur();
							}
						}}
						className="h-7 w-8 rounded-l-md bg-transparent px-1 text-center text-xs font-medium text-slate-800 outline-none focus:bg-white focus:ring-1 focus:ring-inset focus:ring-slate-300"
					/>
					<span className="pr-2 text-slate-400">/ {pageCount || "-"}</span>
				</div>
				<ToolbarIconButton
					disabled={currentPage >= pageCount || pageCount === 0}
					label="下一页"
					onClick={onNextPage}
				>
					<ChevronRight className="size-4" />
				</ToolbarIconButton>
			</div>

			<div className="flex shrink-0 items-center gap-1.5">
				{isRendering ? <LoaderCircle className="size-3.5 animate-spin text-slate-400" /> : null}
				{noText ? (
					<span className="hidden text-[11px] text-amber-700 md:inline">当前页无可选文字</span>
				) : null}
				<div className="flex h-8 items-center rounded-md bg-slate-100/80 p-0.5">
					<ToolbarIconButton label="缩小" onClick={onZoomOut}>
						<ZoomOut className="size-4" />
					</ToolbarIconButton>
					<span className="w-11 text-center text-xs tabular-nums text-slate-600">
						{zoomPercent}%
					</span>
					<ToolbarIconButton label="放大" onClick={onZoomIn}>
						<ZoomIn className="size-4" />
					</ToolbarIconButton>
				</div>
				<div className="flex h-8 items-center rounded-md bg-slate-100/80 p-0.5">
					<ToolbarIconButton
						active={viewMode === "fit-width"}
						label="适应宽度"
						onClick={onFitWidth}
					>
						<MoveHorizontal className="size-4" />
					</ToolbarIconButton>
					<ToolbarIconButton active={viewMode === "fit-page"} label="适应整页" onClick={onFitPage}>
						<Maximize2 className="size-4" />
					</ToolbarIconButton>
				</div>
			</div>
		</div>
	);
}

function ToolbarIconButton({
	active = false,
	children,
	disabled = false,
	label,
	onClick,
}: {
	active?: boolean;
	children: React.ReactNode;
	disabled?: boolean;
	label: string;
	onClick: () => void;
}) {
	return (
		<button
			type="button"
			aria-label={label}
			title={label}
			disabled={disabled}
			onClick={onClick}
			className={`flex size-7 items-center justify-center rounded text-slate-500 transition-colors hover:bg-white hover:text-slate-900 hover:shadow-sm disabled:cursor-not-allowed disabled:opacity-35 disabled:shadow-none ${
				active ? "bg-white text-slate-900 shadow-sm ring-1 ring-slate-200/80" : ""
			}`}
		>
			{children}
		</button>
	);
}

function PdfPageView({
	entry,
	fileName,
	highlightRects,
	layout,
	onRendered,
	pageIndex,
	shouldRender,
}: {
	entry: PdfPageEntry;
	fileName: string;
	highlightRects: PdfSelectionRect[];
	layout: PageLayout;
	onRendered: (pageIndex: number, hasText: boolean) => void;
	pageIndex: number;
	shouldRender: boolean;
}) {
	const canvasRef = useRef<HTMLCanvasElement>(null);
	const textLayerRef = useRef<HTMLDivElement>(null);
	const [renderStatus, setRenderStatus] = useState<PageRenderStatus>("idle");
	const [renderError, setRenderError] = useState("");
	const [retryVersion, setRetryVersion] = useState(0);

	useEffect(() => {
		const page = entry.page;
		const viewport = layout.viewport;
		const canvas = canvasRef.current;
		const textLayerElement = textLayerRef.current;
		if (!shouldRender || !page || !viewport || !canvas || !textLayerElement) return;
		const pageProxy = page;
		const pageViewport = viewport;
		const canvasElement = canvas;
		const layerElement = textLayerElement;

		let cancelled = false;
		let renderTask: RenderTask | null = null;
		let textLayer: { cancel(): void; render(): Promise<unknown>; textDivs: HTMLElement[] } | null =
			null;
		setRenderStatus("loading");
		setRenderError("");
		layerElement.replaceChildren();

		async function renderPage() {
			try {
				const pdfJs = await loadPdfJs();
				if (cancelled) return;
				const outputScale = Math.max(1, window.devicePixelRatio || 1);
				canvasElement.width = Math.max(1, Math.floor(pageViewport.width * outputScale));
				canvasElement.height = Math.max(1, Math.floor(pageViewport.height * outputScale));
				canvasElement.style.width = `${pageViewport.width}px`;
				canvasElement.style.height = `${pageViewport.height}px`;
				renderTask = pageProxy.render({
					canvas: canvasElement,
					viewport: pageViewport,
					transform: outputScale === 1 ? undefined : [outputScale, 0, 0, outputScale, 0, 0],
				});
				const textContentPromise = pageProxy.getTextContent();
				await renderTask.promise;
				const textContent = await textContentPromise;
				if (cancelled) return;

				configurePdfTextLayer(layerElement, pageViewport);
				textLayer = new pdfJs.TextLayer({
					container: layerElement,
					textContentSource: textContent,
					viewport: pageViewport,
				});
				await textLayer.render();
				if (cancelled) return;
				configurePdfTextLayer(layerElement, pageViewport);
				for (const [itemIndex, textDiv] of textLayer.textDivs.entries()) {
					const item = textContent.items[itemIndex];
					configurePdfTextItem({
						hasEOL: Boolean(item && "hasEOL" in item && item.hasEOL),
						itemIndex,
						pageIndex,
						textDiv,
					});
				}
				const hasText = textContent.items.some(
					(item) => "str" in item && item.str.trim().length > 0,
				);
				setRenderStatus("ready");
				onRendered(pageIndex, hasText);
			} catch (err) {
				if (cancelled || isRenderingCancelled(err)) return;
				setRenderError(err instanceof Error ? err.message : "页面渲染失败");
				setRenderStatus("error");
			}
		}

		void renderPage();
		return () => {
			cancelled = true;
			renderTask?.cancel();
			textLayer?.cancel();
			layerElement.replaceChildren();
		};
	}, [entry.page, layout.viewport, onRendered, pageIndex, retryVersion, shouldRender]);

	const highlightStyles = useMemo(
		() =>
			layout.viewport
				? highlightRects.map((rect) =>
						mapPdfQuadToViewportRect(rect, layout.viewport as PageViewport),
					)
				: [],
		[highlightRects, layout.viewport],
	);

	return (
		<section
			data-pdf-page-index={pageIndex}
			data-pdf-page-number={pageIndex + 1}
			className="relative shrink-0 overflow-hidden bg-white shadow-[0_2px_10px_rgba(15,23,42,0.13)] ring-1 ring-black/5"
			style={{ width: layout.width, height: layout.height, scrollMarginTop: 20 }}
		>
			<canvas
				ref={canvasRef}
				role="img"
				aria-label={`${fileName} 第 ${pageIndex + 1} 页`}
				className="absolute inset-0 block"
			/>
			{highlightStyles.length > 0 ? (
				<div className="pointer-events-none absolute inset-0 z-[1]" aria-hidden="true">
					{highlightStyles.map((style, index) => (
						<div
							key={`${pageIndex}-${index}`}
							className="absolute bg-sky-300/35 ring-1 ring-inset ring-sky-400/20"
							style={style}
						/>
					))}
				</div>
			) : null}
			<div
				ref={textLayerRef}
				className="pdf-text-layer absolute inset-0 z-[2]"
				aria-hidden="true"
			/>
			{renderStatus !== "ready" ? (
				<div className="absolute inset-0 z-[3] flex items-center justify-center bg-white">
					{renderStatus === "error" ? (
						<div className="px-8 text-center text-xs text-slate-500">
							<p>第 {pageIndex + 1} 页渲染失败</p>
							<p className="mt-1 line-clamp-2 text-[11px] text-slate-400">{renderError}</p>
							<button
								type="button"
								onClick={() => setRetryVersion((value) => value + 1)}
								className="mt-3 inline-flex items-center gap-1 rounded px-2 py-1 text-[11px] font-medium text-[var(--lework-primary)] hover:bg-[var(--lework-primary-softer)]"
							>
								<RefreshCw className="size-3" />
								重试
							</button>
						</div>
					) : shouldRender && entry.page ? (
						<LoaderCircle className="size-5 animate-spin text-slate-300" />
					) : (
						<span className="text-xs tabular-nums text-slate-300">{pageIndex + 1}</span>
					)}
				</div>
			) : null}
		</section>
	);
}

function DocumentLoadingState() {
	return (
		<div className="absolute inset-0 flex items-center justify-center bg-[#e7eaf0] text-sm text-slate-500">
			<LoaderCircle className="mr-2 size-4 animate-spin" />
			正在打开 PDF
		</div>
	);
}

function DocumentErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
	return (
		<div className="absolute inset-0 flex items-center justify-center bg-[#e7eaf0] px-8 text-center text-sm text-slate-500">
			<div>
				<p className="font-medium text-slate-700">PDF 预览失败</p>
				<p className="mt-1 max-w-md text-xs leading-5">{message}</p>
				<button
					type="button"
					onClick={onRetry}
					className="mt-4 inline-flex items-center gap-1.5 rounded-md bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-sm ring-1 ring-slate-200 hover:bg-slate-50"
				>
					<RefreshCw className="size-3.5" />
					重新加载
				</button>
			</div>
		</div>
	);
}

async function loadPdfJs(): Promise<PdfJsModule> {
	const pdfJs = await import("pdfjs-dist/legacy/build/pdf.mjs");
	if (!pdfJs.GlobalWorkerOptions.workerSrc) {
		pdfJs.GlobalWorkerOptions.workerSrc = new URL(
			"pdfjs-dist/legacy/build/pdf.worker.min.mjs",
			import.meta.url,
		).toString();
	}
	return pdfJs;
}

function configurePdfTextLayer(textLayer: HTMLElement, viewport: PageViewport) {
	Object.assign(textLayer.style, {
		caretColor: "CanvasText",
		letterSpacing: "normal",
		lineHeight: "1",
		overflow: "hidden",
		textAlign: "initial",
		transformOrigin: "0 0",
		userSelect: "text",
		width: `${viewport.width}px`,
		height: `${viewport.height}px`,
		wordSpacing: "normal",
	});
	textLayer.style.setProperty("-webkit-user-select", "text");
	textLayer.style.setProperty("--total-scale-factor", String(viewport.scale));
	textLayer.style.setProperty("--scale-round-x", "1px");
	textLayer.style.setProperty("--scale-round-y", "1px");
	textLayer.style.setProperty(
		"--text-scale-factor",
		"calc(var(--total-scale-factor) * var(--min-font-size))",
	);
	textLayer.style.setProperty("--min-font-size-inv", "calc(1 / var(--min-font-size))");
}

function configurePdfTextItem({
	hasEOL,
	itemIndex,
	pageIndex,
	textDiv,
}: {
	hasEOL: boolean;
	itemIndex: number;
	pageIndex: number;
	textDiv: HTMLElement;
}) {
	textDiv.dataset.pdfPageIndex = String(pageIndex);
	textDiv.dataset.pdfItemIndex = String(itemIndex);
	textDiv.dataset.pdfHasEol = String(hasEOL);
	Object.assign(textDiv.style, {
		color: "transparent",
		cursor: "text",
		fontSize: "calc(var(--text-scale-factor) * var(--font-height))",
		position: "absolute",
		transform:
			"rotate(var(--rotate, 0deg)) scaleX(var(--scale-x, 1)) scale(var(--min-font-size-inv))",
		transformOrigin: "0% 0%",
		userSelect: "text",
		whiteSpace: "pre",
	});
	textDiv.style.setProperty("-webkit-user-select", "text");
}

function mapPdfQuadToViewportRect(rect: PdfSelectionRect, viewport: PageViewport): CSSProperties {
	const points = rect.pdfQuad.map((point) => viewport.convertToViewportPoint(point.x, point.y));
	const xs = points.map((point) => point[0] ?? 0);
	const ys = points.map((point) => point[1] ?? 0);
	const left = Math.min(...xs);
	const top = Math.min(...ys);
	return {
		left,
		top,
		width: Math.max(...xs) - left,
		height: Math.max(...ys) - top,
	};
}

async function sha256Hex(buffer: ArrayBuffer): Promise<string | null> {
	if (!globalThis.crypto?.subtle) return null;
	try {
		const digest = await globalThis.crypto.subtle.digest("SHA-256", buffer);
		return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join(
			"",
		);
	} catch {
		return null;
	}
}

function formatPdfError(error: unknown): string {
	if (!(error instanceof Error)) return "无法读取此 PDF 文件";
	if (error.name === "PasswordException") return "此 PDF 受密码保护，当前预览暂不支持解密";
	if (error.name === "InvalidPDFException") return "PDF 文件已损坏或格式不完整";
	if (error.name === "MissingPDFException") return "未找到 PDF 文件内容";
	return error.message || "无法读取此 PDF 文件";
}

function emptySourceIdentity(): PdfSourceIdentity {
	return { contentHash: null, documentFingerprint: null };
}

function isRenderingCancelled(error: unknown): boolean {
	return error instanceof Error && error.name === "RenderingCancelledException";
}

function clamp(value: number, min: number, max: number): number {
	return Math.min(Math.max(value, min), max);
}

const PDF_TEXT_LAYER_STYLES = `
.pdf-text-layer span,
.pdf-text-layer br {
  color: transparent;
  position: absolute;
  white-space: pre;
  cursor: text;
  transform-origin: 0% 0%;
  user-select: text;
  -webkit-user-select: text;
}
.pdf-text-layer ::selection {
  background: rgba(14, 165, 233, 0.28);
  color: transparent;
}
.pdf-text-layer ::-moz-selection {
  background: rgba(14, 165, 233, 0.28);
  color: transparent;
}
`;
