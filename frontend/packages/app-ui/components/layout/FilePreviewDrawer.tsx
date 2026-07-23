"use client";

import { type BackendProjectFileVersion, projectFileApi, useChatStore } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import {
	ChevronsLeftRightEllipsis,
	Download,
	FileText,
	History,
	LoaderCircle,
	ShieldCheck,
	X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { MarkdownRenderer } from "../common/MarkdownRenderer";
import { DocxSelectionToolbar } from "./DocxSelectionToolbar";
import {
	docxSelectionComposerActions,
	type PendingDocxVersionSync,
	useDocxSelectionComposerStore,
} from "./docx-selection-composer-store";
import {
	DOCX_SELECTION_TEXT_LIMIT,
	type DocxPolishAction,
	getDocxPolishPrompt,
} from "./docx-selection-edit";
import { filePreviewActions } from "./file-preview-store";
import {
	detectFilePreviewKind,
	downloadFilePreviewContent,
	FILE_PREVIEW_DRAWER_DEFAULT_WIDTH,
	FILE_PREVIEW_DRAWER_MAX_WIDTH,
	FILE_PREVIEW_DRAWER_MIN_WIDTH,
	type FilePreviewItem,
	type FilePreviewKind,
	type FilePreviewState,
	fetchFilePreviewContent,
	PROJECT_FILE_VERSION_CHANGED_EVENT,
} from "./file-preview-utils";
import { OfficePreview, type OfficeTextSelection } from "./OfficePreview";
import { PdfPreview, type PdfTextSelection } from "./PdfPreview";
import {
	buildProjectFileVersionEntries,
	getCurrentProjectFileVersionEntry,
	waitForProjectFileVersionChange,
} from "./project-file-version-sync";
import { SpreadsheetPreview } from "./SpreadsheetPreview";

export type { FilePreviewItem } from "./file-preview-utils";

export function FilePreviewDrawer({
	file,
	open,
	onOpenChange,
}: {
	file: FilePreviewItem | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const [preview, setPreview] = useState<FilePreviewState>({ status: "idle" });
	const [htmlView, setHtmlView] = useState<"preview" | "source">("preview");
	const [drawerWidth, setDrawerWidth] = useState(FILE_PREVIEW_DRAWER_DEFAULT_WIDTH);
	const [historyOpen, setHistoryOpen] = useState(false);
	const [versions, setVersions] = useState<BackendProjectFileVersion[]>([]);
	const [versionsLoading, setVersionsLoading] = useState(false);
	const [versionsError, setVersionsError] = useState<string | null>(null);
	const [selectedVersionPublicId, setSelectedVersionPublicId] = useState("");
	const [selectedVersionKey, setSelectedVersionKey] = useState("");
	const [officeSelection, setOfficeSelection] = useState<OfficeTextSelection | null>(null);
	const [pendingVersionSync, setPendingVersionSync] = useState<PendingDocxVersionSync | null>(null);
	const drawerRef = useRef<HTMLDivElement>(null);
	const selectedVersionPublicIdRef = useRef(selectedVersionPublicId);
	const { isGenerating } = useChatStore((state) => state);
	const { submission } = useDocxSelectionComposerStore();
	const versionEntries = useMemo(() => buildProjectFileVersionEntries(versions), [versions]);
	const selectedVersion = useMemo(() => {
		if (selectedVersionKey) {
			const selectedEntry = versionEntries.find((entry) => entry.key === selectedVersionKey);
			if (selectedEntry) return selectedEntry.version;
		}
		return (
			versionEntries.find((entry) => entry.version.public_id === selectedVersionPublicId)
				?.version ?? null
		);
	}, [selectedVersionKey, selectedVersionPublicId, versionEntries]);
	const previewFile = useMemo(() => {
		if (!file || !selectedVersion) return file;
		return {
			...file,
			name: selectedVersion.name || file.name,
			title: selectedVersion.name || file.name,
			mimeType: selectedVersion.mime_type || file.mimeType,
			storageUri: selectedVersion.storage_uri || undefined,
			versionPublicId: selectedVersion.public_id,
			versionLabel: selectedVersion.version_label,
			versionNo: selectedVersion.version_no,
		} satisfies FilePreviewItem;
	}, [file, selectedVersion]);
	const previewKind = useMemo(() => detectFilePreviewKind(previewFile), [previewFile]);
	const canShowHistory = Boolean(file?.projectId && file.publicId);
	const displayedVersionNo = previewFile?.versionNo ?? file?.versionNo;
	const isHistoricalVersion = Boolean(
		previewFile?.versionPublicId && file?.publicId && previewFile.versionPublicId !== file.publicId,
	);

	const closePreview = () => {
		onOpenChange(false);
	};

	useEffect(() => {
		selectedVersionPublicIdRef.current = selectedVersionPublicId;
	}, [selectedVersionPublicId]);

	useEffect(() => {
		if (!submission) return;
		setPendingVersionSync(submission);
		docxSelectionComposerActions.clearSubmission(submission.id);
	}, [submission]);

	useEffect(() => {
		if (!open || !file) return;
		setSelectedVersionKey("");
		setSelectedVersionPublicId(file.versionPublicId ?? "");
	}, [open, file?.publicId, file?.versionPublicId]);

	useEffect(() => {
		setHtmlView("preview");
		setOfficeSelection(null);
	}, [file?.publicId, file?.name, selectedVersionPublicId]);

	useEffect(() => {
		if (!open || !file) {
			setPreview({ status: "idle" });
			setHistoryOpen(false);
			setVersions([]);
			setVersionsError(null);
			setSelectedVersionKey("");
			setSelectedVersionPublicId("");
			return;
		}
		if (previewKind === "unsupported") {
			setPreview({ status: "ready" });
			return;
		}
		if (!previewFile) {
			setPreview({ status: "error", message: "文件缺少预览来源" });
			return;
		}

		const currentFile = previewFile;
		let cancelled = false;
		let objectUrl: string | undefined;
		const controller = new AbortController();

		async function loadPreview() {
			setPreview({ status: "loading" });
			try {
				const response = await fetchFilePreviewContent(currentFile, {
					signal: controller.signal,
				});
				const mimeType =
					response.headers.get("content-type") ??
					currentFile.mimeType ??
					"application/octet-stream";

				if (previewKind === "markdown" || previewKind === "text") {
					const text = await response.text();
					if (!cancelled) setPreview({ status: "ready", text });
					return;
				}

				if (previewKind === "html") {
					const text = await response.text();
					const htmlBlob = new Blob([text], { type: "text/html" });
					objectUrl = URL.createObjectURL(htmlBlob);
					if (!cancelled) setPreview({ status: "ready", text, objectUrl, mimeType });
					return;
				}

				if (
					previewKind === "docx" ||
					previewKind === "xlsx" ||
					previewKind === "pptx" ||
					previewKind === "pdf" ||
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
	}, [open, file, previewFile, previewKind]);

	useEffect(() => {
		if (open && file) {
			setHistoryOpen(Boolean(file.openHistory));
		}
	}, [open, file]);

	useEffect(() => {
		if (!open || !file?.projectId || !file.publicId || (!historyOpen && !file.versionPublicId)) {
			return;
		}
		const projectId = file.projectId;
		const filePublicId = file.publicId;
		const requestedVersionPublicId = file.versionPublicId;
		const requestedVersionNo = file.versionNo;

		let cancelled = false;
		async function loadVersions() {
			setVersionsLoading(true);
			setVersionsError(null);
			try {
				const response = await projectFileApi.versions(projectId, filePublicId);
				if (cancelled) return;
				if (response.data.code !== 0) {
					throw new Error(response.data.message || "版本历史加载失败");
				}
				const versionList = response.data.data;
				const items = versionList?.items ?? [];
				const entries = buildProjectFileVersionEntries(items);
				const selectedEntry =
					entries.find((entry) => entry.key === selectedVersionKey) ??
					entries.find(
						(entry) =>
							entry.version.public_id === requestedVersionPublicId &&
							(!requestedVersionNo || entry.version.version_no === requestedVersionNo),
					) ??
					getCurrentProjectFileVersionEntry(
						entries,
						versionList?.current_file_public_id || filePublicId,
					);
				setVersions(items);
				if (selectedEntry) {
					setSelectedVersionKey(selectedEntry.key);
					setSelectedVersionPublicId(selectedEntry.version.public_id);
				}
			} catch (err) {
				if (!cancelled) {
					setVersionsError(err instanceof Error ? err.message : "版本历史加载失败");
				}
			} finally {
				if (!cancelled) setVersionsLoading(false);
			}
		}

		loadVersions();
		return () => {
			cancelled = true;
		};
	}, [open, file?.projectId, file?.publicId, file?.versionPublicId, historyOpen]);

	useEffect(() => {
		if (!pendingVersionSync || isGenerating) return;
		const pending = pendingVersionSync;
		const controller = new AbortController();

		async function syncLatestVersion() {
			try {
				const change = await waitForProjectFileVersionChange({
					baselinePublicId: pending.baselinePublicId,
					baselineVersionNo: pending.baselineVersionNo,
					signal: controller.signal,
					loadVersions: async () => {
						const response = await projectFileApi.versions(
							pending.projectId,
							pending.chainFilePublicId,
						);
						if (response.data.code !== 0) {
							throw new Error(response.data.message || "最新文件版本加载失败");
						}
						return response.data.data;
					},
				});
				if (controller.signal.aborted) return;
				setPendingVersionSync(null);
				if (!change) {
					toast.warning("未检测到新的文件版本，预览保持当前版本");
					return;
				}

				window.dispatchEvent(
					new CustomEvent(PROJECT_FILE_VERSION_CHANGED_EVENT, {
						detail: { projectId: pending.projectId, taskId: pending.taskId },
					}),
				);
				const historySelectionUnchanged =
					selectedVersionPublicIdRef.current === pending.selectedVersionPublicId;
				const previewUpdated =
					historySelectionUnchanged &&
					filePreviewActions.applyLatestProjectFileVersion({
						projectId: pending.projectId,
						expectedPublicId: pending.expectedPreviewPublicId,
						version: change.latest,
						versionCount: change.versionCount,
					});
				if (previewUpdated) {
					setSelectedVersionKey("");
					setSelectedVersionPublicId("");
					toast.success(`已生成 V${change.latest.version_no}，预览已切换到最新版本`);
					return;
				}
				toast.success(`已生成 V${change.latest.version_no}`);
			} catch (error) {
				if (controller.signal.aborted) return;
				setPendingVersionSync(null);
				console.error("Sync DOCX version error:", error);
				toast.error("新版本已生成，但自动刷新预览失败");
			}
		}

		void syncLatestVersion();
		return () => controller.abort();
	}, [isGenerating, pendingVersionSync]);

	useEffect(() => {
		if (!open) return;

		const handlePointerDown = (event: PointerEvent) => {
			const target = event.target;
			if (!(target instanceof Element)) return;
			if (drawerRef.current?.contains(target)) return;
			if (target.closest("[data-file-preview-trigger]")) return;
			if (target.closest("[data-docx-selection-toolbar]")) return;
			onOpenChange(false);
		};

		document.addEventListener("pointerdown", handlePointerDown);
		return () => document.removeEventListener("pointerdown", handlePointerDown);
	}, [open, onOpenChange]);

	useEffect(() => {
		if (!open) return;

		// 预览抽屉打开时支持使用 Escape 快捷键关闭，保持与关闭按钮一致的状态清理路径。
		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.key === "Escape") {
				onOpenChange(false);
			}
		};

		document.addEventListener("keydown", handleKeyDown);
		return () => document.removeEventListener("keydown", handleKeyDown);
	}, [open, onOpenChange]);

	const handleDownload = async () => {
		if (!file) return;
		try {
			const response = await downloadFilePreviewContent(previewFile ?? file);
			const blob = await response.blob();
			const objectUrl = URL.createObjectURL(blob);
			const link = document.createElement("a");
			link.href = objectUrl;
			link.download = previewFile?.name || file.name;
			document.body.appendChild(link);
			link.click();
			link.remove();
			window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
		} catch (err) {
			console.error("Failed to download file preview", err);
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

	const stageSelectionDraft = (suggestedPrompt?: string) => {
		if (!officeSelection || !previewFile) return;
		if (officeSelection.text.length > DOCX_SELECTION_TEXT_LIMIT) {
			toast.info("选区内容过长，请缩小选区后重试");
			return;
		}
		if (!previewFile.versionPublicId && !previewFile.publicId && !previewFile.projectPath) {
			toast.error("当前文件缺少可编辑的文件标识");
			return;
		}
		docxSelectionComposerActions.setDraft({
			file: previewFile,
			selection: officeSelection,
			suggestedPrompt,
			selectedVersionPublicId,
		});
		setOfficeSelection(null);
		window.getSelection()?.removeAllRanges();
	};

	const handlePolish = (action: DocxPolishAction) => {
		stageSelectionDraft(getDocxPolishPrompt(action));
	};

	const selectVersionPublicId = (publicId: string) => {
		const entry = versionEntries.find((candidate) => candidate.version.public_id === publicId);
		setSelectedVersionKey(entry?.key ?? "");
		setSelectedVersionPublicId(publicId);
	};

	if (!open || !file) {
		return null;
	}

	const displayTitle = previewFile?.title || file.title || file.name;
	const selectionToolbarContainer =
		drawerRef.current?.querySelector<HTMLElement>("[data-office-scroll-viewport]") ?? undefined;

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
				<div className="flex min-w-0 items-center gap-2">
					<div className="truncate text-lg font-medium text-[var(--leros-text-strong)]">
						{displayTitle}
					</div>
					{displayedVersionNo && displayedVersionNo > 0 ? (
						<span className="shrink-0 rounded-md bg-[var(--leros-primary-softer)] px-2 py-0.5 text-xs font-semibold text-[var(--leros-primary)]">
							V{displayedVersionNo}
						</span>
					) : null}
					{displayedVersionNo && displayedVersionNo > 0 ? (
						<span className="shrink-0 text-xs text-[var(--leros-text-muted)]">
							{isHistoricalVersion ? "历史版本" : "最新"}
						</span>
					) : null}
					{isHistoricalVersion && file.publicId ? (
						<button
							type="button"
							onClick={() => selectVersionPublicId(file.publicId ?? "")}
							className="shrink-0 text-xs font-medium text-[var(--leros-primary)] hover:underline"
						>
							切换到最新
						</button>
					) : null}
				</div>
				<div className="flex items-center gap-2">
					{canShowHistory ? (
						<button
							type="button"
							onClick={() => setHistoryOpen((value) => !value)}
							className="group relative rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--leros-primary)]/30"
							aria-label="历史版本"
							title={file.versionCount ? `版本历史（${file.versionCount}）` : "版本历史"}
						>
							<History className="size-4" />
							<span className="pointer-events-none absolute right-0 top-full z-30 mt-2 whitespace-nowrap rounded-md bg-[var(--leros-text-strong)] px-2 py-1 text-[11px] font-medium text-white opacity-0 shadow-sm group-hover:opacity-100 group-focus-visible:opacity-100">
								历史版本
							</span>
						</button>
					) : null}
					<button
						type="button"
						onClick={() => void handleDownload()}
						className="group relative rounded-lg p-2 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--leros-primary)]/30"
						aria-label="下载文件"
						title="下载文件"
					>
						<Download className="size-4" />
						<span className="pointer-events-none absolute right-0 top-full z-30 mt-2 whitespace-nowrap rounded-md bg-[var(--leros-text-strong)] px-2 py-1 text-[11px] font-medium text-white opacity-0 shadow-sm group-hover:opacity-100 group-focus-visible:opacity-100">
							下载文件
						</span>
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
			<div className="flex min-h-0 flex-1 overflow-hidden bg-[var(--leros-surface-soft)]">
				<div className="flex min-w-0 flex-1 flex-col overflow-hidden p-6">
					<FilePreviewContent
						fileName={previewFile?.name || file.name}
						displayTitle={displayTitle}
						previewKind={previewKind}
						preview={preview}
						htmlView={htmlView}
						onHtmlViewChange={setHtmlView}
						onOfficeSelectionChange={(selection) =>
							setOfficeSelection(
								selection?.format === "docx" || selection?.format === "pptx" ? selection : null,
							)
						}
					/>
				</div>
				{historyOpen && canShowHistory ? (
					<FileVersionPanel
						currentPublicId={file.publicId ?? ""}
						selectedVersionKey={selectedVersionKey}
						versions={versions}
						loading={versionsLoading}
						error={versionsError}
						onSelect={(entry) => {
							setSelectedVersionKey(entry.key);
							setSelectedVersionPublicId(entry.version.public_id);
						}}
					/>
				) : null}
			</div>
			{officeSelection?.boundingRect &&
			(previewKind === "docx" || previewKind === "pptx") &&
			officeSelection.format === previewKind ? (
				<DocxSelectionToolbar
					anchor={officeSelection.boundingRect}
					portalContainer={selectionToolbarContainer}
					busy={false}
					onPolish={handlePolish}
					onAddToConversation={() => stageSelectionDraft()}
				/>
			) : null}
		</div>
	);
}

function FileVersionPanel({
	currentPublicId,
	selectedVersionKey,
	versions,
	loading,
	error,
	onSelect,
}: {
	currentPublicId: string;
	selectedVersionKey: string;
	versions: BackendProjectFileVersion[];
	loading: boolean;
	error: string | null;
	onSelect: (entry: ReturnType<typeof buildProjectFileVersionEntries>[number]) => void;
}) {
	const entries = buildProjectFileVersionEntries(versions);
	const currentEntry = getCurrentProjectFileVersionEntry(entries, currentPublicId);
	return (
		<aside className="flex w-52 shrink-0 flex-col border-l border-[var(--leros-control-border)] bg-white">
			<div className="border-b border-[var(--leros-control-border)] px-3 py-2.5">
				<div className="text-sm font-semibold text-[var(--leros-text-strong)]">历史记录</div>
				<div className="mt-0.5 text-xs text-[var(--leros-text-muted)]">
					{versions.length > 0 ? `${versions.length} 个版本` : "查看文件版本"}
				</div>
			</div>
			<div className="min-h-0 flex-1 overflow-auto p-1.5">
				{loading ? (
					<div className="flex items-center justify-center py-10 text-xs text-[var(--leros-text-muted)]">
						<LoaderCircle className="mr-2 size-3.5 animate-spin" />
						加载中
					</div>
				) : error ? (
					<div className="px-3 py-8 text-center text-xs text-[var(--leros-danger)]">{error}</div>
				) : versions.length === 0 ? (
					<div className="px-3 py-8 text-center text-xs text-[var(--leros-text-muted)]">
						暂无历史版本
					</div>
				) : (
					<div className="space-y-1">
						{entries.map((entry) => {
							const { version } = entry;
							const isCurrent = entry.key === currentEntry?.key;
							const isSelected = entry.key === selectedVersionKey;
							return (
								<div key={entry.key}>
									<button
										type="button"
										onClick={() => onSelect(entry)}
										className={cn(
											"w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left transition-colors",
											isSelected
												? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]"
												: "hover:bg-[var(--leros-surface-soft)]",
										)}
									>
										<span className="block truncate text-xs font-semibold">
											V{version.version_no}
										</span>
										<span className="mt-0.5 block truncate text-[10px] text-[var(--leros-text-muted)]">
											{formatVersionTime(version.created_at)}
											{isCurrent ? (
												<span className="ml-1.5 rounded-full bg-[var(--leros-primary)]/10 px-1 py-0 text-[9px] text-[var(--leros-primary)]">
													最新
												</span>
											) : null}
										</span>
									</button>
								</div>
							);
						})}
					</div>
				)}
			</div>
		</aside>
	);
}

function formatVersionTime(timestamp?: number): string {
	if (!timestamp) return "-";
	return new Intl.DateTimeFormat("zh-CN", {
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
	}).format(new Date(timestamp * 1000));
}

function FilePreviewContent({
	fileName,
	displayTitle,
	previewKind,
	preview,
	htmlView,
	onHtmlViewChange,
	onOfficeSelectionChange,
}: {
	fileName: string;
	displayTitle: string;
	previewKind: FilePreviewKind;
	preview: FilePreviewState;
	htmlView: "preview" | "source";
	onHtmlViewChange: (view: "preview" | "source") => void;
	onOfficeSelectionChange: (selection: OfficeTextSelection | null) => void;
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
				<OfficePreview
					buffer={preview.buffer}
					fileName={fileName}
					format={previewKind}
					onTextSelectionChange={onOfficeSelectionChange}
				/>
			</div>
		);
	}

	if (previewKind === "spreadsheet" && preview.buffer) {
		return (
			<div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm">
				<SpreadsheetPreview buffer={preview.buffer} fileName={fileName} />
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

	if (previewKind === "html" && preview.text !== undefined) {
		return (
			<div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl bg-white shadow-sm">
				<div className="flex shrink-0 items-center justify-between border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-3 py-2">
					<div className="flex items-center gap-1">
						{(["preview", "source"] as const).map((view) => (
							<button
								key={view}
								type="button"
								onClick={() => onHtmlViewChange(view)}
								className={cn(
									"rounded-md px-3 py-1.5 text-xs transition-colors",
									htmlView === view
										? "bg-white font-medium text-[var(--leros-text-strong)] shadow-sm"
										: "text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]",
								)}
							>
								{view === "preview" ? "预览" : "源码"}
							</button>
						))}
					</div>
					{htmlView === "preview" && (
						<span
							title="当前仅支持基础静态预览，包含脚本或复杂交互的页面请下载后用浏览器打开。"
							className="inline-flex items-center gap-1 text-[11px] text-[var(--leros-text-muted)]"
						>
							<ShieldCheck className="size-3.5 text-emerald-600" />
							安全预览
						</span>
					)}
				</div>
				{htmlView === "preview" && preview.objectUrl ? (
					<iframe
						title={`${displayTitle} 预览`}
						src={preview.objectUrl}
						sandbox=""
						referrerPolicy="no-referrer"
						className="min-h-0 flex-1 border-0 bg-white"
					/>
				) : (
					<pre className="min-h-0 flex-1 overflow-auto bg-slate-950 p-4 text-xs leading-6 text-slate-100">
						{preview.text}
					</pre>
				)}
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
					alt={displayTitle}
					className="max-h-full max-w-full object-contain"
				/>
			</div>
		);
	}

	if (previewKind === "pdf" && preview.buffer) {
		return (
			<div className="min-h-0 flex-1 overflow-hidden rounded-xl bg-white shadow-sm">
				<PdfPreview
					buffer={preview.buffer}
					fileName={fileName}
					onTextSelectionChange={printPdfTextSelection}
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

function printPdfTextSelection(selection: PdfTextSelection | null): void {
	if (!selection) return;
	console.info("[PdfPreview] 选区文本与位置", {
		text: selection.text,
		surface: {
			kind: selection.surfaceKind,
			index: selection.surfaceIndex,
		},
		boundingRect: selection.boundingRect,
		rects: selection.rects,
		segments: selection.segments,
	});
}
