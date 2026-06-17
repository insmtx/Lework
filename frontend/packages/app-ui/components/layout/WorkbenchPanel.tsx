"use client";

import { projectFileApi, useChatStore, useLayoutStore } from "@leros/store";
import type { Attachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	Bell,
	Check,
	ChevronDown,
	Folder,
	ListTodo,
	Paperclip,
	Plus,
	Search,
	SendHorizonal,
	X,
} from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { PROJECT_ATTACHMENT_ACCEPT } from "../input/ChatInput";
import type { AppNavigation } from "./LeftRail";

const mockActivities = [
	{
		id: "activity-1",
		avatar: "SK",
		name: "Sarah K.",
		project: "backend-v2",
		time: "2 分钟前",
		description: "完成了 API 追踪",
		note: "解决了 auth-middleware 模块中的 4 个延迟问题。系统开销降低了 12%。",
	},
	{
		id: "activity-2",
		avatar: "AL",
		name: "Ada Lovelace",
		project: "frontend-core",
		time: "45 分钟前",
		description: "更新了文档",
		tags: ["文档", "修订版本 3"],
	},
];

export function WorkbenchPanel({ navigation }: { navigation?: AppNavigation }) {
	const {
		projects,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
		selectWorkbenchProject,
		selectWorkbenchTask,
		sendWorkbenchMessage,
		fetchProjects,
		switchProject,
		clearTaskDetailRoute,
	} = useLayoutStore((s) => s);
	const { startSessionResponseStream, resetLocalMessages, addUploadedAttachment } = useChatStore(
		(s) => s,
	);
	const { isAuthenticated, openAuthDialog, requireAuth, user } = useAuth();
	const fileInputRef = useRef<HTMLInputElement>(null);
	const attachmentsRef = useRef<Attachment[]>([]);
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
		const data = await sendWorkbenchMessage(content, activeWorkbenchProjectId, attachments);
		if (data?.session_id) {
			await startSessionResponseStream(data.session_id, content);
		}
		if (navigation && data?.project_id && data?.task_id && data?.session_id) {
			navigation.goToTaskDetail(data.project_id, data.task_id, data.session_id);
		}
		setInput("");
		clearAttachments();
	};

	const handleSend = async () => {
		const content = input.trim();
		if (!content) return;
		if (!isAuthenticated) {
			requireAuth(() => {
				void performSend(content);
			});
			return;
		}
		await performSend(content);
	};

	const handleAttachmentSelect = async (event: React.ChangeEvent<HTMLInputElement>) => {
		const files = Array.from(event.target.files ?? []);
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
		event.target.value = "";
	};

	const uploadWorkbenchAttachment = async (file: File) => {
		// 无项目时先走通用上传，后续随 NewMessage 自动关联到新建项目。
		const response = await projectFileApi.uploadLoose({ file, purpose: "attachment" });
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
	};

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
	const latestProject = projects[0];
	const filteredProjects = useMemo(() => {
		const keyword = projectSearch.trim().toLowerCase();
		if (!keyword) return projects;
		return projects.filter((project) => project.name.toLowerCase().includes(keyword));
	}, [projectSearch, projects]);

	const activeTask = activeProject?.tasks.find((t) => t.id === activeWorkbenchTaskId);
	const filteredTasks = useMemo(() => {
		const keyword = taskSearch.trim().toLowerCase();
		if (!activeProject) return [];
		if (!keyword) return activeProject.tasks;
		return activeProject.tasks.filter(
			(t) => t.title.toLowerCase().includes(keyword) || t.meta.toLowerCase().includes(keyword),
		);
	}, [taskSearch, activeProject]);

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

	const handleOpenActivityProject = (projectName: string) => {
		const project = projects.find((item) => item.id === projectName || item.name === projectName);
		if (project) {
			requireAuth(() => {
				if (navigation) {
					navigation.goToProject(project.id);
					return;
				}
				switchProject(project.id);
			});
		}
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
					<button
						type="button"
						className="relative rounded-full p-2 transition-colors hover:bg-[var(--leros-primary-softer)]"
					>
						<Bell className="size-5" />
						<span className="absolute right-2 top-2 size-2 rounded-full border-2 border-[var(--leros-app-bg)] bg-destructive" />
					</button>
					<button
						type="button"
						onClick={() => {
							if (!isAuthenticated) openAuthDialog("login");
						}}
						className="rounded-full bg-[#070d1c] px-5 py-2 text-sm font-semibold text-white shadow-sm transition-colors hover:bg-[#182033]"
					>
						{isAuthenticated ? (user?.name ?? "已登录") : "登录"}
					</button>
				</div>
			</header>

			{/* Main Content Canvas */}
			<div className="z-10 mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-10 py-12">
				{/* Welcome/Hero Section */}
				<section className="mb-8">
					<div className="max-w-3xl mx-auto">
						<div className="mb-6 flex flex-col items-start gap-4 text-left">
							<h2 className="text-4xl font-bold tracking-tight text-[var(--leros-text-strong)] md:text-5xl">
								你好, <span className="text-[var(--leros-primary)]">我能帮助你什么？</span>
							</h2>
							<p className="text-lg font-medium italic uppercase tracking-widest text-[var(--leros-text-subtle)]">
								你的AI队友，已上线。
							</p>
						</div>

						{/* Enhanced Command Input Card */}
						<div className="flex flex-col rounded-[24px] border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-4 shadow-sm transition-all focus-within:border-[var(--leros-primary)] focus-within:shadow-md">
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
											<span className="max-w-[160px] truncate text-slate-600">
												{attachment.name}
											</span>
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
							<div className="mb-2 flex gap-3">
								<textarea
									value={input}
									onChange={(event) => setInput(event.target.value)}
									onKeyDown={(event) => {
										if (event.key === "Enter" && !event.shiftKey) {
											event.preventDefault();
											handleSend();
										}
									}}
									placeholder="在这里开始新任务，或输入指令以同步您的项目进度..."
									className="h-[60px] flex-1 resize-none border-none bg-transparent text-base text-[var(--leros-chat-input-text)] outline-none placeholder:text-[var(--leros-chat-placeholder)] focus:ring-0"
								/>
							</div>
							<div className="flex items-center justify-between border-t border-[var(--leros-chat-ai-border)] pt-3">
								<div className="flex items-center gap-3">
									<button
										type="button"
										onClick={() => {
											if (!isAuthenticated) {
												openAuthDialog("login");
												return;
											}
											fileInputRef.current?.click();
										}}
										className="rounded-full p-1.5 text-[var(--leros-text-muted)] transition-colors hover:bg-[var(--leros-chat-control-bg)]"
										aria-label="添加附件"
									>
										<Plus className="size-5" />
									</button>
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
										disabled={!input.trim() && attachments.length === 0}
										className="size-9 rounded-xl bg-[var(--leros-primary)] text-white shadow-sm hover:bg-[var(--leros-primary-strong)] disabled:bg-[var(--leros-chat-control-bg)] disabled:text-[var(--leros-text-subtle)]"
									>
										<SendHorizonal className="size-4" />
									</Button>
								</div>
							</div>
						</div>

						{/* Suggested Actions */}
						<div className="mt-6 flex items-center justify-center gap-4">
							<button
								type="button"
								onClick={() => requireAuth()}
								className="flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-widest text-[var(--leros-text-subtle)] transition-colors hover:text-[var(--leros-primary)]"
							>
								分析 SPRINT
							</button>
							<button
								type="button"
								onClick={() => requireAuth()}
								className="flex items-center gap-1.5 text-[11px] font-bold uppercase tracking-widest text-[var(--leros-text-subtle)] transition-colors hover:text-[var(--leros-primary)]"
							>
								总结报告
							</button>
						</div>
					</div>
				</section>

				{/* Workbench Grid */}
				<section className="mt-6 grid flex-1 grid-cols-12 gap-10">
					{/* Left: Activity Stream (col-span-8) */}
					<div className="col-span-8">
						<div className="mb-8 flex items-center justify-between border-b border-[var(--leros-control-border)] pb-4">
							<h3 className="text-xl font-bold tracking-tight text-[var(--leros-text-strong)]">
								动态流
							</h3>
							<div className="flex rounded-lg bg-[var(--leros-chat-control-bg)] p-1">
								<button
									type="button"
									className="rounded bg-[var(--leros-surface)] px-4 py-1.5 text-[12px] font-bold text-[var(--leros-text-strong)] shadow-sm"
								>
									今日
								</button>
								<button
									type="button"
									className="px-4 py-1.5 text-[12px] font-bold text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)]"
								>
									本周
								</button>
							</div>
						</div>

						<div className="relative space-y-10">
							{mockActivities.map((activity, idx) => (
								<div key={activity.id} className="relative flex gap-6">
									{/* Vertical timeline line */}
									{idx < mockActivities.length - 1 && (
										<div className="absolute bottom-[-40px] left-[19px] top-10 w-[1px] bg-[var(--leros-control-border)]" />
									)}
									<div className="z-10 flex-shrink-0">
										<div className="flex size-10 items-center justify-center rounded-full border-2 border-[var(--leros-surface)] bg-[var(--leros-text-strong)] text-sm font-bold text-white shadow-sm">
											{activity.avatar}
										</div>
									</div>
									<div className="flex-1 pt-0.5">
										<div className="mb-2 flex items-baseline justify-between">
											<p className="text-sm text-[var(--leros-text)]">
												<span className="font-bold text-[var(--leros-text-strong)]">
													{activity.name}
												</span>
												<span> 在 </span>
												<button
													type="button"
													className="font-semibold text-[var(--leros-primary)] hover:underline"
													onClick={() => handleOpenActivityProject(activity.project)}
												>
													{activity.project}
												</button>
												<span> 中{activity.description}</span>
											</p>
											<span className="text-[11px] font-medium text-[var(--leros-text-subtle)]">
												{activity.time}
											</span>
										</div>
										{activity.note ? (
											<div className="rounded-xl bg-[var(--leros-chat-control-bg)] p-4 text-[13px] leading-relaxed text-[var(--leros-text)]">
												“{activity.note}”
											</div>
										) : null}
										{activity.tags ? (
											<div className="mt-2 flex items-center gap-2">
												{activity.tags.map((tag) => (
													<span
														key={tag}
														className="rounded bg-[var(--leros-chat-control-bg)] px-2.5 py-1 text-[10px] font-bold uppercase tracking-wider text-[var(--leros-text)]"
													>
														{tag}
													</span>
												))}
											</div>
										) : null}
									</div>
								</div>
							))}

							{latestProject && (
								<div className="mt-6 rounded-2xl border border-[var(--leros-primary-soft)] bg-[var(--leros-primary-softer)] px-5 py-4 text-xs text-[var(--leros-primary-strong)]">
									最近项目：{latestProject.name} · {latestProject.description}
								</div>
							)}
						</div>
					</div>

					{/* Right: Stats & Promotion (col-span-4) */}
					<div className="col-span-4 space-y-10">
						{/* ToDo card */}
						<div className="rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-6 shadow-sm">
							<h4 className="mb-6 text-[11px] font-bold uppercase tracking-widest text-[var(--leros-text-subtle)]">
								待办事项
							</h4>
							<div className="space-y-5">
								<div className="-mx-2 flex cursor-pointer flex-col gap-2 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex items-center justify-between">
										<p className="flex-1 truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											优化数据库查询性能
										</p>
										<span className="rounded bg-destructive/10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-destructive">
											待处理
										</span>
									</div>
									<div className="flex items-center gap-2 text-[var(--leros-text-subtle)]">
										<span className="text-[11px] font-medium">backend-v2</span>
									</div>
								</div>
								<div className="-mx-2 flex cursor-pointer flex-col gap-2 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex items-center justify-between">
										<p className="flex-1 truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											前端 UI 组件化重构
										</p>
										<span className="rounded bg-[var(--leros-primary-soft)] px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-[var(--leros-primary)]">
											进行中
										</span>
									</div>
									<div className="flex items-center gap-2 text-[var(--leros-text-subtle)]">
										<span className="text-[11px] font-medium">frontend-core</span>
									</div>
								</div>
								<div className="-mx-2 flex cursor-pointer flex-col gap-2 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex items-center justify-between">
										<p className="flex-1 truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											基础设施安全审计
										</p>
										<span className="rounded bg-[var(--leros-chat-control-bg)] px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider text-[var(--leros-text)]">
											待处理
										</span>
									</div>
									<div className="flex items-center gap-2 text-[var(--leros-text-subtle)]">
										<span className="text-[11px] font-medium">infra</span>
									</div>
								</div>
							</div>
						</div>

						{/* Recent Visit files card */}
						<div className="rounded-2xl border border-[var(--leros-control-border)] bg-[var(--leros-surface)] p-6 shadow-sm">
							<h4 className="mb-6 text-[11px] font-bold uppercase tracking-widest text-[var(--leros-text-subtle)]">
								最近访问
							</h4>
							<div className="space-y-5">
								<div className="-mx-2 flex cursor-pointer items-center gap-4 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex h-9 w-9 items-center justify-center rounded-lg bg-destructive/10 text-destructive">
										<span className="text-xs font-bold font-mono">PDF</span>
									</div>
									<div className="flex-1 min-w-0">
										<p className="truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											Q4 产品规划指南.pdf
										</p>
										<p className="text-[11px] text-[var(--leros-text-subtle)]">今天 10:45</p>
									</div>
								</div>
								<div className="-mx-2 flex cursor-pointer items-center gap-4 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]">
										<span className="text-xs font-bold font-mono">DOC</span>
									</div>
									<div className="flex-1 min-w-0">
										<p className="truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											后端架构重构草案.docx
										</p>
										<p className="text-[11px] text-[var(--leros-text-subtle)]">昨天 16:20</p>
									</div>
								</div>
								<div className="-mx-2 flex cursor-pointer items-center gap-4 rounded-lg p-2 transition-colors hover:bg-[var(--leros-surface-soft)]">
									<div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[var(--leros-chat-control-bg)] text-[var(--leros-text-muted)]">
										<span className="text-xs font-bold font-mono">PNG</span>
									</div>
									<div className="flex-1 min-w-0">
										<p className="truncate text-[13px] font-bold text-[var(--leros-text-strong)]">
											v0.2 设计手稿.png
										</p>
										<p className="text-[11px] text-[var(--leros-text-subtle)]">10月24日</p>
									</div>
								</div>
							</div>
						</div>
					</div>
				</section>
			</div>
		</div>
	);
}
