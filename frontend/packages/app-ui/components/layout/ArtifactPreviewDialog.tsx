"use client";

import { fetchArtifactDownload, fetchFilePreviewByStorageUri, projectFileApi } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Sheet,
	SheetClose,
	SheetContent,
	SheetDescription,
	SheetHeader,
	SheetTitle,
} from "@leros/ui/components/ui/sheet";
import { Download, FileText, LoaderCircle, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { getOfficeOpenXmlFormat, type OfficeOpenXmlFormat, OfficePreview } from "./OfficePreview";
import { ProjectFileTypeIcon } from "./project-file-type-icon";
import { SpreadsheetPreview } from "./SpreadsheetPreview";

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
	| { status: "ready"; text?: string; objectUrl?: string; buffer?: ArrayBuffer }
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
	const previewKind = useMemo(() => detectPreviewKind(artifact), [artifact]);

	const artifactPath = useMemo(() => {
		if (!artifact || !projectId) return undefined;
		return artifact.id;
	}, [artifact, projectId]);

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
					response = await fetchArtifactDownload(currentArtifact.id, {
						signal: controller.signal,
					});
				}

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
				if (!cancelled) setPreview({ status: "ready", objectUrl });
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

	const handleDownload = async () => {
		if (!artifact) return;
		try {
			let response: Response;
			if (artifact.storageUri) {
				response = await fetchFilePreviewByStorageUri(artifact.storageUri);
			} else if (projectId && artifactPath) {
				response = await projectFileApi.fetchDownload(projectId, artifactPath);
			} else {
				response = await fetchArtifactDownload(artifact.id);
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

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent
				side="right"
				showCloseButton={false}
				className="inset-y-4 right-4 h-auto w-[calc(100vw-2rem)] gap-0 overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-0 shadow-2xl sm:max-w-none md:w-[min(48vw,980px)]"
			>
				{artifact && (
					<>
						<SheetHeader className="flex-row items-center gap-3 border-b border-[var(--leros-control-border)] px-5 py-4">
							<div className="flex size-7 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-muted)]">
								<ProjectFileTypeIcon fileName={artifact.name} className="size-4 object-contain" />
							</div>
							<div className="h-5 w-px shrink-0 bg-[var(--leros-control-border)]" />
							<div className="min-w-0 flex-1">
								<SheetTitle className="truncate text-sm font-medium text-[var(--leros-text-muted)]">
									{artifact.title || artifact.name}
								</SheetTitle>
								<SheetDescription className="sr-only">{artifact.name} 文件预览</SheetDescription>
							</div>
							<Button
								variant="ghost"
								size="icon-sm"
								onClick={handleDownload}
								title="下载"
								className="shrink-0 text-[var(--leros-text)]"
							>
								<Download className="size-4" />
							</Button>
							<SheetClose
								render={
									<Button
										variant="ghost"
										size="icon-sm"
										title="关闭"
										className="shrink-0 text-[var(--leros-text)]"
									/>
								}
							>
								<X className="size-4" />
							</SheetClose>
						</SheetHeader>
						<div className="min-h-0 flex-1 overflow-hidden bg-[#f6f7fb]">
							<ArtifactPreviewBody
								artifact={artifact}
								previewKind={previewKind}
								preview={preview}
							/>
						</div>
					</>
				)}
			</SheetContent>
		</Sheet>
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
			<div className="flex h-full items-center justify-center text-[var(--leros-text-muted)]">
				<LoaderCircle className="mr-2 size-4 animate-spin" />
				加载预览
			</div>
		);
	}

	if (preview.status === "error") {
		return (
			<div className="flex h-full items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
				<div>
					<p>无法加载预览</p>
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
		return <OfficePreview buffer={preview.buffer} fileName={artifact.name} format={previewKind} />;
	}

	if (previewKind === "spreadsheet" && preview.buffer) {
		return <SpreadsheetPreview buffer={preview.buffer} fileName={artifact.name} />;
	}

	if (previewKind === "markdown") {
		return (
			<div className="h-full overflow-auto bg-[var(--leros-surface)] px-8 py-7">
				<MarkdownRenderer
					content={preview.text ?? ""}
					className="prose prose-slate prose-sm max-w-none prose-headings:text-[var(--leros-text-strong)] prose-p:leading-7 prose-pre:rounded-lg prose-pre:bg-slate-950"
				/>
			</div>
		);
	}

	if (previewKind === "text") {
		return (
			<pre className="h-full overflow-auto bg-[var(--leros-surface)] p-5 text-sm leading-6 text-[var(--leros-text)]">
				{preview.text ?? ""}
			</pre>
		);
	}

	if (previewKind === "image" && preview.objectUrl) {
		return (
			<div className="flex h-full items-center justify-center overflow-auto p-5">
				<img
					src={preview.objectUrl}
					alt={artifact.title || artifact.name}
					className="max-h-full max-w-full rounded-lg border border-[var(--leros-control-border)] bg-white object-contain shadow-sm"
				/>
			</div>
		);
	}

	if (previewKind === "pdf" && preview.objectUrl) {
		return (
			<iframe
				title={artifact.title || artifact.name}
				src={preview.objectUrl}
				className="h-full w-full border-0 bg-white"
			/>
		);
	}

	return (
		<div className="flex h-full items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
			<div>
				<FileText className="mx-auto mb-3 size-8 text-[var(--leros-text-subtle)]" />
				<p>此文件类型暂不支持内嵌预览</p>
				<p className="mt-1 text-xs">可下载到本地查看完整内容</p>
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
