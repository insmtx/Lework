"use client";

import {
	fetchFilePreviewByPublicId,
	fetchFilePreviewByStorageUri,
	projectFileApi,
} from "@leros/store";
import { ChevronsLeftRightEllipsis, Download, FileText, LoaderCircle, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { getOfficeOpenXmlFormat, type OfficeOpenXmlFormat, OfficePreview } from "./OfficePreview";
import { SpreadsheetPreview } from "./SpreadsheetPreview";

const FILE_PREVIEW_DRAWER_DEFAULT_WIDTH = 860;
const FILE_PREVIEW_DRAWER_MIN_WIDTH = 720;
const FILE_PREVIEW_DRAWER_MAX_WIDTH = 1200;

type PreviewKind =
	| OfficeOpenXmlFormat
	| "spreadsheet"
	| "markdown"
	| "text"
	| "image"
	| "pdf"
	| "unsupported";

export type ArtifactPreviewItem = {
	id: string;
	name: string;
	title: string;
	description?: string;
	type: "document" | "spreadsheet" | "image";
	artifactType: string;
	mimeType?: string;
	size: string;
	updatedAt?: number;
	downloadUrl: string;
	storageUri?: string;
	sha256?: string;
};

type PreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "ready"; text?: string; objectUrl?: string; buffer?: ArrayBuffer; mimeType?: string }
	| { status: "error"; message: string };

export function ArtifactPreviewDialog({
	artifact,
	open,
	onOpenChange,
	projectId,
}: {
	artifact: ArtifactPreviewItem | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	projectId?: string;
}) {
	const [preview, setPreview] = useState<PreviewState>({ status: "idle" });
	const [drawerWidth, setDrawerWidth] = useState(FILE_PREVIEW_DRAWER_DEFAULT_WIDTH);
	const drawerRef = useRef<HTMLDivElement>(null);
	const previewKind = useMemo(() => detectPreviewKind(artifact), [artifact]);

	const artifactPath = useMemo(() => {
		if (!artifact || !projectId) return undefined;
		return artifact.id;
	}, [artifact, projectId]);

	const closePreview = () => {
		onOpenChange(false);
	};

	useEffect(() => {
		if (!open || !artifact) {
			setPreview({ status: "idle" });
			return;
		}

		if (previewKind === "unsupported") {
			setPreview({ status: "ready" });
			return;
		}

		const currentArtifact = artifact;
		const currentPath = artifactPath;
		const currentProjectId = projectId;
		let cancelled = false;
		let objectUrl: string | undefined;
		const controller = new AbortController();

		async function loadPreview() {
			setPreview({ status: "loading" });
			try {
				let response: Response;
				if (currentArtifact.storageUri) {
					response = await fetchFilePreviewByStorageUri(currentArtifact.storageUri, {
						signal: controller.signal,
					});
				} else if (currentProjectId && currentPath) {
					response = await projectFileApi.fetchDownload(currentProjectId, currentPath, {
						signal: controller.signal,
					});
				} else {
					response = await fetchFilePreviewByPublicId(currentArtifact.id, {
						signal: controller.signal,
					});
				}

				const mimeType =
					response.headers.get("content-type") ??
					currentArtifact.mimeType ??
					"application/octet-stream";

				if (previewKind === "markdown" || previewKind === "text") {
					const text = await response.text();
					if (!cancelled) setPreview({ status: "ready", text });
					return;
				}

				if (
					previewKind === "docx" ||
					previewKind === "xlsx" ||
					previewKind === "pptx" ||
					previewKind === "spreadsheet"
				) {
					const buffer = await response.arrayBuffer();
					if (!cancelled) setPreview({ status: "ready", buffer });
					return;
				}

				const blob = await response.blob();
				objectUrl = URL.createObjectURL(blob);
				if (!cancelled) setPreview({ status: "ready", objectUrl, mimeType });
			} catch (err) {
				if (cancelled || controller.signal.aborted) return;
				const message = err instanceof Error ? err.message : "预览加载失败";
				setPreview({ status: "error", message });
			}
		}

		loadPreview();

		return () => {
			cancelled = true;
			controller.abort();
			if (objectUrl) URL.revokeObjectURL(objectUrl);
		};
	}, [open, artifact, artifactPath, previewKind, projectId]);

	useEffect(() => {
		if (!open) return;

		const handlePointerDown = (event: PointerEvent) => {
			const target = event.target;
			if (!(target instanceof Element)) return;
			if (drawerRef.current?.contains(target)) return;
			if (target.closest("[data-file-preview-trigger]")) return;
			onOpenChange(false);
		};

		document.addEventListener("pointerdown", handlePointerDown);
		return () => document.removeEventListener("pointerdown", handlePointerDown);
	}, [open, onOpenChange]);

	const handleDownload = async () => {
		if (!artifact) return;
		try {
			let response: Response;
			if (artifact.storageUri) {
				response = await fetchFilePreviewByStorageUri(artifact.storageUri);
			} else if (projectId && artifactPath) {
				response = await projectFileApi.fetchDownload(projectId, artifactPath);
			} else {
				response = await fetchFilePreviewByPublicId(artifact.id);
			}
			const blob = await response.blob();
			const objectUrl = URL.createObjectURL(blob);
			const link = document.createElement("a");
			link.href = objectUrl;
			link.download = artifact.name;
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
		} catch (err) {
			console.error("Failed to download artifact", err);
		}
	};

	const handleDrawerResizeStart = (event: React.PointerEvent<HTMLElement>) => {
		event.preventDefault();
		const startX = event.clientX;
		const startWidth = drawerWidth;

		const handlePointerMove = (moveEvent: PointerEvent) => {
			const candidateWidth = startWidth - (moveEvent.clientX - startX);
			const maxWidth = Math.min(FILE_PREVIEW_DRAWER_MAX_WIDTH, window.innerWidth - 160);
			const nextWidth = Math.min(
				Math.max(candidateWidth, FILE_PREVIEW_DRAWER_MIN_WIDTH),
				Math.max(FILE_PREVIEW_DRAWER_MIN_WIDTH, maxWidth),
			);
			setDrawerWidth(nextWidth);
		};

		const handlePointerUp = () => {
			window.removeEventListener("pointermove", handlePointerMove);
			window.removeEventListener("pointerup", handlePointerUp);
		};

		window.addEventListener("pointermove", handlePointerMove);
		window.addEventListener("pointerup", handlePointerUp);
	};

	if (!open || !artifact) {
		return null;
	}

	return (
		<div
			ref={drawerRef}
			className="fixed inset-y-4 right-4 z-50 flex flex-col overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-0 shadow-2xl"
			style={{ width: `${drawerWidth}px`, maxWidth: `${drawerWidth}px` }}
		>
			<button
				type="button"
				aria-label="拖动调整预览宽度"
				title="拖动调整预览宽度"
				onPointerDown={handleDrawerResizeStart}
				className="absolute left-0 top-0 z-10 flex h-full w-4 -translate-x-1/2 cursor-col-resize items-center justify-center"
			>
				<div className="flex h-16 w-2 items-center justify-center rounded-full bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] shadow-sm ring-1 ring-[var(--leros-control-border)]">
					<ChevronsLeftRightEllipsis className="size-3" />
				</div>
			</button>
			<div className="flex items-center justify-between border-b border-[var(--leros-control-border)] px-6 py-4">
				<div className="min-w-0">
					<div className="truncate text-lg font-medium text-[var(--leros-text-strong)]">
						{artifact.title || artifact.name}
					</div>
				</div>
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={() => void handleDownload()}
						className="rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)]"
						title="下载"
					>
						<Download className="size-4" />
					</button>
					<button
						type="button"
						onClick={closePreview}
						className="rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)]"
						title="关闭"
					>
						<X className="size-4" />
					</button>
				</div>
			</div>
			<div className="flex min-h-0 flex-1 flex-col overflow-hidden bg-[var(--leros-surface-soft)] p-6">
				<ArtifactPreviewBody artifact={artifact} previewKind={previewKind} preview={preview} />
			</div>
		</div>
	);
}

function ArtifactPreviewBody({
	artifact,
	previewKind,
	preview,
}: {
	artifact: ArtifactPreviewItem;
	previewKind: PreviewKind;
	preview: PreviewState;
}) {
	if (preview.status === "loading" || preview.status === "idle") {
		return (
			<div className="flex flex-1 items-center justify-center text-sm text-[var(--leros-text-muted)]">
				<LoaderCircle className="mr-2 size-4 animate-spin" />
				加载预览中
			</div>
		);
	}

	if (preview.status === "error") {
		return (
			<div className="flex flex-1 items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
				<div>
					<p>无法加载文件预览</p>
					<p className="mt-1 text-xs">{preview.message}</p>
				</div>
			</div>
		);
	}

	if (preview.status !== "ready") {
		return null;
	}

	if (
		(previewKind === "docx" || previewKind === "xlsx" || previewKind === "pptx") &&
		preview.buffer
	) {
		return (
			<div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm">
				<OfficePreview buffer={preview.buffer} fileName={artifact.name} format={previewKind} />
			</div>
		);
	}

	if (previewKind === "spreadsheet" && preview.buffer) {
		return (
			<div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm">
				<SpreadsheetPreview buffer={preview.buffer} fileName={artifact.name} />
			</div>
		);
	}

	if (previewKind === "markdown") {
		return (
			<div className="min-h-0 flex-1 overflow-auto rounded-xl bg-white px-8 py-7 shadow-sm">
				<MarkdownRenderer
					content={preview.text ?? ""}
					className="prose prose-slate prose-sm max-w-none prose-headings:text-[var(--leros-text-strong)] prose-p:leading-7 prose-pre:rounded-lg prose-pre:bg-slate-950"
				/>
			</div>
		);
	}

	if (previewKind === "text") {
		return (
			<pre className="min-h-0 flex-1 overflow-auto rounded-xl bg-white p-4 text-sm leading-6 text-[var(--leros-text)] shadow-sm">
				{preview.text ?? ""}
			</pre>
		);
	}

	if (previewKind === "image" && preview.objectUrl) {
		return (
			<div className="flex flex-1 items-center justify-center overflow-auto rounded-xl bg-white p-4 shadow-sm">
				<img
					src={preview.objectUrl}
					alt={artifact.title || artifact.name}
					className="max-h-full max-w-full object-contain"
				/>
			</div>
		);
	}

	if (previewKind === "pdf" && preview.objectUrl) {
		return (
			<div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm">
				<iframe
					title={artifact.title || artifact.name}
					src={preview.objectUrl}
					className="h-full w-full border-0 bg-white"
				/>
			</div>
		);
	}

	return (
		<div className="flex flex-1 items-center justify-center rounded-xl bg-white px-8 text-center text-sm text-[var(--leros-text-muted)] shadow-sm">
			<div>
				<FileText className="mx-auto mb-3 size-8 text-[var(--leros-text-subtle)]" />
				<p>此文件类型暂不支持内嵌预览</p>
				<p className="mt-1 text-xs">请使用下载按钮在本地查看</p>
			</div>
		</div>
	);
}

function detectPreviewKind(artifact: ArtifactPreviewItem | null): PreviewKind {
	if (!artifact) return "unsupported";

	const mimeType = artifact.mimeType?.toLowerCase() ?? "";
	const name = artifact.name.toLowerCase();
	const officeFormat = getOfficeOpenXmlFormat(name, mimeType);

	if (officeFormat) return officeFormat;
	if (
		mimeType.includes("spreadsheet") ||
		mimeType.includes("excel") ||
		mimeType === "text/csv" ||
		name.endsWith(".xls") ||
		name.endsWith(".csv")
	) {
		return "spreadsheet";
	}
	if (mimeType.includes("markdown") || name.endsWith(".md") || name.endsWith(".markdown")) {
		return "markdown";
	}
	if (mimeType.startsWith("image/")) {
		return "image";
	}
	if (mimeType === "application/pdf" || name.endsWith(".pdf")) {
		return "pdf";
	}
	if (
		mimeType.startsWith("text/") ||
		name.endsWith(".txt") ||
		name.endsWith(".json") ||
		name.endsWith(".yaml") ||
		name.endsWith(".yml") ||
		name.endsWith(".log")
	) {
		return "text";
	}

	return "unsupported";
}
