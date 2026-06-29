"use client";

import { fetchFilePreview } from "@leros/store";
import type { MessageAttachment } from "@leros/store/types/chat";
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
import {
	getOfficeOpenXmlFormat,
	type OfficeOpenXmlFormat,
	OfficePreview,
} from "../layout/OfficePreview";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import { SpreadsheetPreview } from "../layout/SpreadsheetPreview";

type PreviewKind =
	| OfficeOpenXmlFormat
	| "spreadsheet"
	| "markdown"
	| "text"
	| "image"
	| "pdf"
	| "unsupported";

type PreviewState =
	| { status: "idle" }
	| { status: "loading" }
	| { status: "ready"; text?: string; objectUrl?: string; buffer?: ArrayBuffer }
	| { status: "error"; message: string };

export function MessageAttachmentPreviewDialog({
	attachment,
	open,
	onOpenChange,
}: {
	attachment: MessageAttachment | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const [preview, setPreview] = useState<PreviewState>({ status: "idle" });
	const previewKind = useMemo(() => getPreviewKind(attachment), [attachment]);

	useEffect(() => {
		if (!open || !attachment) {
			setPreview({ status: "idle" });
			return;
		}

		if (previewKind === "unsupported") {
			setPreview({ status: "ready" });
			return;
		}

		let cancelled = false;
		let objectUrl: string | undefined;
		const controller = new AbortController();

		async function loadPreview() {
			setPreview({ status: "loading" });
			try {
				const currentAttachment = attachment;
				if (!currentAttachment) return;

				const response = await fetchAttachmentContent(currentAttachment, {
					signal: controller.signal,
				});

				if (previewKind === "markdown" || previewKind === "text") {
					const text = await response.text();
					if (!cancelled) setPreview({ status: "ready", text });
					return;
				}

				// Office / 旧表格预览需要直接消费二进制内容，不能只转 blob URL。
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
				const message = err instanceof Error ? err.message : "Failed to load preview";
				setPreview({ status: "error", message });
			}
		}

		loadPreview();

		return () => {
			cancelled = true;
			controller.abort();
			if (objectUrl) URL.revokeObjectURL(objectUrl);
		};
	}, [attachment, open, previewKind]);

	const handleDownload = async () => {
		if (!attachment) return;
		try {
			const response = await fetchAttachmentContent(attachment);
			const blob = await response.blob();
			const objectUrl = URL.createObjectURL(blob);
			const link = document.createElement("a");
			link.href = objectUrl;
			link.download = attachment.name;
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
		} catch (err) {
			console.error("Failed to download attachment", err);
		}
	};

	return (
		<Sheet open={open} onOpenChange={onOpenChange}>
			<SheetContent
				side="right"
				showCloseButton={false}
				className="inset-y-4 right-4 h-auto w-[calc(100vw-2rem)] gap-0 overflow-hidden rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-0 shadow-2xl sm:max-w-none md:w-[min(48vw,980px)]"
			>
				{attachment && (
					<>
						<SheetHeader className="flex-row items-center gap-3 border-b border-[var(--leros-control-border)] px-5 py-4">
							<div className="flex size-7 shrink-0 items-center justify-center rounded-md text-[var(--leros-text-muted)]">
								<ProjectFileTypeIcon fileName={attachment.name} className="size-4 object-contain" />
							</div>
							<div className="h-5 w-px shrink-0 bg-[var(--leros-control-border)]" />
							<div className="min-w-0 flex-1">
								<SheetTitle className="truncate text-sm font-medium text-[var(--leros-text-muted)]">
									{attachment.name}
								</SheetTitle>
								<SheetDescription className="sr-only">
									{attachment.name} attachment preview
								</SheetDescription>
							</div>
							<Button
								variant="ghost"
								size="icon-sm"
								onClick={handleDownload}
								title="Download"
								className="shrink-0 text-[var(--leros-text)]"
							>
								<Download className="size-4" />
							</Button>
							<SheetClose
								render={
									<Button
										variant="ghost"
										size="icon-sm"
										title="Close"
										className="shrink-0 text-[var(--leros-text)]"
									/>
								}
							>
								<X className="size-4" />
							</SheetClose>
						</SheetHeader>
						<div className="min-h-0 flex-1 overflow-hidden bg-[#f6f7fb]">
							<AttachmentPreviewBody
								attachment={attachment}
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

async function fetchAttachmentContent(
	attachment: MessageAttachment,
	options?: { signal?: AbortSignal },
): Promise<Response> {
	// 中文注释：问答附件与产物文件统一走 /files/preview，避免误用项目 download 接口
	if (attachment.storageUri || attachment.fileUploadId) {
		return fetchFilePreview(
			{ storageUri: attachment.storageUri, publicId: attachment.fileUploadId },
			options,
		);
	}
	if (attachment.url?.startsWith("blob:")) {
		return fetch(attachment.url, { signal: options?.signal });
	}
	if (attachment.url) {
		return fetch(attachment.url, { signal: options?.signal });
	}
	throw new Error("Attachment has no preview source");
}

function getPreviewKind(attachment: MessageAttachment | null): PreviewKind {
	if (!attachment) return "unsupported";
	const mimeType = attachment.mimeType.toLowerCase();
	const name = attachment.name.toLowerCase();
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
	if (mimeType.startsWith("image/")) return "image";
	if (mimeType.includes("pdf") || name.endsWith(".pdf")) return "pdf";
	if (mimeType.includes("markdown") || name.endsWith(".md") || name.endsWith(".markdown")) {
		return "markdown";
	}
	if (
		mimeType.startsWith("text/") ||
		mimeType.includes("json") ||
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

function AttachmentPreviewBody({
	attachment,
	previewKind,
	preview,
}: {
	attachment: MessageAttachment;
	previewKind: PreviewKind;
	preview: PreviewState;
}) {
	if (preview.status === "loading" || preview.status === "idle") {
		return (
			<div className="flex h-full items-center justify-center text-[var(--leros-text-muted)]">
				<LoaderCircle className="mr-2 size-4 animate-spin" />
				Loading preview
			</div>
		);
	}

	if (preview.status === "error") {
		return (
			<div className="flex h-full items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
				<div>
					<p>Unable to load preview</p>
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
			<OfficePreview buffer={preview.buffer} fileName={attachment.name} format={previewKind} />
		);
	}

	if (previewKind === "spreadsheet" && preview.buffer) {
		return <SpreadsheetPreview buffer={preview.buffer} fileName={attachment.name} />;
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
			<pre className="h-full overflow-auto whitespace-pre-wrap break-words bg-[var(--leros-surface)] px-8 py-7 font-mono text-sm leading-6 text-[var(--leros-text-strong)]">
				{preview.text ?? ""}
			</pre>
		);
	}

	if (previewKind === "image" && preview.objectUrl) {
		return (
			<div className="flex h-full items-center justify-center overflow-auto p-6">
				<img
					src={preview.objectUrl}
					alt={attachment.name}
					className="max-h-full max-w-full rounded-xl object-contain shadow-lg"
				/>
			</div>
		);
	}

	if (previewKind === "pdf" && preview.objectUrl) {
		return (
			<iframe
				title={attachment.name}
				src={preview.objectUrl}
				className="h-full w-full border-0 bg-white"
			/>
		);
	}

	return (
		<div className="flex h-full items-center justify-center px-8 text-center text-sm text-[var(--leros-text-muted)]">
			<div>
				<FileText className="mx-auto mb-3 size-10 text-slate-300" />
				<p>This attachment type cannot be previewed yet</p>
			</div>
		</div>
	);
}
