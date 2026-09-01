"use client";

import { getComposerUploadAccept, type Project, projectFileApi } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Checkbox } from "@leros/ui/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Select, SelectContent, SelectItem, SelectTrigger } from "@leros/ui/components/ui/select";
import { cn } from "@leros/ui/lib/utils";
import { Eye, FileText, FolderOpen, LoaderCircle, Search, Upload, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { BidComparisonIcon } from "../../assets";
import { ListLoadMoreSentinel } from "../common/ListLoadMoreSentinel";
import { filePreviewActions } from "../layout/file-preview-store";
import { ProjectTaskPickerField } from "../layout/ProjectTaskPicker";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";
import {
	collectSelectableFiles,
	type ProjectFileNode,
	parseProjectFileList,
} from "../layout/project-files";
import { usePaginatedProjectList } from "../project/usePaginatedProjectList";

export type BidComparisonProjectFile = {
	name: string;
	previewUrl?: string;
	mimeType?: string;
	publicId?: string;
	storageUri?: string;
	projectId?: string;
	projectPath?: string;
	/** 本地上传暂存，开始对比时上传后清空 */
	file?: File;
	size?: number;
};

export type BidComparisonConfig = {
	projectId?: string;
	/** 选中已有任务时续聊；空表示在该项目下新建任务 */
	taskId?: string;
	mainFile?: BidComparisonProjectFile;
	compareFiles: BidComparisonProjectFile[];
	reportFormat: "Word" | "PDF" | "PPT" | "MD";
	comparisonRequirements: string;
};

type BidComparisonProjectOption = Pick<Project, "id" | "name" | "tasks">;

const REPORT_FORMATS: BidComparisonConfig["reportFormat"][] = [
	"Word",
	// 中文注释：PDF/PPT 报告效果不佳，暂时从弹窗入口隐藏。
	// "PDF",
	// "PPT",
	"MD",
];

/** 中文注释：用项目路径/publicId 区分同名文件，本地上传用 upload: 前缀。 */
function fileSelectionKey(file: BidComparisonProjectFile): string {
	if (file.projectId) {
		return `${file.projectId}:${file.publicId || file.projectPath || file.name}`;
	}
	return `upload:${file.name}`;
}

function isProjectSourcedFile(file: BidComparisonProjectFile): boolean {
	return Boolean(file.projectId);
}

export function BidComparisonConfigDialog({
	open,
	onOpenChange,
	onSave,
	projects = [],
	initialProjectId,
	initialTaskId,
	onProjectChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onSave: (config: BidComparisonConfig) => void | Promise<void>;
	projects?: BidComparisonProjectOption[];
	initialProjectId?: string | null;
	initialTaskId?: string | null;
	onProjectChange?: (projectId: string) => void;
}) {
	const [targetProjectId, setTargetProjectId] = useState(initialProjectId ?? "");
	const [targetTaskId, setTargetTaskId] = useState(initialTaskId ?? "");
	const [mainFile, setMainFile] = useState<BidComparisonProjectFile | undefined>();
	const [compareFiles, setCompareFiles] = useState<BidComparisonProjectFile[]>([]);
	const [reportFormat, setReportFormat] = useState<BidComparisonConfig["reportFormat"]>("Word");
	const [comparisonRequirements, setComparisonRequirements] = useState("");
	const [filePicker, setFilePicker] = useState<"main" | "compare" | null>(null);
	const [saving, setSaving] = useState(false);
	const uploadAccept = getComposerUploadAccept(
		typeof navigator === "undefined" ? undefined : navigator.platform,
	);
	const mainInputRef = useRef<HTMLInputElement>(null);
	const compareInputRef = useRef<HTMLInputElement>(null);

	useEffect(() => {
		if (!open) return;
		setTargetProjectId(initialProjectId ?? "");
		setTargetTaskId(initialTaskId ?? "");
		setMainFile(undefined);
		setCompareFiles([]);
		setReportFormat("Word");
		setComparisonRequirements("");
	}, [initialProjectId, initialTaskId, open]);

	useEffect(() => {
		if (open && targetProjectId) {
			onProjectChange?.(targetProjectId);
		}
	}, [onProjectChange, open, targetProjectId]);

	const dialogDescription = "选择项目/任务与文件后，开始标书对比";

	const chooseUploadedFiles = (files: FileList | null, kind: "main" | "compare") => {
		const selectedFiles = Array.from(files ?? []).map(
			(file) =>
				({
					name: file.name,
					mimeType: file.type,
					previewUrl: URL.createObjectURL(file),
					file,
					size: file.size,
				}) satisfies BidComparisonProjectFile,
		);
		if (!selectedFiles.length) return;
		if (kind === "main") {
			setMainFile(selectedFiles[0]);
			return;
		}
		setCompareFiles((current) => {
			const next = [...current];
			for (const file of selectedFiles) {
				const key = fileSelectionKey(file);
				const index = next.findIndex((item) => fileSelectionKey(item) === key);
				if (index >= 0) next[index] = file;
				else if (next.length < 10) next.push(file);
			}
			return next;
		});
	};

	const selectProjectFiles = (files: BidComparisonProjectFile[]) => {
		if (filePicker === "main") {
			if (files[0]) setMainFile(files[0]);
		} else if (filePicker === "compare") {
			// 中文注释：本地上传保留；项目文件以本次勾选结果为准，避免按文件名去重导致数量不一致。
			const uploads = compareFiles.filter((file) => !isProjectSourcedFile(file));
			const merged = [...uploads];
			for (const file of files) {
				if (merged.length >= 10) break;
				merged.push(file);
			}
			setCompareFiles(merged);
		}
		setFilePicker(null);
	};

	const save = async () => {
		if (saving) return;
		setSaving(true);
		try {
			await onSave({
				projectId: targetProjectId || undefined,
				taskId: targetTaskId || undefined,
				mainFile,
				compareFiles,
				reportFormat,
				comparisonRequirements,
			});
			onOpenChange(false);
		} finally {
			setSaving(false);
		}
	};

	const canPreview = (file: BidComparisonProjectFile) =>
		Boolean(file.previewUrl || file.publicId || file.storageUri);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="flex max-h-[min(92dvh,880px)] max-w-[min(92vw,560px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogHeader className="shrink-0 border-b border-slate-100 px-7 py-5">
					<div className="flex items-center gap-3">
						<div className="flex size-9 items-center justify-center rounded-xl bg-[var(--leros-primary-soft)] text-[var(--leros-primary)]">
							<BidComparisonIcon className="size-5" />
						</div>
						<div>
							<DialogTitle className="text-base">标书对比</DialogTitle>
							<DialogDescription className="mt-1 text-xs">{dialogDescription}</DialogDescription>
						</div>
					</div>
				</DialogHeader>

				<div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-7 py-5">
					<ProjectTaskPickerField
						projectId={targetProjectId}
						taskId={targetTaskId}
						allowNewProject
						allowSelectTask
						onLoadProjectTasks={onProjectChange}
						onSelect={(nextProjectId, nextTaskId) => {
							setTargetProjectId(nextProjectId);
							setTargetTaskId(nextTaskId);
							if (nextProjectId) onProjectChange?.(nextProjectId);
						}}
					/>
					<FilePickerSection
						title="投标文件"
						required
						limitText="只能选择 1 份文件"
						files={mainFile ? [mainFile] : []}
						canPreview={canPreview}
						onUpload={() => mainInputRef.current?.click()}
						onChooseProjectFile={() => setFilePicker("main")}
						onPreview={(file) => openPreview(file)}
						onRemove={() => setMainFile(undefined)}
					/>
					<FilePickerSection
						title="对比文件"
						required
						limitText={`已选择 ${compareFiles.length}/10 份文件`}
						files={compareFiles}
						canPreview={canPreview}
						onUpload={() => compareInputRef.current?.click()}
						onChooseProjectFile={() => setFilePicker("compare")}
						onPreview={(file) => openPreview(file)}
						onRemove={(file) =>
							setCompareFiles((current) =>
								current.filter((item) => fileSelectionKey(item) !== fileSelectionKey(file)),
							)
						}
					/>
					<div>
						<div className="mb-2 block text-sm font-semibold text-slate-800">
							生成的对比报告格式
						</div>
						<div className="grid grid-cols-2 gap-2">
							{REPORT_FORMATS.map((format) => (
								<button
									key={format}
									type="button"
									onClick={() => setReportFormat(format)}
									className={cn(
										"rounded-xl border px-3 py-2.5 text-sm font-medium transition-colors",
										reportFormat === format
											? "border-[var(--leros-primary)] bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]"
											: "border-slate-200 text-slate-500 hover:border-[var(--leros-primary-soft)] hover:bg-[var(--leros-primary-softer)]/50",
									)}
								>
									{format}
								</button>
							))}
						</div>
					</div>
					<div>
						<label
							htmlFor="bid-comparison-notes"
							className="mb-2 block text-sm font-semibold text-slate-800"
						>
							对比要求 <span className="font-normal text-xs text-slate-400">选填</span>
						</label>
						<textarea
							id="bid-comparison-notes"
							value={comparisonRequirements}
							onChange={(event) => setComparisonRequirements(event.target.value)}
							placeholder="例如：重点关注技术方案、商务条款和评分标准的差异"
							className="min-h-24 w-full resize-y rounded-xl border border-slate-200 px-3.5 py-3 text-sm outline-none transition-colors placeholder:text-slate-300 focus:border-[var(--leros-primary)] focus:ring-2 focus:ring-[var(--leros-primary-softer)]"
						/>
					</div>
				</div>

				<DialogFooter className="shrink-0 border-t border-slate-100 px-7 py-4">
					<Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<Button
						type="button"
						disabled={saving || !mainFile || compareFiles.length === 0}
						onClick={() => void save()}
						className="bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90"
					>
						{saving ? "启动中..." : "开始对比"}
					</Button>
				</DialogFooter>

				<input
					ref={mainInputRef}
					type="file"
					className="hidden"
					accept={uploadAccept}
					onChange={(event) => chooseUploadedFiles(event.target.files, "main")}
				/>
				<input
					ref={compareInputRef}
					type="file"
					className="hidden"
					accept={uploadAccept}
					multiple
					onChange={(event) => chooseUploadedFiles(event.target.files, "compare")}
				/>
				{filePicker ? (
					<ProjectFilePicker
						open
						mode={filePicker}
						projects={projects}
						initialSelected={
							filePicker === "main"
								? mainFile && isProjectSourcedFile(mainFile)
									? [mainFile]
									: []
								: compareFiles.filter(isProjectSourcedFile)
						}
						reservedCount={
							filePicker === "compare"
								? compareFiles.filter((file) => !isProjectSourcedFile(file)).length
								: 0
						}
						onConfirm={selectProjectFiles}
						onClose={() => setFilePicker(null)}
					/>
				) : null}
			</DialogContent>
		</Dialog>
	);

	function openPreview(file: BidComparisonProjectFile) {
		filePreviewActions.open({
			name: file.name,
			title: file.name,
			mimeType: file.mimeType,
			url: file.previewUrl,
			publicId: file.publicId,
			storageUri: file.storageUri,
			projectId: file.projectId,
			projectPath: file.projectPath,
		});
	}
}

function FilePickerSection({
	title,
	required,
	limitText,
	files,
	canPreview,
	onUpload,
	onChooseProjectFile,
	onPreview,
	onRemove,
}: {
	title: string;
	required?: boolean;
	limitText: string;
	files: BidComparisonProjectFile[];
	canPreview: (file: BidComparisonProjectFile) => boolean;
	onUpload: () => void;
	onChooseProjectFile: () => void;
	onPreview: (file: BidComparisonProjectFile) => void;
	onRemove: (file: BidComparisonProjectFile) => void;
}) {
	return (
		<div>
			<div className="mb-2 flex items-end justify-between">
				<div className="text-sm font-semibold text-slate-800">
					{title} {required ? <span className="text-red-500">*</span> : null}
				</div>
				<span className="text-xs text-slate-400">{limitText}</span>
			</div>
			<div className="rounded-xl border border-dashed border-slate-200 bg-slate-50/60 p-3">
				{files.length ? (
					<div className="space-y-2">
						{files.map((file) => (
							<div
								key={fileSelectionKey(file)}
								className="flex items-center gap-2 rounded-lg bg-white px-3 py-2 text-sm shadow-sm"
							>
								<FileText className="size-4 shrink-0 text-[var(--leros-primary)]" />
								<span className="min-w-0 flex-1 truncate text-slate-700">{file.name}</span>
								{canPreview(file) ? (
									<button
										type="button"
										onClick={() => onPreview(file)}
										className="rounded-md p-1 text-slate-400 hover:bg-[var(--leros-primary-softer)] hover:text-[var(--leros-primary)]"
										title="预览文件"
									>
										<Eye className="size-4" />
									</button>
								) : null}
								<button
									type="button"
									onClick={() => onRemove(file)}
									className="text-slate-400 hover:text-slate-700"
								>
									<X className="size-4" />
								</button>
							</div>
						))}
					</div>
				) : (
					<div className="py-3 text-center text-xs text-slate-400">
						上传文件或从项目文件树中选择
					</div>
				)}
				<div className="mt-3 flex gap-2">
					<Button type="button" size="sm" variant="outline" onClick={onUpload}>
						<Upload className="size-3.5" />
						上传文件
					</Button>
					<Button type="button" size="sm" variant="outline" onClick={onChooseProjectFile}>
						<FolderOpen className="size-3.5" />
						项目文件
					</Button>
				</div>
			</div>
		</div>
	);
}

export function ProjectFilePicker({
	open,
	mode,
	initialSelected = [],
	reservedCount = 0,
	maxCountOverride,
	titleOverride,
	onConfirm,
	onClose,
}: {
	open: boolean;
	mode: "main" | "compare";
	projects?: BidComparisonProjectOption[];
	initialSelected?: BidComparisonProjectFile[];
	/** 中文注释：已占用的本地上传名额，对比文件总上限仍为 10。 */
	reservedCount?: number;
	maxCountOverride?: number;
	titleOverride?: string;
	onConfirm: (files: BidComparisonProjectFile[]) => void;
	onClose: () => void;
}) {
	const maxCount = maxCountOverride ?? (mode === "main" ? 1 : Math.max(0, 10 - reservedCount));
	const title = titleOverride ?? (mode === "main" ? "选择投标文件" : "选择对比文件");
	const projectList = usePaginatedProjectList({ enabled: open });
	const projects = projectList.projects;
	const [selectRoot, setSelectRoot] = useState<HTMLDivElement | null>(null);
	const [selectedProjectId, setSelectedProjectId] = useState("");
	const [projectTrees, setProjectTrees] = useState<Record<string, ProjectFileNode[]>>({});
	const [loading, setLoading] = useState(false);
	const [loadError, setLoadError] = useState("");
	const [searchKeyword, setSearchKeyword] = useState("");
	const [selectedMap, setSelectedMap] = useState<Record<string, BidComparisonProjectFile>>({});
	const initialSelectedRef = useRef(initialSelected);
	initialSelectedRef.current = initialSelected;

	const selectedProject = projects.find((project) => project.id === selectedProjectId);

	useEffect(() => {
		if (!open) return;
		if (!selectedProjectId && projects[0]?.id) {
			setSelectedProjectId(projects[0].id);
		}
	}, [open, projects, selectedProjectId]);
	const selectedFiles = useMemo(() => Object.values(selectedMap), [selectedMap]);
	const totalSelectedCount = selectedFiles.length + (mode === "compare" ? reservedCount : 0);
	const totalMaxCount = mode === "main" ? 1 : maxCount + reservedCount;
	const projectFiles = useMemo(() => {
		const nodes = projectTrees[selectedProjectId] ?? [];
		return collectSelectableFiles(nodes);
	}, [projectTrees, selectedProjectId]);
	const filteredFiles = useMemo(() => {
		const keyword = searchKeyword.trim().toLowerCase();
		if (!keyword) return projectFiles;
		return projectFiles.filter((file) => file.name.toLowerCase().includes(keyword));
	}, [projectFiles, searchKeyword]);

	useEffect(() => {
		if (!selectedProjectId && projects[0]?.id) {
			setSelectedProjectId(projects[0].id);
		}
	}, [selectedProjectId, projects]);

	useEffect(() => {
		if (!open || !selectedProjectId) return;
		if (projectTrees[selectedProjectId]) return;

		let cancelled = false;
		setLoading(true);
		setLoadError("");
		void projectFileApi
			.list({ projectId: selectedProjectId })
			.then((response) => {
				if (cancelled) return;
				setProjectTrees((current) => ({
					...current,
					[selectedProjectId]: parseProjectFileList(response.data.data),
				}));
			})
			.catch((error) => {
				if (cancelled) return;
				console.error("BidComparison load project files error:", error);
				setLoadError("加载失败，请重试");
			})
			.finally(() => {
				if (!cancelled) setLoading(false);
			});

		return () => {
			cancelled = true;
		};
	}, [open, projectTrees, selectedProjectId]);

	useEffect(() => {
		// 中文注释：仅清空搜索与错误态，不清理已选，支持对比文件跨项目累计勾选。
		setSearchKeyword("");
		setLoadError("");
	}, [selectedProjectId]);

	useEffect(() => {
		if (!open) return;
		// 中文注释：仅在打开时回填已选，避免父组件重渲染用新数组引用清空勾选。
		const next: Record<string, BidComparisonProjectFile> = {};
		for (const file of initialSelectedRef.current) {
			next[fileSelectionKey(file)] = file;
		}
		setSelectedMap(next);
		setSearchKeyword("");
	}, [mode, open]);

	const fileKey = (projectId: string, file: Pick<ProjectFileNode, "publicId" | "path">) =>
		`${projectId}:${file.publicId || file.path}`;

	const toggleFile = (file: ProjectFileNode) => {
		const key = fileKey(selectedProjectId, file);
		setSelectedMap((current) => {
			if (current[key]) {
				const next = { ...current };
				delete next[key];
				return next;
			}
			const nextFile: BidComparisonProjectFile = {
				name: file.name,
				mimeType: file.mimeType,
				publicId: file.publicId,
				storageUri: file.storageUri,
				projectId: selectedProjectId,
				projectPath: file.path,
				size: file.size,
			};
			if (mode === "main") return { [key]: nextFile };
			if (Object.keys(current).length >= maxCount) return current;
			return { ...current, [key]: nextFile };
		});
	};

	const confirm = () => {
		onConfirm(selectedFiles);
	};

	const previewFile = (file: ProjectFileNode) => {
		filePreviewActions.open({
			name: file.name,
			title: file.name,
			mimeType: file.mimeType,
			publicId: file.publicId,
			storageUri: file.storageUri,
			projectId: selectedProjectId,
			projectPath: file.path,
		});
	};

	return (
		<Dialog open={open} onOpenChange={(next) => !next && onClose()}>
			<DialogContent className="flex h-[min(88dvh,640px)] max-w-[min(94vw,520px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
				<DialogHeader className="shrink-0 border-b border-slate-100 px-6 py-5">
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription className="mt-1">
						{projects.length === 0
							? "当前账号暂无项目，请先创建项目后再选择文件"
							: selectedProject
								? `从 ${selectedProject.name} 的项目文件中选择`
								: "请先选择项目，再从项目文件中选择"}
					</DialogDescription>
				</DialogHeader>

				<div className="shrink-0 space-y-3 border-b border-slate-100 px-6 py-4">
					<div>
						<div className="mb-1.5 text-xs font-medium text-slate-500">选择项目</div>
						<Select
							value={selectedProjectId || null}
							disabled={projects.length === 0}
							onValueChange={(value) => setSelectedProjectId(value ?? "")}
						>
							<SelectTrigger className="!h-10 w-full min-w-0 rounded-xl border border-slate-200 bg-white px-3 text-sm text-slate-700 shadow-none disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400">
								<span className="min-w-0 flex-1 truncate text-left">
									{projects.length === 0 ? "暂无可用项目" : selectedProject?.name || "请选择项目"}
								</span>
							</SelectTrigger>
							{projects.length > 0 ? (
								<SelectContent
									ref={setSelectRoot}
									align="start"
									side="bottom"
									className="z-[70] max-h-64 min-w-[var(--anchor-width)] overflow-y-auto rounded-xl border border-slate-200 bg-white p-1 shadow-lg"
								>
									{projects.map((project) => (
										<SelectItem
											key={project.id}
											value={project.id}
											className="rounded-lg px-3 py-2 text-sm"
										>
											{project.name}
										</SelectItem>
									))}
									<ListLoadMoreSentinel
										hasMore={projectList.hasMore}
										loading={
											projectList.loadingMore || (projectList.loading && projects.length === 0)
										}
										onLoadMore={projectList.loadMore}
										root={selectRoot}
										className="py-2"
									/>
								</SelectContent>
							) : null}
						</Select>
					</div>
					<div className="relative">
						<Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-slate-300" />
						<input
							value={searchKeyword}
							onChange={(event) => setSearchKeyword(event.target.value)}
							placeholder="搜索项目文件"
							disabled={projects.length === 0 || !selectedProjectId}
							className="h-10 w-full rounded-xl border border-slate-200 bg-white pr-3 pl-9 text-sm outline-none transition-colors placeholder:text-slate-300 focus:border-[var(--leros-primary)] focus:ring-2 focus:ring-[var(--leros-primary-softer)] disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
						/>
					</div>
				</div>

				<div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
					{projects.length === 0 ? (
						<div className="flex h-full flex-col items-center justify-center gap-2 text-xs text-slate-400">
							<FolderOpen className="size-6" />
							暂无可用项目
						</div>
					) : !selectedProjectId ? (
						<div className="flex h-full flex-col items-center justify-center gap-2 text-xs text-slate-400">
							<FolderOpen className="size-6" />
							请先选择项目
						</div>
					) : loading ? (
						<div className="flex h-full items-center justify-center gap-2 text-xs text-slate-400">
							<LoaderCircle className="size-4 animate-spin" />
							加载项目文件中...
						</div>
					) : loadError ? (
						<div className="flex h-full items-center justify-center text-xs text-rose-500">
							{loadError}
						</div>
					) : filteredFiles.length ? (
						<div className="space-y-1">
							{filteredFiles.map((file) => {
								const id = fileKey(selectedProjectId, file);
								const checked = Boolean(selectedMap[id]);
								return (
									// biome-ignore lint/a11y/useSemanticElements: 文件行内包含独立的预览按钮，不能改为嵌套 button
									<div
										key={id}
										className={cn(
											"flex w-full items-center gap-1 rounded-xl px-3 py-2.5 transition-colors",
											checked ? "bg-[var(--leros-primary-softer)]" : "hover:bg-slate-50",
										)}
									>
										<button
											type="button"
											onClick={() => toggleFile(file)}
											className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 text-left"
										>
											<Checkbox
												checked={checked}
												tabIndex={-1}
												className="pointer-events-none data-checked:border-[var(--leros-primary)] data-checked:bg-[var(--leros-primary)]"
											/>
											<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-white shadow-sm ring-1 ring-slate-200/70">
												<ProjectFileTypeIcon
													fileName={file.name}
													className="size-5 object-contain"
												/>
											</div>
											<div className="min-w-0 flex-1">
												<div className="truncate text-sm font-medium text-slate-800">
													{file.name}
												</div>
												<div className="mt-0.5 truncate text-xs text-slate-400">
													{[
														file.size > 0 ? formatBytes(file.size) : "",
														file.createdAt ? formatFileTime(file.createdAt) : "",
													]
														.filter(Boolean)
														.join(" · ") || "项目文件"}
												</div>
											</div>
										</button>
										<button
											type="button"
											onClick={() => previewFile(file)}
											className="relative z-10 rounded-md p-1.5 text-slate-400 hover:bg-white hover:text-[var(--leros-primary)]"
											title="预览文件"
										>
											<Eye className="size-4" />
										</button>
									</div>
								);
							})}
						</div>
					) : (
						<div className="flex h-full flex-col items-center justify-center gap-2 text-xs text-slate-400">
							<FolderOpen className="size-6" />
							{searchKeyword.trim() ? "没有匹配的项目文件" : "该项目暂无可选文件"}
						</div>
					)}
				</div>

				<DialogFooter className="shrink-0 border-t border-slate-100 px-6 py-4 sm:items-center sm:justify-between">
					<div className="text-sm text-slate-500">
						已选择{" "}
						{Number.isFinite(totalMaxCount)
							? `${totalSelectedCount}/${totalMaxCount}`
							: totalSelectedCount}
					</div>
					<div className="flex gap-2">
						<Button type="button" variant="ghost" onClick={onClose}>
							取消
						</Button>
						<Button
							type="button"
							disabled={selectedFiles.length === 0}
							onClick={confirm}
							className="bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90"
						>
							确定
						</Button>
					</div>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function formatBytes(size: number): string {
	if (!size) return "";
	if (size < 1024) return `${size} B`;
	if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
	if (size < 1024 * 1024 * 1024) return `${(size / (1024 * 1024)).toFixed(1)} MB`;
	return `${(size / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

function formatFileTime(timestamp: number): string {
	if (!timestamp) return "";
	const date = new Date(timestamp);
	const now = new Date();
	const sameDay =
		date.getFullYear() === now.getFullYear() &&
		date.getMonth() === now.getMonth() &&
		date.getDate() === now.getDate();
	const time = new Intl.DateTimeFormat("zh-CN", {
		hour: "2-digit",
		minute: "2-digit",
		hour12: false,
	}).format(date);
	if (sameDay) return `今天 ${time}`;
	return new Intl.DateTimeFormat("zh-CN", {
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		hour12: false,
	}).format(date);
}
