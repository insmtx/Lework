"use client";

import {
	buildComposerFolderUploadSummaryMessage,
	COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE,
	COMPOSER_UPLOAD_SUCCESS_MESSAGE,
	COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE,
	getComposerUploadAccept,
	hasComposerSkillTokens,
	isComposerUploadAllowedFile,
	isEmptyUploadFile,
	type Project,
	type ProjectTask,
	partitionComposerFolderFiles,
	prepareOutgoingComposer,
	projectFileApi,
	revokeAttachmentObjectUrls,
	useChatStore,
	useDAStore,
	useLayoutStore,
} from "@leros/store";
import type { Attachment, ComposerToken, MessageMetadata } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@leros/ui/components/ui/popover";
import { getRequestErrorMessage } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import {
	BookOpenText,
	ChevronDown,
	FileText,
	ListTodo,
	type LucideIcon,
	SendHorizonal,
	TrendingUp,
} from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuth } from "../auth";
import { isAssistantAvailable } from "../digitalAssistant/assistantStatus";
import { AttachmentPreview } from "../input/AttachmentPreview";
import { ComposerActionBar } from "../input/ComposerActionBar";
import { ComposerUsageTipsPanel } from "../input/ComposerUsageTipsPanel";
import { buildComposerUsageTips } from "../input/composerUsageTips";
import {
	type ComposerAssistantOption,
	StructuredComposer,
	type StructuredComposerHandle,
} from "../input/StructuredComposer";
import {
	FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE,
	getFileRelativePath,
	getFolderNameFromFiles,
	isFolderUploadSizeExceeded,
} from "../input/upload-folder";
import { useComposerConnectorOptions } from "../input/useComposerConnectorOptions";
import { useComposerSkillOptions } from "../input/useComposerSkillOptions";
import { useBrandIdentity } from "../private-deployment/useBrandIdentity";
import { openPendingAttachmentPreview } from "./file-preview-store";
import type { AppNavigation } from "./LeftRail";
import { formatProjectTaskPickerLabel, ProjectTaskPickerContent } from "./ProjectTaskPicker";
import { ProjectIcon } from "./project-icon";

function removeWorkbenchDirectiveTokens(value: string): string {
	// 中文注释：选择已有项目后不再支持临时召唤队友，需要同步移除输入框中已插入的 @ 指令 token；技能 mention 保留。
	return value
		.replace(/(^|\s)@[^\s@/]+(?=\s|$)/g, " ")
		.replace(/[ \t]{2,}/g, " ")
		.trimStart();
}

function buildInvokedAssistantMetadata(
	baseMetadata: MessageMetadata | undefined,
	assistant: ComposerAssistantOption,
): MessageMetadata {
	// 中文注释：@ 队友保留在 content 中，与 AddMessage / 技能指令一致；metadata 仅补充路由与头像展示信息。
	const invokedAssistant: NonNullable<MessageMetadata["invokedAssistant"]> = {
		id: assistant.id,
		name: assistant.name,
	};
	if (assistant.avatarUrl) invokedAssistant.avatarUrl = assistant.avatarUrl;
	return { ...baseMetadata, invokedAssistant };
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

	const matchedByText = assistantOptions.find((option) => {
		const mention = `@${option.name}`;
		return (
			content === mention || content.includes(`${mention} `) || content.includes(` ${mention}`)
		);
	});
	if (matchedByText) return matchedByText;

	for (const match of content.matchAll(/(?:^|\s)@([^\s@/#]+)(?=\s|$)/g)) {
		const name = match[1] ?? "";
		const assistant = assistantOptions.find((option) => option.name === name);
		if (assistant) return assistant;
	}
	return null;
}

const WORKBENCH_FEATURE_CARDS: Array<{
	step: string;
	title: string;
	description: string;
	icon: LucideIcon;
	iconClassName: string;
}> = [
	{
		step: "01",
		title: "任务规划与拆解",
		description: "对齐目标背景，拆解执行路径。",
		icon: ListTodo,
		iconClassName: "bg-violet-100 text-violet-600",
	},
	{
		step: "02",
		title: "任务指派",
		description: "明确任务要求，指派角色执行。",
		icon: FileText,
		iconClassName: "bg-blue-100 text-blue-600",
	},
	{
		step: "03",
		title: "执行与审批",
		description: "AI 自动推进，人工确认关键节点。",
		icon: TrendingUp,
		iconClassName: "bg-emerald-100 text-emerald-600",
	},
	{
		step: "04",
		title: "知识沉淀",
		description: "沉淀过程成果，形成项目资产。",
		icon: BookOpenText,
		iconClassName: "bg-orange-100 text-orange-600",
	},
];

function detectDesktopApp(): boolean {
	if (typeof window === "undefined") return false;
	const win = window as Window & { electron?: unknown; lerosDesktop?: unknown };
	return Boolean(win.electron ?? win.lerosDesktop);
}

export function NewTaskPage({ navigation }: { navigation?: AppNavigation }) {
	const { name: brandName } = useBrandIdentity();
	const composerUploadAccept = getComposerUploadAccept(
		typeof navigator === "undefined" ? undefined : navigator.platform,
	);
	const {
		projects,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
		selectWorkbenchProject,
		selectWorkbenchTask,
		sendWorkbenchMessage,
		fetchProjects,
		fetchTasks,
		clearTaskDetailRoute,
		workbenchComposerPrefill,
		consumeWorkbenchComposerPrefill,
	} = useLayoutStore((s) => s);
	const { assistants, fetchAssistants } = useDAStore((s) => s);
	const { startGlobalEvents } = useChatStore((s) => s);
	const { isAuthenticated, openAuthDialog, requireAuth } = useAuth();
	const fileInputRef = useRef<HTMLInputElement>(null);
	const folderInputRef = useRef<HTMLInputElement>(null);
	const uploadAbortControllersRef = useRef<Map<string, AbortController>>(new Map());
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const attachmentsRef = useRef<Attachment[]>([]);
	const { skillOptions, skillsLoading, reloadSkillOptions } = useComposerSkillOptions(
		activeWorkbenchProjectId ?? null,
		isAuthenticated,
	);
	const { connectorOptions, connectorsLoading } = useComposerConnectorOptions({
		projectId: activeWorkbenchProjectId ?? null,
		enabled: isAuthenticated,
	});
	const [selectedConnectorIds, setSelectedConnectorIds] = useState<string[]>([]);
	const handleAssistantPickerOpen = useCallback(() => fetchAssistants(), [fetchAssistants]);
	const handleSelectConnector = useCallback((publicId: string) => {
		setSelectedConnectorIds((prev) => (prev.includes(publicId) ? prev : [...prev, publicId]));
	}, []);
	const handleRemoveConnector = useCallback((publicId: string) => {
		setSelectedConnectorIds((prev) => prev.filter((id) => id !== publicId));
	}, []);
	const projectTriggerClearRef = useRef<(() => void) | null>(null);
	const projectTriggerDismissRef = useRef<(() => void) | null>(null);
	const sendingRef = useRef(false);
	const [input, setInput] = useState("");
	const [executionMode, setExecutionMode] = useState<"default" | "plan">("default");
	const [attachments, setAttachments] = useState<Attachment[]>([]);
	const hasUploadingAttachments = attachments.some(
		(attachment) => attachment.uploadStatus === "uploading",
	);
	const [isSending, setIsSending] = useState(false);
	const [projectMenuOpen, setProjectMenuOpen] = useState(false);
	const [projectSearch, setProjectSearch] = useState("");
	const [isDesktopApp, setIsDesktopApp] = useState(false);
	const applyingWorkbenchPrefillIdRef = useRef<string | null>(null);
	const wasAuthenticatedRef = useRef(isAuthenticated);

	const clearAttachments = () => {
		revokeAttachmentObjectUrls(attachmentsRef.current);
		setAttachments([]);
	};

	useEffect(() => {
		attachmentsRef.current = attachments;
	}, [attachments]);

	useEffect(() => {
		setIsDesktopApp(detectDesktopApp());
	}, []);

	useEffect(() => {
		if (!isAuthenticated) return;
		void fetchProjects();
	}, [fetchProjects, isAuthenticated]);

	useEffect(() => {
		if (!isAuthenticated) return;
		void fetchAssistants();
	}, [fetchAssistants, isAuthenticated]);

	useEffect(() => {
		if (!isAuthenticated) return;
		void startGlobalEvents();
	}, [isAuthenticated, startGlobalEvents]);

	useLayoutEffect(() => {
		clearTaskDetailRoute();
		selectWorkbenchProject(null);
	}, [clearTaskDetailRoute, selectWorkbenchProject]);

	useLayoutEffect(() => {
		if (!workbenchComposerPrefill) return;
		applyingWorkbenchPrefillIdRef.current = workbenchComposerPrefill.id;
		setInput(workbenchComposerPrefill.value);
	}, [workbenchComposerPrefill]);

	useEffect(() => {
		if (!workbenchComposerPrefill) return;
		if (applyingWorkbenchPrefillIdRef.current !== workbenchComposerPrefill.id) return;

		let cancelled = false;
		const applyPrefill = () => {
			if (cancelled) return;
			if (!composerRef.current) {
				requestAnimationFrame(applyPrefill);
				return;
			}

			composerRef.current.setContent(
				workbenchComposerPrefill.value,
				workbenchComposerPrefill.tokens,
			);
			consumeWorkbenchComposerPrefill(workbenchComposerPrefill.id);
			applyingWorkbenchPrefillIdRef.current = null;
		};

		applyPrefill();
		return () => {
			cancelled = true;
		};
	}, [consumeWorkbenchComposerPrefill, workbenchComposerPrefill]);

	useEffect(() => {
		if (!activeWorkbenchProjectId) return;
		setInput((current) => {
			const next = removeWorkbenchDirectiveTokens(current);
			return next === current ? current : next;
		});
	}, [activeWorkbenchProjectId]);

	// 中文注释：连接器关联是项目级配置，切换项目后清空已选连接器，避免跨项目残留。
	useEffect(() => {
		setSelectedConnectorIds([]);
	}, [activeWorkbenchProjectId]);

	const performSend = async () => {
		// 中文注释：首页新建任务不应被其他任务的全局生成态锁住，只拦截本输入框的重复提交。
		if (sendingRef.current) return;
		sendingRef.current = true;
		setIsSending(true);
		try {
			await startGlobalEvents();
			const composerTokens = composerRef.current?.getComposerTokens() ?? [];
			const prepared = prepareOutgoingComposer(input, composerTokens);
			if (!prepared.content && attachments.length === 0) {
				return;
			}
			const mentionedAssistant = activeWorkbenchProjectId
				? null
				: resolveMentionedAssistant(
						prepared.content || input,
						prepared.metadata?.composerTokens ?? composerTokens,
						availableAssistantOptions,
					);
			const messageMetadata = mentionedAssistant
				? buildInvokedAssistantMetadata(prepared.metadata, mentionedAssistant)
				: prepared.metadata;
			// 中文注释：NewMessage 后端按 publicId 字符串数组解析 assistant_ids。
			const mentionedAssistantIds = mentionedAssistant ? [mentionedAssistant.id] : undefined;
			const data = await sendWorkbenchMessage(
				prepared.content,
				activeWorkbenchProjectId,
				executionMode,
				attachments,
				messageMetadata,
				mentionedAssistantIds,
				selectedConnectorIds,
			);
			const hasInvokedSkill = hasComposerSkillTokens(prepared.content);
			if (data && activeWorkbenchProjectId && hasInvokedSkill) {
				void reloadSkillOptions();
			}
			if (navigation && data?.project_id && data?.task_id && data?.session_id) {
				navigation.goToTaskDetail(data.project_id, data.task_id, data.session_id);
			}
			if (data) {
				setInput("");
				clearAttachments();
				// 中文注释：发送成功后清空已选连接器；失败时不进入这里，保留以便重试。
				setSelectedConnectorIds([]);
			}
		} catch (err) {
			console.error("NewTaskPage createInitialMessage error:", err);
			toast.error(`创建任务失败：${getRequestErrorMessage(err) ?? "请稍后重试"}`);
		} finally {
			sendingRef.current = false;
			setIsSending(false);
		}
	};

	const handleSend = async () => {
		const content = input.trim();
		if (!content || sendingRef.current) return;
		if (!isAuthenticated) {
			requireAuth(() => {
				void performSend();
			});
			return;
		}
		await performSend();
	};

	const uploadWorkbenchAttachment = useCallback(
		async (
			file: File,
			attachmentId: string,
			previewUrl: string | undefined,
			signal: AbortSignal,
		) => {
			if (isEmptyUploadFile(file)) {
				throw new Error(COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE);
			}
			if (!isComposerUploadAllowedFile(file)) {
				throw new Error(COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE);
			}

			const response = activeWorkbenchProjectId
				? await projectFileApi.upload({
						projectId: activeWorkbenchProjectId,
						projectPublicId: activeWorkbenchProjectId,
						file,
						signal,
					})
				: await projectFileApi.uploadLoose({
						file,
						purpose: "attachment",
						withLocalPath: true,
						signal,
					});
			const payload = response.data;
			const attachment: Attachment = {
				id: attachmentId,
				type: file.type.startsWith("image/") ? "image" : "file",
				name: payload.original_name || payload.filename || file.name,
				size: payload.file_size ?? payload.size ?? file.size,
				url: previewUrl,
				file,
				path: payload.public_id || payload.storage_uri || payload.path,
				fileUploadId: payload.public_id,
				mimeType: payload.mime_type || file.type,
				storageUri: payload.storage_uri,
				uploadStatus: "completed",
			};
			return { attachment, message: response.message };
		},
		[activeWorkbenchProjectId],
	);

	const uploadAttachments = useCallback(
		async (files: File[]) => {
			if (!files.length) return;

			for (const file of files) {
				const attachmentId = `att-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
				const previewUrl = file.type.startsWith("image/") ? URL.createObjectURL(file) : undefined;
				const abortController = new AbortController();
				uploadAbortControllersRef.current.set(attachmentId, abortController);
				const placeholder: Attachment = {
					id: attachmentId,
					type: file.type.startsWith("image/") ? "image" : "file",
					name: file.name,
					size: file.size,
					url: previewUrl,
					file,
					mimeType: file.type,
					uploadStatus: "uploading",
				};
				setAttachments((prev) => [...prev, placeholder]);

				try {
					const { attachment } = await uploadWorkbenchAttachment(
						file,
						attachmentId,
						previewUrl,
						abortController.signal,
					);
					setAttachments((prev) =>
						prev.map((item) => (item.id === attachmentId ? attachment : item)),
					);
					toast.success(COMPOSER_UPLOAD_SUCCESS_MESSAGE);
				} catch (err) {
					if (abortController.signal.aborted) {
						continue;
					}
					setAttachments((prev) => prev.filter((item) => item.id !== attachmentId));
					if (previewUrl) {
						URL.revokeObjectURL(previewUrl);
					}
					const message = err instanceof Error ? err.message : "文件上传失败";
					console.error("Workbench upload attachment error:", err);
					toast.error(message);
				} finally {
					uploadAbortControllersRef.current.delete(attachmentId);
				}
			}
		},
		[uploadWorkbenchAttachment],
	);

	const handleFolderSelect = async (event: React.ChangeEvent<HTMLInputElement>) => {
		const files = Array.from(event.target.files ?? []);
		event.target.value = "";
		if (!files.length) return;

		if (isFolderUploadSizeExceeded(files)) {
			toast.error(FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE, { position: "bottom-right" });
			return;
		}

		const { uploadable, skippedEmpty, skippedType } = partitionComposerFolderFiles(files);
		if (uploadable.length === 0) {
			toast.error(
				buildComposerFolderUploadSummaryMessage(0, skippedEmpty.length, skippedType.length),
				{ position: "bottom-right" },
			);
			return;
		}

		const folderName = getFolderNameFromFiles(files);
		const attachmentId = `att-folder-${Date.now()}`;
		const estimatedSize = uploadable.reduce((sum, file) => sum + file.size, 0);
		const abortController = new AbortController();
		uploadAbortControllersRef.current.set(attachmentId, abortController);

		const placeholder: Attachment = {
			id: attachmentId,
			type: "folder",
			name: folderName,
			size: estimatedSize,
			uploadStatus: "uploading",
		};
		setAttachments((prev) => [...prev, placeholder]);

		const folderFiles: NonNullable<Attachment["folderFiles"]> = [];
		let totalSize = 0;

		try {
			for (const file of uploadable) {
				if (abortController.signal.aborted) {
					return;
				}

				const response = activeWorkbenchProjectId
					? await projectFileApi.upload({
							projectId: activeWorkbenchProjectId,
							projectPublicId: activeWorkbenchProjectId,
							file,
							signal: abortController.signal,
						})
					: await projectFileApi.uploadLoose({
							file,
							purpose: "attachment",
							withLocalPath: true,
							signal: abortController.signal,
						});
				const payload = response.data;
				if (!payload?.public_id) {
					throw new Error("上传接口未返回 public_id");
				}
				const relativePath = getFileRelativePath(file);
				const displayName = payload.original_name || payload.filename || file.name;
				const fileSize = payload.file_size ?? payload.size ?? file.size;
				folderFiles.push({
					fileUploadId: payload.public_id,
					name: displayName,
					relativePath,
					mimeType: payload.mime_type || file.type || "application/octet-stream",
					size: fileSize,
				});
				totalSize += fileSize;
			}

			const attachment: Attachment = {
				id: attachmentId,
				type: "folder",
				name: folderName,
				size: totalSize,
				folderFiles,
				uploadStatus: "completed",
			};
			setAttachments((prev) => prev.map((item) => (item.id === attachmentId ? attachment : item)));
			const summaryMessage = buildComposerFolderUploadSummaryMessage(
				uploadable.length,
				skippedEmpty.length,
				skippedType.length,
			);
			if (summaryMessage.includes("已跳过")) {
				toast.info(summaryMessage, { position: "bottom-right" });
			} else {
				toast.success(summaryMessage, { position: "bottom-right" });
			}
		} catch (err) {
			if (abortController.signal.aborted) {
				return;
			}
			handleRemoveAttachment(attachmentId);
			const message = err instanceof Error ? err.message : "文件夹上传失败";
			console.error("Workbench upload folder error:", err);
			toast.error(message, { position: "bottom-right" });
		} finally {
			uploadAbortControllersRef.current.delete(attachmentId);
		}
	};

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
				openAuthDialog("phone");
				return;
			}
			void uploadAttachments(files);
		},
		[isAuthenticated, openAuthDialog, uploadAttachments],
	);

	const handleRemoveAttachment = (attachmentId: string) => {
		const abortController = uploadAbortControllersRef.current.get(attachmentId);
		if (abortController) {
			abortController.abort();
			uploadAbortControllersRef.current.delete(attachmentId);
		}

		setAttachments((prev) => {
			const target = prev.find((attachment) => attachment.id === attachmentId);
			revokeAttachmentObjectUrls(target ? [target] : undefined);
			return prev.filter((attachment) => attachment.id !== attachmentId);
		});
	};
	const activeProject = isAuthenticated
		? projects.find((project) => project.id === activeWorkbenchProjectId)
		: undefined;
	const availableAssistantOptions = useMemo<ComposerAssistantOption[]>(
		() =>
			assistants.filter(isAssistantAvailable).map((assistant) => ({
				id: assistant.publicId,
				code: assistant.publicId,
				name: assistant.name,
				roleName: assistant.roleName,
				description:
					assistant.description ||
					(assistant.expertise.length > 0 ? assistant.expertise.join("、") : "AI 队友"),
				avatarUrl: assistant.avatar,
			})),
		[assistants],
	);
	const activeTask = activeProject?.tasks.find((t) => t.id === activeWorkbenchTaskId);
	const projectTaskSelectorLabel = formatProjectTaskPickerLabel(
		projects,
		activeWorkbenchProjectId,
		activeWorkbenchTaskId,
	);
	const workbenchUsageTips = useMemo(
		() => buildComposerUsageTips(activeProject, activeTask),
		[activeProject, activeTask],
	);

	const applyUsageTip = useCallback((prompt: string) => {
		if (composerRef.current) {
			composerRef.current.setContent(prompt);
			return;
		}
		setInput(prompt);
	}, []);

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

	const resetWorkbenchOnLogout = useCallback(() => {
		clearTaskDetailRoute();
		selectWorkbenchProject(null);
		setInput("");
		composerRef.current?.setContent("");
		revokeAttachmentObjectUrls(attachmentsRef.current);
		setAttachments([]);
		setExecutionMode("default");
		closeProjectMenu();
	}, [clearTaskDetailRoute, closeProjectMenu, selectWorkbenchProject]);

	useEffect(() => {
		if (wasAuthenticatedRef.current && !isAuthenticated) {
			resetWorkbenchOnLogout();
		}
		wasAuthenticatedRef.current = isAuthenticated;
	}, [isAuthenticated, resetWorkbenchOnLogout]);

	const handleSelectProject = useCallback(
		(project: Project) => {
			requireAuth(() => {
				clearProjectTriggerText();
				selectWorkbenchProject(project.id);
				setInput((current) => removeWorkbenchDirectiveTokens(current));
				closeProjectMenu();
			});
		},
		[clearProjectTriggerText, closeProjectMenu, requireAuth, selectWorkbenchProject],
	);

	const handleSelectTask = useCallback(
		(project: Project, task: ProjectTask) => {
			requireAuth(() => {
				clearProjectTriggerText();
				selectWorkbenchProject(project.id);
				selectWorkbenchTask(task.id);
				setInput((current) => removeWorkbenchDirectiveTokens(current));
				closeProjectMenu();
			});
		},
		[
			clearProjectTriggerText,
			closeProjectMenu,
			requireAuth,
			selectWorkbenchProject,
			selectWorkbenchTask,
		],
	);

	const handleSelectNewProjectTask = useCallback(() => {
		requireAuth(() => {
			clearProjectTriggerText();
			selectWorkbenchProject(null);
			closeProjectMenu();
		});
	}, [clearProjectTriggerText, closeProjectMenu, requireAuth, selectWorkbenchProject]);

	const handleProjectTrigger = useCallback(
		(query: string, clearTrigger: () => void, dismissTrigger: () => void) => {
			projectTriggerClearRef.current = clearTrigger;
			projectTriggerDismissRef.current = dismissTrigger;
			if (!isAuthenticated) {
				openAuthDialog("phone");
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

	useEffect(() => () => revokeAttachmentObjectUrls(attachmentsRef.current), []);

	return (
		<div
			data-slot="workbench-panel"
			className="min-h-0 flex-1 overflow-y-auto bg-[var(--leros-app-bg)]"
		>
			{/* Main Content Canvas */}
			<div className="z-10 mx-auto flex min-h-full w-full max-w-[1080px] flex-col justify-center px-10 py-10">
				{/* Welcome/Hero Section */}
				<section>
					<div className="mb-10 text-center">
						<h2 className="text-4xl font-semibold tracking-tight text-[var(--leros-primary)] md:text-5xl">
							你好，今天我们从哪里开始？
						</h2>
						<p className="mt-4 text-base text-[var(--leros-text-muted)] md:text-lg">
							告诉{brandName}你的目标，我们会帮你拆解任务、分配执行，并交付结果
						</p>
					</div>

					{/* 中文注释：四步流程说明单独成卡，快捷提示仍放在卡片外并与输入区保持间距。 */}
					<div className="mb-10 overflow-hidden rounded-[28px] bg-white shadow-[0_18px_50px_rgba(15,23,42,0.06)] ring-1 ring-slate-200/70">
						<div
							className={cn(
								"grid",
								isDesktopApp ? "grid-cols-4" : "grid-cols-1 sm:grid-cols-2 xl:grid-cols-4",
							)}
						>
							{WORKBENCH_FEATURE_CARDS.map((card, index) => {
								const Icon = card.icon;
								return (
									<div
										key={card.title}
										className={cn(
											"relative flex flex-col gap-2.5 px-5 py-4",
											!isDesktopApp && index > 0 && "border-t border-slate-100 sm:border-t-0",
											!isDesktopApp && index % 2 === 1 && "sm:border-l sm:border-slate-100",
											!isDesktopApp &&
												index >= 2 &&
												"sm:border-t sm:border-slate-100 xl:border-t-0",
											index > 0 && "xl:border-l xl:border-slate-100",
											isDesktopApp && index > 0 && "border-l border-slate-100",
										)}
									>
										<span className="absolute right-4 top-3.5 text-xs font-medium tabular-nums text-slate-300">
											{card.step}
										</span>
										<div
											className={cn(
												"flex size-10 shrink-0 items-center justify-center rounded-xl",
												card.iconClassName,
											)}
										>
											<Icon className="size-5" />
										</div>
										<div className="min-w-0 pr-6">
											<div className="text-sm font-semibold text-[var(--leros-text-strong)]">
												{card.title}
											</div>
											<p className="mt-1 text-xs leading-relaxed text-[var(--leros-text-muted)]">
												{card.description}
											</p>
										</div>
									</div>
								);
							})}
						</div>
					</div>

					<ComposerUsageTipsPanel tips={workbenchUsageTips} onApply={applyUsageTip} />

					{/* 中文注释：新建任务输入卡片与 ChatInput 的 project 变体保持同一套边框、阴影与内边距规范。 */}
					{/* 中文注释：输入框保持完整圆角，和 Codex 一样作为上层卡片悬浮在项目选择条之上。 */}
					<div className="relative z-10 flex flex-col rounded-2xl bg-white px-4 py-2 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-[0_0_24px_rgba(15,23,42,0.12)] focus-within:ring-slate-200/70">
						<input
							ref={fileInputRef}
							type="file"
							className="hidden"
							accept={composerUploadAccept}
							multiple
							onChange={handleAttachmentSelect}
						/>
						<input
							ref={folderInputRef}
							type="file"
							className="hidden"
							multiple
							onChange={handleFolderSelect}
							{...({
								webkitdirectory: "",
								directory: "",
							} as React.InputHTMLAttributes<HTMLInputElement>)}
						/>

						{attachments.length > 0 && (
							<AttachmentPreview
								attachments={attachments}
								onPreview={openPendingAttachmentPreview}
								onRemove={handleRemoveAttachment}
							/>
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
								placeholder="在这里输入需求或描述目标。使用#选择项目、@召唤AI队友、/调用技能..."
								isProjectVariant
								assistantOptions={availableAssistantOptions}
								onAssistantPickerOpen={handleAssistantPickerOpen}
								assistantSelectionMode="single"
								skillOptions={skillOptions}
								skillsLoading={skillsLoading}
								assistantDirectivesDisabled={Boolean(activeProject)}
								onProjectTrigger={handleProjectTrigger}
							/>
						</div>
						<div className="flex items-center justify-between border-t border-[var(--leros-chat-ai-border)] pt-3">
							<div className="flex items-center gap-3">
								<ComposerActionBar
									inputValue={input}
									composerRef={composerRef}
									onUpload={() => fileInputRef.current?.click()}
									onUploadFolder={() => folderInputRef.current?.click()}
									onBeforeAction={() => {
										if (!isAuthenticated) {
											openAuthDialog("phone");
											return false;
										}
										return true;
									}}
									assistantOptions={availableAssistantOptions}
									onAssistantPickerOpen={handleAssistantPickerOpen}
									assistantSelectionMode="single"
									skillOptions={skillOptions}
									skillsLoading={skillsLoading}
									executionMode={executionMode}
									setExecutionMode={setExecutionMode}
									isGenerating={isSending}
									connectorOptions={connectorOptions}
									connectorsLoading={connectorsLoading}
									selectedConnectorIds={selectedConnectorIds}
									onSelectConnector={handleSelectConnector}
									onRemoveConnector={handleRemoveConnector}
								/>
							</div>
							<div className="flex items-center gap-2">
								<Button
									size="icon"
									onClick={handleSend}
									disabled={isSending || !input.trim() || hasUploadingAttachments}
									// 中文注释：新建任务发送按钮与项目/任务页保持同一视觉规格。
									className="size-9 min-w-0 rounded-xl bg-black !text-white shadow-sm hover:bg-blue-700 disabled:bg-[#f3f3f4] disabled:!text-slate-400"
								>
									<SendHorizonal
										className={cn(
											"size-3.5",
											input.trim() && !isSending
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
								<ProjectIcon className="size-4 shrink-0" />
								<span className="truncate">{projectTaskSelectorLabel}</span>
								<ChevronDown className="size-3.5 shrink-0 text-slate-400" />
							</PopoverTrigger>
						</div>
						<PopoverContent
							align="start"
							side="top"
							sideOffset={10}
							collisionAvoidance={{ side: "none", align: "shift", fallbackAxisSide: "none" }}
							className="!flex-none w-auto overflow-visible rounded-none border-0 bg-transparent p-0 shadow-none ring-0"
						>
							<ProjectTaskPickerContent
								projects={isAuthenticated ? projects : []}
								selectedProjectId={activeWorkbenchProjectId}
								selectedTaskId={activeWorkbenchTaskId}
								searchQuery={projectSearch}
								onSearchQueryChange={setProjectSearch}
								allowNewProject
								scrollSelectedIntoView={projectMenuOpen}
								onLoadProjectTasks={fetchTasks}
								onSelectProject={(project) => handleSelectProject(project as Project)}
								onSelectTask={(project, task) =>
									handleSelectTask(project as Project, task as ProjectTask)
								}
								onSelectNewProject={handleSelectNewProjectTask}
							/>
						</PopoverContent>
					</Popover>
				</section>
			</div>
		</div>
	);
}
