"use client";

import { projectFileApi, useLayoutStore } from "@leros/store";
import type { Attachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { getRequestErrorMessage } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import { Calculator, FileSpreadsheet, FolderOpen, Upload, X } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { BidComparisonIcon } from "../../assets";
import { useAuth } from "../auth";
import {
	type BidComparisonConfig,
	BidComparisonConfigDialog,
	type BidComparisonProjectFile,
	ProjectFilePicker,
} from "../input/BidComparisonConfigDialog";
import {
	bidComparisonConfigToAttachments,
	bidComparisonOutputFormat,
	bidComparisonPrompt,
	ensureBidComparisonFilesUploaded,
} from "../input/bidComparisonAttachments";
import type { AppNavigation } from "./LeftRail";
import { ProjectTaskPickerField } from "./ProjectTaskPicker";

type SelectedFile = {
	id: string;
	file?: File;
	name: string;
	publicId?: string;
	projectId?: string;
	storageUri?: string;
	mimeType?: string;
	size: number;
	role: "roster" | "historical_payroll" | "attendance";
};

function payrollStarterPrompt(files: SelectedFile[]): string {
	const names = (role: SelectedFile["role"]) =>
		files
			.filter((file) => file.role === role)
			.map((file) => file.name)
			.join("、") || "未指定";
	return [
		"请开展考勤工资核算。",
		`人员底表：${names("roster")}`,
		`历史工资表：${names("historical_payroll")}`,
		`当月考勤表：${names("attendance")}`,
	].join("\n");
}

export function WorkbenchPage({ navigation }: { navigation?: AppNavigation }) {
	const { projects, fetchProjects, fetchTasks, sendWorkbenchMessage } = useLayoutStore((s) => s);
	const { isAuthenticated, requireAuth } = useAuth();
	const [dialogOpen, setDialogOpen] = useState(false);
	const [bidComparisonOpen, setBidComparisonOpen] = useState(false);
	const [projectId, setProjectId] = useState("");
	const [taskId, setTaskId] = useState("");
	const [files, setFiles] = useState<SelectedFile[]>([]);
	const [submitting, setSubmitting] = useState(false);
	const [filePickerRole, setFilePickerRole] = useState<SelectedFile["role"] | null>(null);
	const rosterInputRef = useRef<HTMLInputElement>(null);
	const historicalInputRef = useRef<HTMLInputElement>(null);
	const attendanceInputRef = useRef<HTMLInputElement>(null);

	const projectOptions = useMemo(
		() => projects.map((project) => ({ id: project.id, name: project.name, tasks: project.tasks })),
		[projects],
	);

	const openDialog = () => {
		requireAuth(() => {
			void fetchProjects();
			setDialogOpen(true);
		});
	};

	const openBidComparison = () => {
		requireAuth(() => {
			void fetchProjects();
			setBidComparisonOpen(true);
		});
	};

	const startBidComparison = async (config: BidComparisonConfig) => {
		try {
			const resolved = await ensureBidComparisonFilesUploaded(config, config.projectId);
			const result = await sendWorkbenchMessage(
				bidComparisonPrompt(resolved),
				resolved.projectId,
				"default",
				bidComparisonConfigToAttachments(resolved),
				undefined,
				undefined,
				undefined,
				"bid_comparison",
				bidComparisonOutputFormat(resolved),
				resolved.taskId ?? null,
			);
			if (!result?.project_id || !result.task_id || !result.session_id) {
				throw new Error("启动标书对比失败，请确认所选任务未在回复中后重试");
			}
			navigation?.goToTaskDetail(result.project_id, result.task_id, result.session_id);
		} catch (error) {
			console.error("WorkbenchPage start bid comparison error:", error);
			toast.error(getRequestErrorMessage(error) ?? "启动标书对比失败");
			throw error;
		}
	};

	const resetDialog = () => {
		setProjectId("");
		setTaskId("");
		setFiles([]);
	};

	const addFiles = (selected: FileList | null, role: SelectedFile["role"]) => {
		const nextFiles = Array.from(selected ?? []);
		if (!nextFiles.length) return;
		setFiles((current) => {
			const existing = new Set(
				current
					.filter((item) => item.file)
					.map(({ file }) => `${file?.name}:${file?.size}:${file?.lastModified}`),
			);
			const additions = nextFiles
				.filter((file) => {
					const key = `${file.name}:${file.size}:${file.lastModified}`;
					if (existing.has(key)) return false;
					existing.add(key);
					return true;
				})
				.map((file) => ({
					id: `payroll-${crypto.randomUUID()}`,
					file,
					name: file.name,
					mimeType: file.type,
					size: file.size,
					role,
				}));
			return [...current, ...additions];
		});
	};

	const uploadFiles = async (): Promise<Attachment[]> => {
		const attachments: Attachment[] = [];
		for (const selected of files) {
			if (selected.publicId) {
				attachments.push({
					id: selected.id,
					type: "file",
					name: selected.name,
					size: selected.size,
					fileUploadId: selected.publicId,
					mimeType: selected.mimeType,
					storageUri: selected.storageUri,
					uploadStatus: "completed",
					attachmentRole: selected.role,
				});
				continue;
			}
			if (!selected.file) throw new Error(`文件「${selected.name}」缺少内容`);
			const response = projectId
				? await projectFileApi.upload({
						projectId,
						projectPublicId: projectId,
						file: selected.file,
					})
				: await projectFileApi.uploadLoose({
						file: selected.file,
						purpose: "attachment",
						withLocalPath: true,
					});
			const payload = response.data;
			const fileUploadId = payload.public_id?.trim();
			if (!fileUploadId) throw new Error(`文件「${selected.name}」上传失败`);
			attachments.push({
				id: selected.id,
				type: selected.file.type.startsWith("image/") ? "image" : "file",
				name: payload.original_name || payload.filename || selected.name,
				size: payload.file_size ?? payload.size ?? selected.size,
				fileUploadId,
				mimeType: payload.mime_type || selected.file.type,
				storageUri: payload.storage_uri,
				uploadStatus: "completed",
				attachmentRole: selected.role,
			});
		}
		return attachments;
	};

	const openProjectFilePicker = (role: SelectedFile["role"]) => {
		if (!projects.length) {
			toast.info("当前账号暂无项目，请先创建项目");
			return;
		}
		setFilePickerRole(role);
	};

	const selectProjectFiles = (selected: BidComparisonProjectFile[]) => {
		if (!filePickerRole) return;
		const role = filePickerRole;
		setFiles((current) => {
			const uploads = current.filter((file) => file.role === role && !file.publicId);
			const projectFiles = selected.map((file) => ({
				id: `payroll-project-${file.publicId}`,
				name: file.name,
				publicId: file.publicId,
				projectId: file.projectId,
				storageUri: file.storageUri,
				mimeType: file.mimeType,
				size: file.size ?? 0,
				role,
			}));
			return [...current.filter((file) => file.role !== role), ...uploads, ...projectFiles];
		});
		setFilePickerRole(null);
	};

	const startAnalysis = async () => {
		if (
			!files.some((file) => file.role === "roster") ||
			!files.some((file) => file.role === "historical_payroll") ||
			!files.some((file) => file.role === "attendance") ||
			submitting
		)
			return;
		setSubmitting(true);
		try {
			const attachments = await uploadFiles();
			const result = await sendWorkbenchMessage(
				payrollStarterPrompt(files),
				projectId || undefined,
				"default",
				attachments,
				undefined,
				undefined,
				undefined,
				"salary_accounting",
				undefined,
				taskId || null,
			);
			if (!result?.project_id || !result.task_id || !result.session_id) {
				throw new Error("创建工资核算任务失败");
			}
			setDialogOpen(false);
			resetDialog();
			navigation?.goToTaskDetail(result.project_id, result.task_id, result.session_id);
		} catch (error) {
			console.error("WorkbenchPage start analysis error:", error);
			toast.error(error instanceof Error ? error.message : "启动工资核算分析失败");
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<div className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]">
			<header className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-5">
				<h1 className="text-xl font-semibold text-[var(--leros-text-strong)]">工作台</h1>
				<p className="mt-2 text-sm text-[var(--leros-text-muted)]">
					选择业务功能，快速开始处理工作资料。
				</p>
			</header>
			<main className="flex min-h-0 flex-1 flex-col px-6 py-6">
				<div className="w-full">
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
						<button
							type="button"
							onClick={openBidComparison}
							className={cn(
								"group flex min-h-[132px] cursor-pointer flex-col rounded-lg border border-slate-200 bg-white p-4",
								"text-left transition-colors hover:border-[var(--leros-primary-soft)]",
								"hover:bg-[var(--leros-primary-softer)]/35",
							)}
						>
							<div className="flex size-10 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
								<BidComparisonIcon className="size-5" />
							</div>
							<h3 className="mt-4 text-sm font-semibold text-[var(--leros-text-strong)]">
								标书对比
							</h3>
							<p className="mt-1 text-xs leading-5 text-[var(--leros-text-muted)]">
								选择投标文件与对比文件，发起标书对比分析任务。
							</p>
						</button>
						<button
							type="button"
							onClick={openDialog}
							className={cn(
								"group flex min-h-[132px] cursor-pointer flex-col rounded-lg border border-slate-200 bg-white p-4",
								"text-left transition-colors hover:border-[var(--leros-primary-soft)]",
								"hover:bg-[var(--leros-primary-softer)]/35",
							)}
						>
							<div className="flex size-10 items-center justify-center rounded-lg bg-emerald-50 text-emerald-600">
								<Calculator className="size-5" />
							</div>
							<h3 className="mt-4 text-sm font-semibold text-[var(--leros-text-strong)]">
								考勤工资核算
							</h3>
							<p className="mt-1 text-xs leading-5 text-[var(--leros-text-muted)]">
								上传人员、历史工资和当月考勤资料，发起工资核算分析任务。
							</p>
						</button>
					</div>
				</div>
			</main>

			<Dialog
				open={dialogOpen}
				onOpenChange={(open) => {
					setDialogOpen(open);
					if (!open) resetDialog();
				}}
			>
				<DialogContent className="flex max-h-[min(92dvh,880px)] max-w-[min(92vw,560px)] flex-col gap-0 overflow-hidden p-0 sm:rounded-2xl">
					<DialogHeader className="shrink-0 border-b border-slate-100 px-7 py-5">
						<div className="flex items-center gap-3">
							<div className="flex size-9 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600">
								<Calculator className="size-5" />
							</div>
							<div>
								<DialogTitle className="text-base">新建考勤工资核算</DialogTitle>
								<DialogDescription className="mt-1 text-xs">
									选择项目/任务、人员底表/历史工资表/当月考勤表后，开始工资核算
								</DialogDescription>
							</div>
						</div>
					</DialogHeader>

					<div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-7 py-5">
						<ProjectTaskPickerField
							projectId={projectId}
							taskId={taskId}
							allowNewProject
							allowSelectTask
							onLoadProjectTasks={fetchTasks}
							onSelect={(nextProjectId, nextTaskId) => {
								setProjectId(nextProjectId);
								setTaskId(nextTaskId);
								if (nextProjectId) fetchTasks(nextProjectId);
							}}
						/>

						{(
							[
								["人员底表", "roster", rosterInputRef],
								["历史工资表", "historical_payroll", historicalInputRef],
								["当月考勤表", "attendance", attendanceInputRef],
							] as const
						).map(([title, role, ref]) => {
							const roleFiles = files.filter((file) => file.role === role);
							return (
								<section key={role}>
									<div className="mb-2 flex items-end justify-between gap-3">
										<div>
											<div className="text-sm font-semibold text-slate-800">
												{title} <span className="text-red-500">*</span>
											</div>
										</div>
										<span className="shrink-0 text-xs text-slate-400">{roleFiles.length} 个</span>
									</div>
									<div className="rounded-xl border border-dashed border-slate-200 bg-slate-50/60 p-3">
										{roleFiles.length ? (
											<div className="space-y-2">
												{roleFiles.map((selected) => (
													<div
														key={selected.id}
														className="flex min-h-10 items-center gap-2 rounded-lg bg-white px-3 py-2 text-sm shadow-sm"
													>
														<FileSpreadsheet className="size-4 shrink-0 text-emerald-600" />
														<span className="min-w-0 flex-1 truncate text-slate-700">
															{selected.name}
														</span>
														<button
															type="button"
															onClick={() =>
																setFiles((current) =>
																	current.filter((file) => file.id !== selected.id),
																)
															}
															className="rounded-md p-1 text-slate-400 hover:bg-slate-50 hover:text-slate-700"
															aria-label={`移除 ${selected.name}`}
														>
															<X className="size-4" />
														</button>
													</div>
												))}
											</div>
										) : (
											<p className="py-3 text-center text-xs text-slate-400">
												上传文件或从项目文件树中选择
											</p>
										)}
										<div className="mt-3 flex gap-2">
											<Button
												type="button"
												size="sm"
												variant="outline"
												onClick={() => ref.current?.click()}
											>
												<Upload className="size-3.5" />
												上传文件
											</Button>
											<Button
												type="button"
												size="sm"
												variant="outline"
												onClick={() => void openProjectFilePicker(role)}
											>
												<FolderOpen className="size-3.5" />
												项目文件
											</Button>
										</div>
									</div>
								</section>
							);
						})}
					</div>

					<DialogFooter className="shrink-0 border-t border-slate-100 px-7 py-4">
						<Button type="button" variant="ghost" onClick={() => setDialogOpen(false)}>
							取消
						</Button>
						<Button
							type="button"
							onClick={() => void startAnalysis()}
							disabled={
								!isAuthenticated ||
								submitting ||
								!files.some((file) => file.role === "roster") ||
								!files.some((file) => file.role === "historical_payroll") ||
								!files.some((file) => file.role === "attendance")
							}
							className="bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90"
						>
							{submitting ? "启动中..." : "开始分析"}
						</Button>
					</DialogFooter>
					<input
						ref={rosterInputRef}
						type="file"
						className="hidden"
						multiple
						onChange={(event) => {
							addFiles(event.target.files, "roster");
							event.target.value = "";
						}}
					/>
					<input
						ref={historicalInputRef}
						type="file"
						className="hidden"
						multiple
						onChange={(event) => {
							addFiles(event.target.files, "historical_payroll");
							event.target.value = "";
						}}
					/>
					<input
						ref={attendanceInputRef}
						type="file"
						className="hidden"
						multiple
						onChange={(event) => {
							addFiles(event.target.files, "attendance");
							event.target.value = "";
						}}
					/>
					{filePickerRole ? (
						<ProjectFilePicker
							open
							mode="compare"
							titleOverride={
								filePickerRole === "roster"
									? "选择人员底表"
									: filePickerRole === "historical_payroll"
										? "选择历史工资表"
										: "选择当月考勤表"
							}
							maxCountOverride={Number.POSITIVE_INFINITY}
							projects={projectOptions}
							initialSelected={files
								.filter((file) => file.role === filePickerRole && file.publicId && file.projectId)
								.map(
									(file) =>
										({
											name: file.name,
											publicId: file.publicId,
											projectId: file.projectId,
											storageUri: file.storageUri,
											mimeType: file.mimeType,
											size: file.size,
										}) satisfies BidComparisonProjectFile,
								)}
							onConfirm={selectProjectFiles}
							onClose={() => setFilePickerRole(null)}
						/>
					) : null}
				</DialogContent>
			</Dialog>
			<BidComparisonConfigDialog
				open={bidComparisonOpen}
				onOpenChange={setBidComparisonOpen}
				onSave={startBidComparison}
				onProjectChange={fetchTasks}
				projects={projectOptions}
			/>
		</div>
	);
}
