"use client";

import { projectFileApi, useChatStore, useLayoutStore } from "@leros/store";
import type { Attachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	Check,
	ChevronDown,
	Files,
	FileText,
	Folder,
	FolderOpen,
	ListTodo,
	Paperclip,
	Search,
	SendHorizonal,
	Sparkles,
	Target,
	X,
} from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { PROJECT_ATTACHMENT_ACCEPT } from "../input/ChatInput";
import { ComposerActionBar } from "../input/ComposerActionBar";
import { StructuredComposer, type StructuredComposerHandle } from "../input/StructuredComposer";
import type { AppNavigation } from "./LeftRail";

export function WorkbenchPanel({ navigation }: { navigation?: AppNavigation }) {
	const {
		projects,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
		selectWorkbenchProject,
		selectWorkbenchTask,
		sendWorkbenchMessage,
		fetchProjects,
		clearTaskDetailRoute,
	} = useLayoutStore((s) => s);
	const { startSessionResponseStream, resetLocalMessages, addUploadedAttachment, isGenerating } =
		useChatStore((s) => s);
	const { isAuthenticated, openAuthDialog, requireAuth } = useAuth();
	const fileInputRef = useRef<HTMLInputElement>(null);
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const attachmentsRef = useRef<Attachment[]>([]);
	const sendingRef = useRef(false);
	const [input, setInput] = useState("");
	const [attachments, setAttachments] = useState<Attachment[]>([]);
	const [projectMenuOpen, setProjectMenuOpen] = useState(false);
	const [projectSearch, setProjectSearch] = useState("");
	const [taskMenuOpen, setTaskMenuOpen] = useState(false);
	const [taskSearch, setTaskSearch] = useState("");

	const revokeAttachmentURLs = (items: Attachment[]) => {
		for (const attachment of items) {
			if (attachment.url?.startsWith("blob:")) {
				URL.revokeObjectURL(attachment.url);
			}
		}
	};

	const clearAttachments = () => {
		revokeAttachmentURLs(attachmentsRef.current);
		setAttachments([]);
	};

	const cloneAttachmentsForOptimisticMessage = (items: Attachment[]) =>
		items.map((attachment) => {
			// 中文注释：工作台清空输入区前，先为图片附件复制一份独立预览地址，避免跳页后首屏丢图。
			if (attachment.type === "image" && attachment.file) {
				return {
					...attachment,
					url: URL.createObjectURL(attachment.file),
				};
			}
			return { ...attachment };
		});

	useEffect(() => {
		attachmentsRef.current = attachments;
	}, [attachments]);

	useEffect(() => {
		fetchProjects();
	}, [fetchProjects]);

	useLayoutEffect(() => {
		clearTaskDetailRoute();
		resetLocalMessages();
	}, [clearTaskDetailRoute, resetLocalMessages]);

	const performSend = async (content: string) => {
		if (isGenerating || sendingRef.current) return;
		sendingRef.current = true;
		try {
			const data = await sendWorkbenchMessage(content, activeWorkbenchProjectId, attachments);
			if (data?.session_id) {
				const optimisticAttachments = cloneAttachmentsForOptimisticMessage(attachments);
				// 中文注释：工作台跳转详情页前，先把附件写进 optimistic 消息，避免首屏只剩文本。
				await startSessionResponseStream(data.session_id, content, optimisticAttachments);
			}
			if (navigation && data?.project_id && data?.task_id && data?.session_id) {
				navigation.goToTaskDetail(data.project_id, data.task_id, data.session_id);
			}
			setInput("");
			clearAttachments();
		} finally {
			sendingRef.current = false;
		}
	};

	const handleSend = async () => {
		const content = input.trim();
		if (!content || isGenerating || sendingRef.current) return;
		if (!isAuthenticated) {
			requireAuth(() => {
				void performSend(content);
			});
			return;
		}
		await performSend(content);
	};

	const uploadWorkbenchAttachment = useCallback(async (file: File) => {
		// 中文注释：未选项目时先走通用上传，后续再随 NewMessage 关联到新建任务上下文。
		const response = await projectFileApi.uploadLoose({
			file,
			purpose: "attachment",
		});
		const payload = response.data;
		const attachment: Attachment = {
			id: `att-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
			type: file.type.startsWith("image/") ? "image" : "file",
			name: payload.original_name || payload.filename || file.name,
			size: payload.file_size ?? payload.size ?? file.size,
			url: file.type.startsWith("image/") ? URL.createObjectURL(file) : undefined,
			file,
			path: payload.public_id || payload.storage_path || payload.path,
			fileUploadId: payload.file_upload_id,
			mimeType: payload.mime_type || file.type,
		};
		return { attachment, message: response.message };
	}, []);

	const uploadAttachments = useCallback(
		async (files: File[]) => {
			if (!files.length) return;

			for (const file of files) {
				try {
					const uploaded = activeWorkbenchProjectId
						? await addUploadedAttachment(activeWorkbenchProjectId, file)
						: await uploadWorkbenchAttachment(file);
					const { attachment, message } = uploaded;
					setAttachments((prev) => [...prev, attachment]);
					toast.success(message || "文件上传成功");
				} catch (err) {
					const message = err instanceof Error ? err.message : "文件上传失败";
					console.error("Workbench upload attachment error:", err);
					toast.error(message);
				}
			}
		},
		[activeWorkbenchProjectId, addUploadedAttachment, uploadWorkbenchAttachment],
	);

	const handleAttachmentSelect = async (event: React.ChangeEvent<HTMLInputElement>) => {
		const files = Array.from(event.target.files ?? []);
		if (!files.length) return;

		await uploadAttachments(files);
		event.target.value = "";
	};

	const handlePasteFiles = useCallback(
		(event: React.ClipboardEvent<HTMLElement>) => {
			const files = Array.from(event.clipboardData.files);
			if (!files.length) return;

			if (!isAuthenticated) {
				openAuthDialog("login");
				return;
			}
			void uploadAttachments(files);
		},
		[isAuthenticated, openAuthDialog, uploadAttachments],
	);

	const handleRemoveAttachment = (attachmentId: string) => {
		setAttachments((prev) => {
			const target = prev.find((attachment) => attachment.id === attachmentId);
			if (target?.url?.startsWith("blob:")) {
				URL.revokeObjectURL(target.url);
			}
			return prev.filter((attachment) => attachment.id !== attachmentId);
		});
	};
	const activeProject = projects.find((project) => project.id === activeWorkbenchProjectId);
	const filteredProjects = useMemo(() => {
		const keyword = projectSearch.trim().toLowerCase();
		if (!keyword) return projects;
		return projects.filter((project) => project.name.toLowerCase().includes(keyword));
	}, [projectSearch, projects]);
	const recentProjects = useMemo(() => projects.slice(0, 3), [projects]);

	const activeTask = activeProject?.tasks.find((t) => t.id === activeWorkbenchTaskId);
	const filteredTasks = useMemo(() => {
		const keyword = taskSearch.trim().toLowerCase();
		if (!activeProject) return [];
		if (!keyword) return activeProject.tasks;
		return activeProject.tasks.filter(
			(t) => t.title.toLowerCase().includes(keyword) || t.meta.toLowerCase().includes(keyword),
		);
	}, [taskSearch, activeProject]);
	const suggestedPrompts = useMemo(
		() => [
			"帮我拆解当前项目的下一步执行计划",
			"总结这个项目的当前进展和风险",
			activeProject
				? `基于 ${activeProject.name} 生成今天的工作清单`
				: "帮我创建一个新项目并给出启动方案",
		],
		[activeProject],
	);

	const handleSelectProject = (projectId: string | null) => {
		requireAuth(() => {
			selectWorkbenchProject(projectId);
			setProjectMenuOpen(false);
			setProjectSearch("");
		});
	};

	const handleSelectTask = (taskId: string | null) => {
		requireAuth(() => {
			selectWorkbenchTask(taskId);
			setTaskMenuOpen(false);
			setTaskSearch("");
		});
	};

	const handleProjectMenuOpenChange = (open: boolean) => {
		if (!open) {
			setProjectMenuOpen(false);
			return;
		}
		requireAuth(() => setProjectMenuOpen(true));
	};

	const handleTaskMenuOpenChange = (open: boolean) => {
		if (!open) {
			setTaskMenuOpen(false);
			return;
		}
		requireAuth(() => setTaskMenuOpen(true));
	};

	const applyPrompt = (prompt: string) => {
		setInput(prompt);
	};

	const openProject = (projectId: string) => {
		requireAuth(() => {
			if (navigation) {
				navigation.goToProject(projectId);
				return;
			}
			selectWorkbenchProject(projectId);
		});
	};

	useEffect(() => () => revokeAttachmentURLs(attachmentsRef.current), []);

	return (
		<div
			data-slot="workbench-panel"
			className="min-h-0 flex-1 overflow-y-auto bg-[var(--leros-app-bg)]"
		>
			{/* Top Header */}
			<header className="z-10 flex h-16 shrink-0 items-center justify-end px-10">
				<div className="flex items-center gap-4 text-[var(--leros-text)]">
					<button
						type="button"
						className="rounded-full p-2 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<Search className="size-5" />
					</button>
					{/* <button
						type="button"
						className="relative rounded-full p-2 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<Bell className="size-5" />
						<span className="absolute right-2 top-2 size-2 rounded-full border-2 border-[var(--leros-app-bg)] bg-destructive" />
					</button> */}
					{/* <button
						type="button"
						onClick={() => {
							if (!isAuthenticated) openAuthDialog("login");
						}}
						className="rounded-full bg-[#070d1c] px-5 py-2 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-[#182033]"
						disabled={!isHydrated}
					>
						{!isHydrated ? "" : isAuthenticated ? (user?.name ?? "已登录") : "登录"}
					</button> */}
				</div>
			</header>

			{/* Main Content Canvas */}
			<div className="z-10 mx-auto flex min-h-[calc(100vh-4rem)] w-full max-w-[1100px] flex-col justify-center px-10 py-16">
				{/* Welcome/Hero Section */}
				<section className="mb-8">
					<div className="mb-6 flex flex-col items-start gap-4 text-left">
						<h2 className="text-4xl font-bold tracking-tight text-[var(--leros-text-strong)] md:text-5xl">
							你好, <span className="text-[var(--leros-primary)]">我能帮助你什么？</span>
						</h2>
						<p className="text-lg font-medium italic uppercase tracking-widest text-[var(--leros-text-subtle)]">
							你的AI队友，已上线。
						</p>
					</div>

					{/* 中文注释：工作台输入卡片与 ChatInput 的 project 变体保持同一套边框、阴影与内边距规范。 */}
					<div className="relative flex flex-col rounded-2xl bg-white px-4 py-2 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-[0_0_24px_rgba(15,23,42,0.12)] focus-within:ring-slate-200/70">
						<input
							ref={fileInputRef}
							type="file"
							className="hidden"
							accept={PROJECT_ATTACHMENT_ACCEPT}
							multiple
							onChange={handleAttachmentSelect}
						/>
						{attachments.length > 0 && (
							<div className="mb-3 flex flex-wrap gap-2">
								{attachments.map((attachment) => (
									<div
										key={attachment.id}
										className="flex items-center gap-2 rounded-lg bg-white/90 px-3 py-2 text-sm shadow-sm ring-1 ring-slate-200/70"
									>
										{attachment.type === "image" && attachment.url ? (
											<img
												src={attachment.url}
												alt={attachment.name}
												className="size-8 rounded object-cover"
											/>
										) : (
											<Paperclip className="size-3.5 text-slate-400" />
										)}
										<span className="max-w-[160px] truncate text-slate-600">{attachment.name}</span>
										<button
											type="button"
											onClick={() => handleRemoveAttachment(attachment.id)}
											className="text-slate-400 transition-colors hover:text-slate-600"
										>
											<X className="size-3.5" />
										</button>
									</div>
								))}
							</div>
						)}
						<div className="min-w-0">
							<StructuredComposer
								ref={composerRef}
								value={input}
								onChange={setInput}
								onSubmit={() => {
									void handleSend();
								}}
								onPasteFiles={handlePasteFiles}
								onFocus={() => undefined}
								onBlur={() => undefined}
								placeholder="在这里开始新任务，或输入指令以同步您的项目进度..."
								isProjectVariant
							/>
						</div>
						<div className="flex items-center justify-between border-t border-[var(--leros-chat-ai-border)] pt-3">
							<div className="flex items-center gap-3">
								<ComposerActionBar
									inputValue={input}
									composerRef={composerRef}
									onUpload={() => fileInputRef.current?.click()}
									onBeforeAction={() => {
										if (!isAuthenticated) {
											openAuthDialog("login");
											return false;
										}
										return true;
									}}
								/>
								<Popover open={projectMenuOpen} onOpenChange={handleProjectMenuOpenChange}>
									<PopoverTrigger
										type="button"
										className="flex h-8 min-w-[140px] items-center gap-2 rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3 text-xs font-semibold text-[var(--leros-text)] outline-none transition-colors hover:border-[var(--leros-focus-ring)] data-[open]:border-[var(--leros-primary)]"
										aria-label="新项目"
									>
										<Folder className="size-4 shrink-0 text-[var(--leros-text-muted)]" />
										<span className="max-w-[120px] truncate">
											{activeProject?.name ?? "新项目"}
										</span>
										{activeProject && (
											<button
												type="button"
												onClick={(e) => {
													e.stopPropagation();
													handleSelectProject(null);
												}}
												className="shrink-0 rounded-full p-0.5 text-[var(--leros-text-subtle)] hover:bg-[var(--leros-chat-control-bg)] hover:text-[var(--leros-text)]"
											>
												<X className="size-3.5" />
											</button>
										)}
										<ChevronDown className="ml-auto size-3.5 shrink-0 text-[var(--leros-text-subtle)]" />
									</PopoverTrigger>
									<PopoverContent
										align="start"
										side="bottom"
										sideOffset={10}
										className="w-[260px] gap-0 rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-2.5 shadow-[0_18px_45px_rgba(30,41,59,0.18)] ring-0"
									>
										<div className="flex h-10 items-center gap-2 rounded-xl border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-3 text-[var(--leros-text-muted)]">
											<Search className="size-4 shrink-0" />
											<input
												value={projectSearch}
												onChange={(event) => setProjectSearch(event.target.value)}
												placeholder="搜索项目"
												className="h-full min-w-0 flex-1 bg-transparent text-sm text-[var(--leros-text)] outline-none placeholder:text-[var(--leros-text-subtle)]"
											/>
										</div>

										<div className="mt-2.5 max-h-[200px] space-y-1 overflow-y-auto pr-1">
											{filteredProjects.map((project) => {
												const selected = activeWorkbenchProjectId === project.id;

												return (
													<button
														key={project.id}
														type="button"
														onClick={() => handleSelectProject(project.id)}
														className={cn(
															"flex h-9 w-full items-center gap-2.5 rounded-lg px-3 text-left text-sm font-semibold transition-colors",
															selected
																? "bg-[var(--leros-primary)] text-white"
																: "text-[var(--leros-text)] hover:bg-[var(--leros-chat-control-bg)]",
														)}
													>
														<span className="flex size-4 shrink-0 items-center justify-center">
															{selected && <Check className="size-4" />}
														</span>
														<span className="truncate">{project.name}</span>
													</button>
												);
											})}

											{filteredProjects.length === 0 && (
												<div className="px-3 py-6 text-center text-sm text-[var(--leros-text-muted)]">
													没有匹配的项目
												</div>
											)}
										</div>
									</PopoverContent>
								</Popover>
								{activeProject && (
									<Popover open={taskMenuOpen} onOpenChange={handleTaskMenuOpenChange}>
										<PopoverTrigger
											type="button"
											className="flex h-8 min-w-[140px] items-center gap-2 rounded-full border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3 text-xs font-semibold text-[var(--leros-text)] outline-none transition-colors hover:border-[var(--leros-focus-ring)] data-[open]:border-[var(--leros-primary)]"
											aria-label="选择任务"
										>
											<ListTodo className="size-4 shrink-0 text-[var(--leros-text-muted)]" />
											<span className="max-w-[120px] truncate">
												{activeTask?.title ?? "选择任务"}
											</span>
											{activeTask && (
												<button
													type="button"
													onClick={(e) => {
														e.stopPropagation();
														handleSelectTask(null);
													}}
													className="shrink-0 rounded-full p-0.5 text-[var(--leros-text-subtle)] hover:bg-[var(--leros-chat-control-bg)] hover:text-[var(--leros-text)]"
												>
													<X className="size-3.5" />
												</button>
											)}
											<ChevronDown className="ml-auto size-3.5 shrink-0 text-[var(--leros-text-subtle)]" />
										</PopoverTrigger>
										<PopoverContent
											align="start"
											side="bottom"
											sideOffset={10}
											className="w-[260px] gap-0 rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-2.5 shadow-[0_18px_45px_rgba(30,41,59,0.18)] ring-0"
										>
											<div className="flex h-10 items-center gap-2 rounded-xl border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-3 text-[var(--leros-text-muted)]">
												<Search className="size-4 shrink-0" />
												<input
													value={taskSearch}
													onChange={(event) => setTaskSearch(event.target.value)}
													placeholder="搜索任务"
													className="h-full min-w-0 flex-1 bg-transparent text-sm text-[var(--leros-text)] outline-none placeholder:text-[var(--leros-text-subtle)]"
												/>
											</div>

											<div className="mt-2.5 max-h-[200px] space-y-1 overflow-y-auto pr-1">
												{filteredTasks.map((task) => {
													const selected = activeWorkbenchTaskId === task.id;

													return (
														<button
															key={task.id}
															type="button"
															onClick={() => handleSelectTask(task.id)}
															className={cn(
																"flex h-auto w-full flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left transition-colors",
																selected
																	? "bg-[var(--leros-primary)] text-white"
																	: "text-[var(--leros-text)] hover:bg-[var(--leros-chat-control-bg)]",
															)}
														>
															<div className="flex w-full items-center gap-2.5">
																<span className="flex size-4 shrink-0 items-center justify-center">
																	{selected && <Check className="size-4" />}
																</span>
																<span className="text-sm font-semibold">{task.title}</span>
															</div>
															<span
																className={cn(
																	"ml-[26px] text-xs",
																	selected ? "text-white/70" : "text-[var(--leros-text-muted)]",
																)}
															>
																{task.meta}
															</span>
														</button>
													);
												})}

												{filteredTasks.length === 0 && (
													<div className="px-3 py-6 text-center text-sm text-[var(--leros-text-muted)]">
														没有匹配的任务
													</div>
												)}
											</div>
										</PopoverContent>
									</Popover>
								)}
							</div>
							<div className="flex items-center gap-2">
								<Button
									size="icon"
									onClick={handleSend}
									disabled={isGenerating || !input.trim()}
									// 中文注释：工作台发送按钮与项目/任务页保持同一视觉规格。
									className="size-9 min-w-0 rounded-xl bg-black !text-white shadow-sm hover:bg-blue-700 disabled:bg-[#f3f3f4] disabled:!text-slate-400"
								>
									<SendHorizonal
										className={cn(
											"size-3.5",
											input.trim() && !isGenerating
												? "fill-white stroke-white text-white"
												: "fill-none stroke-current text-current",
										)}
									/>
								</Button>
							</div>
						</div>
					</div>
				</section>

				<section className="grid gap-6 lg:grid-cols-[1.05fr_0.95fr]">
					<div className="h-full min-w-0">
						<div className="flex h-full flex-col rounded-[24px] border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-6 shadow-sm">
							<div className="mb-5">
								<div className="flex items-center gap-2">
									<div className="shrink-0 rounded-full bg-[var(--leros-primary-softer)] p-2 text-[var(--leros-primary)]">
										<Sparkles className="size-4" />
									</div>
									<h3 className="text-lg font-semibold text-[var(--leros-text-strong)]">
										开始建议
									</h3>
								</div>
								<p className="mt-1 pl-10 text-sm text-[var(--leros-text-muted)]">
									点一下即可填入输入框，适合用来启动工作台对话。
								</p>
							</div>

							<div className="grid gap-3 md:grid-cols-3">
								{suggestedPrompts.map((prompt) => (
									<button
										key={prompt}
										type="button"
										onClick={() => applyPrompt(prompt)}
										title={prompt}
										className="flex min-w-0 flex-col gap-3 rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-4 py-4 text-left transition-colors hover:border-[var(--leros-primary)] hover:bg-[var(--leros-primary-softer)]"
									>
										<div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)]">
											<FileText className="size-5" />
										</div>
										<p className="line-clamp-3 text-sm font-medium leading-6 text-[var(--leros-text)]">
											{prompt}
										</p>
									</button>
								))}
							</div>
						</div>
					</div>

					<div className="h-full min-w-0">
						<div className="flex h-full flex-col rounded-[24px] border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-6 shadow-sm">
							<div className="mb-5">
								<div className="flex items-center gap-2">
									<div className="shrink-0 rounded-full bg-[var(--leros-primary-softer)] p-2 text-[var(--leros-primary)]">
										<FolderOpen className="size-4" />
									</div>
									<h3 className="text-lg font-semibold text-[var(--leros-text-strong)]">
										最近项目
									</h3>
								</div>
								<p className="mt-1 pl-10 text-sm text-[var(--leros-text-muted)]">
									从最近同步的项目里快速恢复上下文。
								</p>
							</div>

							{recentProjects.length > 0 ? (
								<div className="space-y-3">
									{recentProjects.slice(0, 1).map((project) => (
										<button
											key={project.id}
											type="button"
											onClick={() => openProject(project.id)}
											title={project.name}
											className="flex w-full min-w-0 items-start gap-3 rounded-2xl border border-[var(--leros-control-border)] px-4 py-4 text-left transition-colors hover:border-[var(--leros-primary)] hover:bg-[var(--leros-primary-softer)]"
										>
											<div className="shrink-0 rounded-xl bg-[var(--leros-surface-soft)] p-2 text-[var(--leros-text-muted)]">
												<Folder className="size-4" />
											</div>
											<div className="min-w-0 flex-1">
												<p className="line-clamp-2 text-sm font-semibold text-[var(--leros-text-strong)]">
													{project.name}
												</p>
												<p className="mt-1 line-clamp-2 text-sm text-[var(--leros-text-muted)]">
													{project.description || "暂无项目描述"}
												</p>
												<div className="mt-3 flex items-center gap-4 text-xs text-[var(--leros-text-subtle)]">
													<span className="inline-flex items-center gap-1">
														<Target className="size-3.5" />
														{project.tasks.length} 个任务
													</span>
													<span className="inline-flex items-center gap-1">
														<Files className="size-3.5" />
														{project.files.length} 个文件
													</span>
												</div>
											</div>
										</button>
									))}
								</div>
							) : (
								<div className="rounded-2xl border border-dashed border-[var(--leros-control-border)] px-4 py-5 text-sm text-[var(--leros-text-muted)]">
									还没有项目数据。先发起一个任务，系统会自动为你沉淀项目上下文。
								</div>
							)}
						</div>
					</div>
				</section>
			</div>
		</div>
	);
}
