"use client";

import {
	type DigitalAssistantItem,
	type Project,
	type ProjectTask,
	projectFileApi,
	useChatStore,
	useDAStore,
	useLayoutStore,
} from "@leros/store";
import type { Attachment, ComposerToken, MessageMetadata } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Command, CommandInput } from "@leros/ui/components/ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { cn } from "@leros/ui/lib/utils";
import {
	Check,
	ChevronDown,
	ChevronRight,
	Files,
	FileText,
	Folder,
	FolderOpen,
	ListTodo,
	Paperclip,
	SendHorizonal,
	Sparkles,
	Target,
	X,
} from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { renderHighlightedText } from "../common/searchText";
import { PROJECT_ATTACHMENT_ACCEPT } from "../input/ChatInput";
import { ComposerActionBar } from "../input/ComposerActionBar";
import {
	type ComposerAssistantOption,
	StructuredComposer,
	type StructuredComposerHandle,
} from "../input/StructuredComposer";
import type { AppNavigation } from "./LeftRail";

function removeWorkbenchDirectiveTokens(value: string): string {
	// 中文注释：选择已有项目后不再支持临时召唤队友/技能，需要同步移除输入框中已插入的指令 token。
	return value
		.replace(/(^|\s)(?:@[^\s@/]+|\/[^\s@/]+)(?=\s|$)/g, " ")
		.replace(/[ \t]{2,}/g, " ")
		.trimStart();
}

function buildComposerMetadata(
	content: string,
	tokens: ComposerToken[],
): MessageMetadata | undefined {
	const trimmed = content.trim();
	if (!trimmed || tokens.length === 0) return undefined;
	const leadingOffset = content.length - content.trimStart().length;
	const composerTokens = tokens
		.map((token) => ({
			...token,
			start: token.start - leadingOffset,
			end: token.end - leadingOffset,
		}))
		.filter((token) => token.start >= 0 && trimmed.slice(token.start, token.end) === token.label);
	return composerTokens.length > 0 ? { composerTokens } : undefined;
}

function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isSummonableAssistant(assistant: DigitalAssistantItem): boolean {
	if (assistant.status !== "active") return false;
	const deploymentStatus = assistant.deploymentStatus?.trim();
	return !deploymentStatus || deploymentStatus === "ready";
}

function resolveMentionedAssistant(
	content: string,
	tokens: ComposerToken[],
	assistantOptions: ComposerAssistantOption[],
): ComposerAssistantOption | null {
	const mentionedNames = tokens
		.filter((token) => token.kind === "assistant")
		.map((token) => token.label.replace(/^@/, ""))
		.filter(Boolean);

	for (const name of mentionedNames) {
		const assistant = assistantOptions.find((option) => option.name === name);
		if (assistant) return assistant;
	}

	for (const match of content.matchAll(/(?:^|\s)@([^\s@/#]+)(?=\s|$)/g)) {
		const name = match[1] ?? "";
		const assistant = assistantOptions.find((option) => option.name === name);
		if (assistant) return assistant;
	}
	return null;
}

function removeAssistantMentionText(
	content: string,
	tokens: ComposerToken[],
	assistant?: ComposerAssistantOption | null,
): string {
	const tokenNames = tokens
		.filter((token) => token.kind === "assistant")
		.map((token) => token.label.replace(/^@/, ""))
		.filter(Boolean);
	const mentionedNames = Array.from(
		new Set([...tokenNames, assistant?.name ?? ""].filter(Boolean)),
	);

	return mentionedNames
		.reduce((next, name) => {
			const pattern = new RegExp(`(^|\\s)@${escapeRegExp(name)}(?=\\s|$)`, "g");
			return next.replace(pattern, " ");
		}, content)
		.replace(/[ \t]{2,}/g, " ")
		.trim();
}

function getFilteredProjectTree(projects: Project[], query: string) {
	const keyword = query.trim().toLowerCase();
	if (!keyword) {
		return projects.map((project) => ({
			project,
			tasks: project.tasks,
		}));
	}

	return projects.flatMap((project) => {
		const projectMatched = project.name.toLowerCase().includes(keyword);
		const matchedTasks = project.tasks.filter((task) => task.title.toLowerCase().includes(keyword));
		if (!projectMatched && matchedTasks.length === 0) return [];
		return [
			{
				project,
				tasks: projectMatched ? project.tasks : matchedTasks,
			},
		];
	});
}

export function WorkbenchPanel({ navigation }: { navigation?: AppNavigation }) {
	const {
		projects,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
		selectWorkbenchProject,
		selectWorkbenchTask,
		sendWorkbenchMessage,
		fetchProjects,
		fetchTasks,
		saveWorkbenchRecentContext,
		clearTaskDetailRoute,
	} = useLayoutStore((s) => s);
	const { assistants, assistantsLoaded, fetchAssistants } = useDAStore((s) => s);
	const { addUploadedAttachment, isGenerating, startGlobalEvents } = useChatStore((s) => s);
	const { isAuthenticated, openAuthDialog, requireAuth } = useAuth();
	const fileInputRef = useRef<HTMLInputElement>(null);
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const attachmentsRef = useRef<Attachment[]>([]);
	const projectTriggerClearRef = useRef<(() => void) | null>(null);
	const projectTriggerDismissRef = useRef<(() => void) | null>(null);
	const sendingRef = useRef(false);
	const [input, setInput] = useState("");
	const [executionMode, setExecutionMode] = useState<"default" | "plan">("default");
	const [attachments, setAttachments] = useState<Attachment[]>([]);
	const [projectMenuOpen, setProjectMenuOpen] = useState(false);
	const [projectSearch, setProjectSearch] = useState("");
	const [expandedProjectIds, setExpandedProjectIds] = useState<Set<string>>(() => new Set());
	const [taskLoadedProjectIds, setTaskLoadedProjectIds] = useState<Set<string>>(() => new Set());
	const [loadingTaskProjectIds, setLoadingTaskProjectIds] = useState<Set<string>>(() => new Set());

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
		void fetchProjects();
	}, [fetchProjects]);

	useEffect(() => {
		if (assistantsLoaded) return;
		void fetchAssistants();
	}, [assistantsLoaded, fetchAssistants]);

	useEffect(() => {
		if (!isAuthenticated) return;
		void startGlobalEvents();
	}, [isAuthenticated, startGlobalEvents]);

	useLayoutEffect(() => {
		clearTaskDetailRoute();
		selectWorkbenchProject(null);
	}, [clearTaskDetailRoute, selectWorkbenchProject]);

	useEffect(() => {
		if (!activeWorkbenchProjectId) return;
		setInput((current) => {
			const next = removeWorkbenchDirectiveTokens(current);
			return next === current ? current : next;
		});
	}, [activeWorkbenchProjectId]);

	const performSend = async (content: string) => {
		if (isGenerating || sendingRef.current) return;
		sendingRef.current = true;
		try {
			await startGlobalEvents();
			const composerTokens = composerRef.current?.getComposerTokens() ?? [];
			const composerMetadata = buildComposerMetadata(input, composerTokens);
			const mentionedAssistant = activeWorkbenchProjectId
				? null
				: resolveMentionedAssistant(content, composerTokens, availableAssistantOptions);
			const messageContent = mentionedAssistant
				? removeAssistantMentionText(content, composerTokens, mentionedAssistant)
				: content;
			const messageMetadata = mentionedAssistant ? undefined : composerMetadata;
			const data = await sendWorkbenchMessage(
				messageContent,
				activeWorkbenchProjectId,
				executionMode,
				attachments,
				messageMetadata,
				mentionedAssistant?.id,
			);
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
			path: payload.public_id || payload.storage_uri || payload.path,
			fileUploadId: payload.public_id,
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
	const availableAssistantOptions = useMemo<ComposerAssistantOption[]>(
		() =>
			assistants.filter(isSummonableAssistant).map((assistant) => ({
				id: assistant.id,
				code: String(assistant.id),
				name: assistant.name,
				description:
					assistant.description ||
					(assistant.expertise.length > 0 ? assistant.expertise.join("、") : "AI 队友"),
			})),
		[assistants],
	);
	const filteredProjectTree = useMemo(
		() => getFilteredProjectTree(projects, projectSearch),
		[projectSearch, projects],
	);
	const recentProjects = useMemo(() => projects.slice(0, 3), [projects]);

	// 中文注释：项目列表接口不含任务，展开或搜索展示时需按需拉取详情中的任务树。
	const loadProjectTasksIfNeeded = useCallback(
		(projectId: string) => {
			const project = projects.find((item) => item.id === projectId);
			if (!project || project.tasks.length > 0 || taskLoadedProjectIds.has(projectId)) {
				return;
			}
			setLoadingTaskProjectIds((current) => new Set(current).add(projectId));
			void fetchTasks(projectId).finally(() => {
				setTaskLoadedProjectIds((current) => new Set(current).add(projectId));
				setLoadingTaskProjectIds((current) => {
					const next = new Set(current);
					next.delete(projectId);
					return next;
				});
			});
		},
		[fetchTasks, projects, taskLoadedProjectIds],
	);

	useEffect(() => {
		if (!projectMenuOpen) return;
		for (const projectId of expandedProjectIds) {
			loadProjectTasksIfNeeded(projectId);
		}
	}, [expandedProjectIds, loadProjectTasksIfNeeded, projectMenuOpen]);

	useEffect(() => {
		if (!projectMenuOpen || !projectSearch.trim()) return;
		for (const { project } of filteredProjectTree) {
			loadProjectTasksIfNeeded(project.id);
		}
	}, [filteredProjectTree, loadProjectTasksIfNeeded, projectMenuOpen, projectSearch]);

	const activeTask = activeProject?.tasks.find((t) => t.id === activeWorkbenchTaskId);
	const projectTaskSelectorLabel = activeProject
		? activeTask
			? `${activeProject.name} / ${activeTask.title}`
			: `${activeProject.name} / 新建任务`
		: "新建项目/任务";
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

	const clearProjectTriggerText = useCallback(() => {
		projectTriggerClearRef.current?.();
		projectTriggerClearRef.current = null;
		projectTriggerDismissRef.current = null;
	}, []);

	const dismissProjectTriggerText = useCallback(() => {
		// 中文注释：用户手动关闭弹窗时保留 # 文本为正文，并阻止同一位置再次触发项目选择器。
		projectTriggerDismissRef.current?.();
		projectTriggerClearRef.current = null;
		projectTriggerDismissRef.current = null;
	}, []);

	const closeProjectMenu = useCallback(() => {
		dismissProjectTriggerText();
		setProjectMenuOpen(false);
		setProjectSearch("");
	}, [dismissProjectTriggerText]);

	const handleSelectNewProjectTask = useCallback(() => {
		requireAuth(() => {
			clearProjectTriggerText();
			selectWorkbenchProject(null);
		});
	}, [clearProjectTriggerText, requireAuth, selectWorkbenchProject]);

	const handleSelectProject = useCallback(
		(project: Project) => {
			requireAuth(() => {
				clearProjectTriggerText();
				selectWorkbenchProject(project.id);
				void saveWorkbenchRecentContext(project.id, null);
				// 中文注释：选择已有项目后，首页输入框不再支持临时 AI 队友/技能标签，需同步清理已选指令。
				setInput((current) => removeWorkbenchDirectiveTokens(current));
				setExpandedProjectIds((current) => {
					const next = new Set(current);
					next.add(project.id);
					return next;
				});
			});
		},
		[clearProjectTriggerText, requireAuth, saveWorkbenchRecentContext, selectWorkbenchProject],
	);

	const handleSelectTask = useCallback(
		(project: Project, task: ProjectTask) => {
			requireAuth(() => {
				clearProjectTriggerText();
				selectWorkbenchProject(project.id);
				selectWorkbenchTask(task.id);
				void saveWorkbenchRecentContext(project.id, task.id);
				setInput((current) => removeWorkbenchDirectiveTokens(current));
				setExpandedProjectIds((current) => {
					const next = new Set(current);
					next.add(project.id);
					return next;
				});
			});
		},
		[
			clearProjectTriggerText,
			requireAuth,
			saveWorkbenchRecentContext,
			selectWorkbenchProject,
			selectWorkbenchTask,
		],
	);

	const toggleProjectExpanded = useCallback(
		(projectId: string) => {
			let shouldExpand = false;
			setExpandedProjectIds((current) => {
				const next = new Set(current);
				if (next.has(projectId)) {
					next.delete(projectId);
				} else {
					next.add(projectId);
					shouldExpand = true;
				}
				return next;
			});
			if (shouldExpand) {
				loadProjectTasksIfNeeded(projectId);
			}
		},
		[loadProjectTasksIfNeeded],
	);

	const handleProjectTrigger = useCallback(
		(query: string, clearTrigger: () => void, dismissTrigger: () => void) => {
			projectTriggerClearRef.current = clearTrigger;
			projectTriggerDismissRef.current = dismissTrigger;
			if (!isAuthenticated) {
				openAuthDialog("login");
				return;
			}
			setProjectSearch(query);
			setProjectMenuOpen(true);
		},
		[isAuthenticated, openAuthDialog],
	);

	const handleProjectMenuOpenChange = (open: boolean) => {
		if (!open) {
			closeProjectMenu();
			return;
		}
		requireAuth(() => {
			setProjectMenuOpen(true);
		});
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
						<h2 className="text-4xl font-semibold tracking-tight text-[var(--leros-text-strong)] md:text-5xl">
							你好, <span className="text-[var(--leros-primary)]">我能帮助你什么？</span>
						</h2>
						<p className="text-lg font-medium italic uppercase tracking-widest text-[var(--leros-text-subtle)]">
							你的AI队友，已上线。
						</p>
					</div>

					{/* 中文注释：工作台输入卡片与 ChatInput 的 project 变体保持同一套边框、阴影与内边距规范。 */}
					{/* 中文注释：输入框保持完整圆角，和 Codex 一样作为上层卡片悬浮在项目选择条之上。 */}
					<div className="relative z-10 flex flex-col rounded-2xl bg-white px-4 py-2 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-[0_0_24px_rgba(15,23,42,0.12)] focus-within:ring-slate-200/70">
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
								assistantOptions={availableAssistantOptions}
								directivesDisabled={Boolean(activeProject)}
								onProjectTrigger={handleProjectTrigger}
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
									assistantOptions={availableAssistantOptions}
									disableAssistantAndSkill={Boolean(activeProject)}
									executionMode={executionMode}
									setExecutionMode={setExecutionMode}
									isGenerating={isGenerating}
								/>
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
					<Popover open={projectMenuOpen} onOpenChange={handleProjectMenuOpenChange}>
						{/* 中文注释：项目/任务选择条保持与输入框同宽，并轻微上移到输入框阴影下方，形成 Codex 式上下双层卡片。 */}
						<div className="-mt-3 flex w-full items-center rounded-b-2xl bg-slate-50/90 px-4 pb-2 pt-4 text-sm text-slate-500 shadow-sm ring-1 ring-slate-200/60">
							<PopoverTrigger
								type="button"
								className="inline-flex min-w-0 items-center gap-2 rounded-lg px-2 py-1 text-sm font-medium text-slate-600 transition-colors hover:bg-white hover:text-slate-900 data-[state=open]:bg-white data-[state=open]:text-slate-900"
								aria-label="选择项目任务"
								title={projectTaskSelectorLabel}
							>
								<Folder className="size-4 shrink-0" />
								<span className="truncate">{projectTaskSelectorLabel}</span>
								<ChevronDown className="size-3.5 shrink-0 text-slate-400" />
							</PopoverTrigger>
						</div>
						<PopoverContent
							align="start"
							side="bottom"
							sideOffset={8}
							// 中文注释：项目任务选择器固定在触发器下方，空间不足时只压缩列表高度，不切换弹窗方向。
							collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
							className="w-[360px] rounded-2xl border border-slate-200/80 bg-white/95 p-2 shadow-[0_18px_45px_rgba(15,23,42,0.16)] ring-0 backdrop-blur"
						>
							<Command shouldFilter={false} className="rounded-xl! bg-transparent p-0">
								<CommandInput
									value={projectSearch}
									onValueChange={setProjectSearch}
									placeholder="搜索项目或任务"
								/>
							</Command>
							<div className="no-scrollbar mt-1 max-h-64 space-y-1 overflow-y-auto">
								<button
									type="button"
									onClick={handleSelectNewProjectTask}
									className={cn(
										"flex h-10 w-full items-center gap-2.5 rounded-xl px-3 text-left text-sm font-medium transition-colors",
										!activeProject
											? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)] ring-1 ring-[var(--leros-primary-soft)]"
											: "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
									)}
								>
									<span className="flex size-4 shrink-0 items-center justify-center">
										{!activeProject ? <Check className="size-4" /> : null}
									</span>
									<span className="min-w-0 flex-1 truncate">新建项目/任务</span>
								</button>
								<div className="my-1.5 h-px bg-slate-100" />
								{filteredProjectTree.map(({ project, tasks }) => {
									const projectSelected = activeWorkbenchProjectId === project.id;
									const taskSelected = (task: ProjectTask) =>
										projectSelected && activeWorkbenchTaskId === task.id;
									const expanded =
										Boolean(projectSearch.trim()) || expandedProjectIds.has(project.id);

									return (
										<div key={project.id} className="space-y-1">
											<div
												className={cn(
													"flex h-10 w-full items-center gap-1.5 rounded-xl px-2 text-left text-sm font-medium transition-colors",
													projectSelected
														? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)] ring-1 ring-[var(--leros-primary-soft)]"
														: "text-slate-700 hover:bg-slate-100",
												)}
											>
												<button
													type="button"
													onClick={(event) => {
														event.stopPropagation();
														toggleProjectExpanded(project.id);
													}}
													className="flex size-7 shrink-0 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-white/80 hover:text-slate-700"
													aria-label={expanded ? "收起项目任务" : "展开项目任务"}
												>
													<ChevronRight
														className={cn("size-3.5 transition-transform", expanded && "rotate-90")}
													/>
												</button>
												<button
													type="button"
													onClick={() => handleSelectProject(project)}
													className="flex min-w-0 flex-1 items-center gap-2 text-left"
												>
													<Folder className="size-4 shrink-0" />
													<span className="min-w-0 flex-1 truncate">
														{renderHighlightedText(project.name, projectSearch)}
													</span>
												</button>
												{projectSelected && <Check className="size-4 shrink-0" />}
											</div>
											{expanded && (
												<div className="ml-6 space-y-1 border-l border-slate-100 pl-2">
													{loadingTaskProjectIds.has(project.id) ? (
														<div className="px-3 py-2 text-xs text-slate-400">任务加载中...</div>
													) : tasks.length > 0 ? (
														tasks.map((task) => (
															<button
																key={task.id}
																type="button"
																onClick={() => handleSelectTask(project, task)}
																className={cn(
																	"flex w-full items-center gap-2.5 rounded-xl px-3 py-2 text-left text-sm transition-colors",
																	taskSelected(task)
																		? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)] ring-1 ring-[var(--leros-primary-soft)]"
																		: "text-slate-600 hover:bg-slate-100 hover:text-slate-900",
																)}
															>
																<span className="flex size-4 shrink-0 items-center justify-center">
																	{taskSelected(task) && <Check className="size-4" />}
																</span>
																<ListTodo className="size-4 shrink-0 opacity-75" />
																<span className="min-w-0 flex-1 truncate">
																	{renderHighlightedText(task.title, projectSearch)}
																</span>
															</button>
														))
													) : (
														<div className="px-3 py-2 text-xs text-slate-400">
															暂无任务，选择项目后将新建任务
														</div>
													)}
												</div>
											)}
										</div>
									);
								})}
								{filteredProjectTree.length === 0 && (
									<div className="px-3 py-8 text-center text-sm text-slate-400">
										没有匹配的项目或任务
									</div>
								)}
							</div>
						</PopoverContent>
					</Popover>
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
										<p className="line-clamp-2 text-sm font-medium leading-6 text-[var(--leros-text)]">
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
