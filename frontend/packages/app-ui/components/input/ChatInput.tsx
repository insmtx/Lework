"use client";

import {
	COMPOSER_UPLOAD_ACCEPT,
	COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE,
	COMPOSER_UPLOAD_SUCCESS_MESSAGE,
	COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE,
	getComposerUploadAccept,
	hasComposerSkillTokens,
	isComposerUploadAllowedFile,
	isEmptyUploadFile,
	isSystemDefaultAssistant,
	type ProjectMember,
	prepareOutgoingComposer,
	projectFileApi,
	useChatStore,
	useDAStore,
	useLayoutStore,
} from "@leros/store";
import type {
	ApprovalAction,
	ApprovalRequest,
	ComposerToken,
	Message,
	MessageMetadata,
	QuestionRequest,
} from "@leros/store/types/chat";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { Checkbox } from "@leros/ui/components/ui/checkbox";
import { Tooltip, TooltipContent, TooltipTrigger } from "@leros/ui/components/ui/tooltip";
import { getRequestErrorMessage } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import {
	AlertCircle,
	AtSign,
	ChevronDown,
	CircleStop,
	ClipboardPenLine,
	LoaderCircle,
	Paperclip,
	SendHorizonal,
	ShieldAlert,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { isAssistantAvailable } from "../digitalAssistant/assistantStatus";
import type { AppNavigation } from "../layout";
import {
	applyDocxSelectionDraftToComposer,
	type DocxSelectionComposerDraft,
	docxSelectionComposerActions,
	type PendingDocxVersionSync,
	removeDocxReferenceFromComposer,
	useDocxSelectionComposerStore,
} from "../layout/docx-selection-composer-store";
import { buildDocxSelectionPromptRequest } from "../layout/docx-selection-edit";
import { openPendingAttachmentPreview } from "../layout/file-preview-store";
import {
	getProjectChatLayoutClasses,
	type ProjectChatLayoutMode,
} from "../layout/project-chat-layout";
import { getLatestProjectFileVersion } from "../layout/project-file-version-sync";
import {
	hasMultipleHumanProjectMembers,
	PROJECT_MCP_COLLABORATION_WARNING,
} from "../layout/project-mcp-collaboration-warning";
import { AttachmentPreview } from "./AttachmentPreview";
import { type BidComparisonConfig, BidComparisonConfigDialog } from "./BidComparisonConfigDialog";
import {
	bidComparisonConfigToAttachments,
	bidComparisonOutputFormat,
	bidComparisonPrompt,
	ensureBidComparisonFilesUploaded,
} from "./bidComparisonAttachments";
import { ComposerActionBar } from "./ComposerActionBar";
import { BidComparisonEntryButton, ComposerUsageTipsPanel } from "./ComposerUsageTipsPanel";
import { buildComposerUsageTips } from "./composerUsageTips";
import { QuestionAnswerInput } from "./QuestionAnswerInput";
import {
	type ComposerAssistantOption,
	StructuredComposer,
	type StructuredComposerHandle,
} from "./StructuredComposer";
import { FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE, isFolderUploadSizeExceeded } from "./upload-folder";
import { useComposerConnectorOptions } from "./useComposerConnectorOptions";
import { useComposerSkillOptions } from "./useComposerSkillOptions";

export const PROJECT_ATTACHMENT_ACCEPT = COMPOSER_UPLOAD_ACCEPT;

export function ChatInput({
	variant = "default",
	projectLayoutMode = "sidebar-expanded",
	navigation,
	bidComparisonEntry = "auto",
}: {
	variant?: "default" | "project";
	/** 项目页聊天区布局：随右侧栏展开/收起切换宽度与留白 */
	projectLayoutMode?: ProjectChatLayoutMode;
	navigation?: AppNavigation;
	/**
	 * 标书对比入口：
	 * - auto：项目新建任务展示使用提示+按钮，任务详情仅按钮
	 * - button：仅标书对比按钮（任务详情）
	 * - tips：使用提示面板含标书对比
	 * - none：不展示
	 */
	bidComparisonEntry?: "auto" | "button" | "tips" | "none";
}) {
	const projectAttachmentAccept = getComposerUploadAccept(
		typeof navigator === "undefined" ? undefined : navigator.platform,
	);
	const {
		activeSessionId,
		inputText,
		inputAttachments,
		isGenerating,
		cancellingSessionId,
		messagesMap,
		messageIds,
		streamingMessageId,
		selectedModel,
		executionMode,
		modelOptions,
		setInputText,
		sendProjectMessage,
		sendTaskRoomMessage,
		submitApprovalDecision,
		submitQuestionAnswer,
		cancelGeneration,
		addAttachment,
		addUploadedAttachment,
		addUploadedFolderAttachment,
		removeAttachment,
		setInputFocused,
		setSelectedModel,
		setExecutionMode,
	} = useChatStore((s) => s);
	const isCancelling = Boolean(activeSessionId && cancellingSessionId === activeSessionId);
	const isAwaitingRun = streamingMessageId
		? messagesMap[streamingMessageId]?.status === "waiting"
		: false;
	const {
		activeProjectId,
		activeTaskDetailProjectId,
		activeTaskDetailTaskId,
		activeTaskDetailSessionId,
		currentView,
		projectComposerPrefill,
		projects,
		fetchTasks,
		consumeProjectComposerPrefill,
	} = useLayoutStore((s) => s);
	const { assistants, assistantsLoaded } = useDAStore((s) => s);

	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const [bidComparisonOpen, setBidComparisonOpen] = useState(false);
	const lastAppliedSelectionDraftRef = useRef<{
		id: string;
		suggestedPrompt?: string;
	} | null>(null);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const folderInputRef = useRef<HTMLInputElement>(null);
	const previousProjectSkillCodesRef = useRef<string[] | null>(null);
	const previousConnectorProjectIdRef = useRef<string | null | undefined>(undefined);
	const [showModelDropdown, setShowModelDropdown] = useState(false);
	const { draft: docxSelectionDraft } = useDocxSelectionComposerStore();

	const currentModel = modelOptions.find((m) => m.id === selectedModel);
	const isProjectVariant = variant === "project";
	const projectLayout = getProjectChatLayoutClasses(projectLayoutMode);
	const canSend =
		Boolean(inputText.trim()) &&
		!inputAttachments.some((attachment) => attachment.uploadStatus === "uploading");
	const pendingApproval = findPendingApproval(messageIds, messagesMap, activeSessionId);
	const pendingQuestion = findPendingQuestion(messageIds, messagesMap, activeSessionId);
	const currentProjectId = activeTaskDetailProjectId ?? activeProjectId;
	const currentProject = projects.find((project) => project.id === currentProjectId);
	const projectConnectorsDisabled =
		isProjectVariant && hasMultipleHumanProjectMembers(currentProject?.members ?? []);
	const { skillOptions, skillsLoading, reloadSkillOptions } = useComposerSkillOptions(
		isProjectVariant ? currentProjectId : null,
		true,
		isProjectVariant ? "project" : "all",
	);
	const handleSkillPickerOpen = useCallback(() => {
		void reloadSkillOptions();
	}, [reloadSkillOptions]);
	const { connectorOptions, connectorsLoading } = useComposerConnectorOptions({
		projectId: isProjectVariant ? currentProjectId : null,
	});
	const [selectedConnectorIds, setSelectedConnectorIds] = useState<string[]>([]);
	const handleSelectConnector = useCallback(
		(publicId: string) => {
			if (projectConnectorsDisabled) return;
			setSelectedConnectorIds((prev) => (prev.includes(publicId) ? prev : [...prev, publicId]));
		},
		[projectConnectorsDisabled],
	);
	const handleRemoveConnector = useCallback((publicId: string) => {
		setSelectedConnectorIds((prev) => prev.filter((id) => id !== publicId));
	}, []);
	const projectAssistantOptions = useMemo<ComposerAssistantOption[] | undefined>(() => {
		if (!isProjectVariant) return undefined;
		return (
			(currentProject?.members ?? [])
				// 中文注释：项目默认 AI 员工用于兜底分配，不作为输入框里可手动召唤的候选项展示。
				.filter(
					(member) =>
						member.type === "assistant" &&
						!member.isDefault &&
						!isSystemDefaultAssistant(member.publicId),
				)
				.flatMap((member) => {
					const assistant = assistants.find(
						(item) =>
							(member.publicId && item.publicId === member.publicId) ||
							(member.memberId > 0 && item.id === member.memberId),
					);
					// 中文注释：项目快照可能保留已删除或已停止的队友，@ 候选只开放当前仍部署就绪的成员。
					if (assistant && !isAssistantAvailable(assistant)) return [];
					if (!assistant && assistantsLoaded) return [];
					return [
						projectMemberToComposerAssistantOption(
							assistant
								? {
										...member,
										name: assistant.name,
										roleName: assistant.roleName,
										description: assistant.description,
										avatarUrl: assistant.avatar,
									}
								: member,
						),
					];
				})
		);
	}, [assistants, assistantsLoaded, currentProject?.members, isProjectVariant]);
	const projectSkillCodes = useMemo(
		() => skillOptions?.filter((skill) => skill.projectAssociated).map((skill) => skill.code) ?? [],
		[skillOptions],
	);
	const isNewProjectTaskView = isProjectVariant && currentView === "project";
	const isTaskDetailView =
		isProjectVariant &&
		(currentView === "taskDetail" ||
			Boolean(activeTaskDetailTaskId && activeTaskDetailSessionId && currentView !== "project"));
	const showBidComparisonTips =
		bidComparisonEntry === "tips" || (bidComparisonEntry === "auto" && isNewProjectTaskView);
	const showBidComparisonButtonOnly =
		bidComparisonEntry === "button" || (bidComparisonEntry === "auto" && isTaskDetailView);
	const showBidComparisonDialog = showBidComparisonTips || showBidComparisonButtonOnly;
	const composerUsageTips = useMemo(
		() => (showBidComparisonTips ? buildComposerUsageTips(currentProject) : []),
		[showBidComparisonTips, currentProject],
	);
	const applyUsageTip = useCallback(
		(prompt: string) => {
			if (composerRef.current) {
				composerRef.current.setContent(prompt);
				return;
			}
			setInputText(prompt);
		},
		[setInputText],
	);
	const startBidComparison = useCallback(
		async (config: BidComparisonConfig) => {
			try {
				const projectId = config.projectId || currentProjectId;
				if (!projectId) {
					toast.error("请先选择项目");
					throw new Error("请先选择项目");
				}
				const resolved = await ensureBidComparisonFilesUploaded(config, projectId);
				const attachments = bidComparisonConfigToAttachments(resolved);
				const prompt = bidComparisonPrompt(resolved);
				const outputFormat = bidComparisonOutputFormat(resolved);

				if (bidComparisonEntry === "button" || isTaskDetailView) {
					const taskId = activeTaskDetailTaskId;
					const sessionId = activeTaskDetailSessionId;
					if (!taskId || !sessionId) {
						toast.error("当前任务会话未就绪");
						throw new Error("当前任务会话未就绪");
					}
					const result = await sendTaskRoomMessage(
						prompt,
						{
							projectId,
							taskId,
							sessionId,
							scene: "bid_comparison",
							outputFormat,
						},
						attachments,
					);
					if (!result) {
						toast.error("启动标书对比失败，请稍后重试");
						throw new Error("启动标书对比失败，请稍后重试");
					}
					return;
				}

				const taskEntry = await sendProjectMessage(prompt, projectId, attachments, undefined, {
					scene: "bid_comparison",
					outputFormat,
				});
				if (!taskEntry) {
					toast.error("启动标书对比失败，请稍后重试");
					throw new Error("启动标书对比失败，请稍后重试");
				}
				if (taskEntry.project_id && taskEntry.task_id && taskEntry.session_id) {
					navigation?.goToTaskDetail(taskEntry.project_id, taskEntry.task_id, taskEntry.session_id);
				}
			} catch (err) {
				console.error("ChatInput bid comparison upload error:", err);
				const message = err instanceof Error ? err.message.trim() : "";
				const alreadyToasted =
					message === "请先选择项目" ||
					message === "当前任务会话未就绪" ||
					message === "启动标书对比失败，请稍后重试";
				if (!alreadyToasted) {
					toast.error(getRequestErrorMessage(err) ?? "启动标书对比失败");
				}
				throw err;
			}
		},
		[
			activeTaskDetailSessionId,
			activeTaskDetailTaskId,
			bidComparisonEntry,
			currentProjectId,
			isTaskDetailView,
			navigation,
			sendProjectMessage,
			sendTaskRoomMessage,
		],
	);
	const activeProjectComposerPrefill =
		isProjectVariant &&
		currentView === "project" &&
		activeProjectId &&
		projectComposerPrefill?.projectId === activeProjectId
			? projectComposerPrefill
			: undefined;

	useEffect(() => {
		if (!docxSelectionDraft || lastAppliedSelectionDraftRef.current?.id === docxSelectionDraft.id) {
			return;
		}
		const currentTokens = composerRef.current?.getComposerTokens() ?? [];
		const next = applyDocxSelectionDraftToComposer({
			value: inputText,
			tokens: currentTokens,
			draft: docxSelectionDraft,
			previousSuggestedPrompt: lastAppliedSelectionDraftRef.current?.suggestedPrompt,
		});
		lastAppliedSelectionDraftRef.current = {
			id: docxSelectionDraft.id,
			suggestedPrompt: docxSelectionDraft.suggestedPrompt,
		};
		if (composerRef.current) {
			composerRef.current.setContent(next.value, next.tokens);
		} else {
			setInputText(next.value);
		}
		setInputFocused(true);
	}, [docxSelectionDraft, inputText, setInputFocused, setInputText]);

	useEffect(() => {
		if (!isProjectVariant) {
			previousProjectSkillCodesRef.current = null;
			return;
		}

		const previousCodes = previousProjectSkillCodesRef.current;
		previousProjectSkillCodesRef.current = projectSkillCodes;
		if (!previousCodes) return;

		const currentCodes = new Set(projectSkillCodes);
		const removedCodes = previousCodes.filter((code) => !currentCodes.has(code));
		if (removedCodes.length === 0) return;

		// 中文注释：项目维度移除技能后，按 token.id（catalog code）清理输入框里的技能 mention。
		if (!composerRef.current) return;
		for (const code of removedCodes) {
			composerRef.current.removeSkill(code);
		}
	}, [isProjectVariant, projectSkillCodes]);

	// 中文注释：连接器关联是项目级配置，切换项目后清空已选连接器，避免跨项目残留。
	useEffect(() => {
		const key = isProjectVariant ? currentProjectId : undefined;
		if (previousConnectorProjectIdRef.current === key) {
			return;
		}
		previousConnectorProjectIdRef.current = key;
		if (selectedConnectorIds.length > 0) {
			setSelectedConnectorIds([]);
		}
	}, [currentProjectId, isProjectVariant, selectedConnectorIds.length]);

	// 中文注释：项目进入多人真人协作状态后立即清理旧选择，避免成员变更与发送并发时携带连接器。
	useEffect(() => {
		if (projectConnectorsDisabled && selectedConnectorIds.length > 0) {
			setSelectedConnectorIds([]);
		}
	}, [projectConnectorsDisabled, selectedConnectorIds.length]);

	const submitMessage = useCallback(async () => {
		// 中文注释：真实 SessionEvents 当前由单条 SSE 连接接管，生成中先阻止重复发送。
		if (isGenerating) return;
		const trimmedInput = inputText.trim();
		if (trimmedInput) {
			const composerTokens = composerRef.current?.getComposerTokens() ?? [];
			const prepared = prepareOutgoingComposer(inputText, composerTokens);
			const activeSelectionReference = docxSelectionDraft
				? composerTokens.find(
						(token) =>
							token.kind === "reference" &&
							token.id === docxSelectionDraft.referenceId &&
							inputText.slice(token.start, token.end) === token.label,
					)
				: undefined;
			let outgoingContent = prepared.content;
			const outgoingAttachments = inputAttachments;
			let composerMetadata = prepared.metadata;
			let pendingVersionSync: PendingDocxVersionSync | null = null;

			if (docxSelectionDraft && activeSelectionReference) {
				const visibleSnapshot = removeDocxReferenceFromComposer(inputText, composerTokens);
				const userPrompt = visibleSnapshot.value.trim();
				if (!userPrompt) {
					toast.info("请补充希望如何修改这段内容");
					return;
				}
				const request = buildDocxSelectionPromptRequest({
					prompt: userPrompt,
					file: docxSelectionDraft.file,
					selection: docxSelectionDraft.selection,
				});
				if (
					!docxSelectionDraft.file.versionPublicId &&
					!docxSelectionDraft.file.publicId &&
					!docxSelectionDraft.file.projectPath
				) {
					toast.error("当前文件缺少可编辑的文件标识");
					return;
				}
				outgoingContent = request.content;
				const visibleMetadata = buildComposerMetadata(inputText, composerTokens);
				composerMetadata = {
					...prepared.metadata,
					displayContent: trimmedInput,
					displayComposerTokens: visibleMetadata?.composerTokens,
				};
				pendingVersionSync = await resolveDocxVersionSync(docxSelectionDraft);
			}

			let submitted: unknown;
			if (
				isProjectVariant &&
				currentView === "taskDetail" &&
				activeTaskDetailProjectId &&
				activeTaskDetailTaskId &&
				activeTaskDetailSessionId
			) {
				submitted = await sendTaskRoomMessage(
					outgoingContent,
					{
						projectId: activeTaskDetailProjectId,
						taskId: activeTaskDetailTaskId,
						sessionId: activeTaskDetailSessionId,
						metadata: composerMetadata,
						connectorIds: projectConnectorsDisabled ? [] : selectedConnectorIds,
					},
					outgoingAttachments,
				);
			} else if (isProjectVariant && currentView === "project") {
				try {
					const taskEntry = await sendProjectMessage(
						outgoingContent,
						activeProjectId,
						outgoingAttachments,
						composerMetadata,
						{ connectorIds: projectConnectorsDisabled ? [] : selectedConnectorIds },
					);
					submitted = taskEntry;
					if (taskEntry?.project_id && taskEntry?.task_id && taskEntry.session_id) {
						// 中文注释：项目首页创建出真实任务后，立即跳到任务详情页，避免仍停留在项目首页的新建任务视图。
						navigation?.goToTaskDetail(
							taskEntry.project_id,
							taskEntry.task_id,
							taskEntry.session_id,
						);
					}
				} catch (err) {
					console.error("ChatInput createInitialMessage error:", err);
					toast.error(`创建任务失败：${getRequestErrorMessage(err) ?? "请稍后重试"}`);
					return;
				}
			} else {
				// 中文注释：任务详情依赖路径中的 sessionId；未知场景拒绝发送。
				return;
			}

			if (!submitted) return;
			const hasInvokedSkill = hasComposerSkillTokens(outgoingContent);
			if (isProjectVariant && currentProjectId && hasInvokedSkill) {
				void reloadSkillOptions();
			}
			// 中文注释：发送成功后清空已选连接器；失败时不进入这里，保留以便重试。
			setSelectedConnectorIds([]);
			if (docxSelectionDraft && activeSelectionReference) {
				docxSelectionComposerActions.markSubmitted(pendingVersionSync);
				docxSelectionComposerActions.clearDraft(docxSelectionDraft.id);
				lastAppliedSelectionDraftRef.current = null;
			} else if (docxSelectionDraft) {
				// 中文注释：用户手动删掉引用 token 后，本次按普通消息发送，同时清除失效的选区草稿。
				docxSelectionComposerActions.clearDraft(docxSelectionDraft.id);
				lastAppliedSelectionDraftRef.current = null;
			}
		}
	}, [
		inputText,
		inputAttachments,
		isProjectVariant,
		currentProjectId,
		currentView,
		activeProjectId,
		activeTaskDetailProjectId,
		activeTaskDetailTaskId,
		activeTaskDetailSessionId,
		isGenerating,
		docxSelectionDraft,
		navigation,
		sendProjectMessage,
		sendTaskRoomMessage,
		reloadSkillOptions,
		selectedConnectorIds,
		projectConnectorsDisabled,
	]);

	const uploadProjectAttachment = useCallback(
		async (file: File) => {
			if (!isProjectVariant || !currentProjectId) {
				return false;
			}
			try {
				const { cancelled } = await addUploadedAttachment(currentProjectId, file);
				if (cancelled) return true;
				toast.success(COMPOSER_UPLOAD_SUCCESS_MESSAGE);
				return true;
			} catch (err) {
				const message = err instanceof Error ? err.message : "文件上传失败";
				console.error("ChatInput upload project attachment error:", err);
				toast.error(message);
				return true;
			}
		},
		[currentProjectId, addUploadedAttachment, isProjectVariant],
	);

	const handlePasteFiles = useCallback(
		(e: React.ClipboardEvent<HTMLElement>) => {
			const files = Array.from(e.clipboardData.files);
			if (!files.length) return;

			for (const file of files) {
				if (isEmptyUploadFile(file)) {
					toast.error(COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE);
					continue;
				}
				if (!isComposerUploadAllowedFile(file)) {
					toast.error(COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE);
					continue;
				}
				void uploadProjectAttachment(file).then((uploaded) => {
					if (!uploaded) {
						addAttachment(file);
					}
				});
			}
		},
		[addAttachment, uploadProjectAttachment],
	);

	const handleFileSelect = useCallback(
		async (e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files ?? []);
			for (const file of files) {
				if (isEmptyUploadFile(file)) {
					toast.error(COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE);
					continue;
				}
				if (!isComposerUploadAllowedFile(file)) {
					toast.error(COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE);
					continue;
				}
				const uploaded = await uploadProjectAttachment(file);
				if (!uploaded) {
					addAttachment(file);
				}
			}
			e.target.value = "";
		},
		[addAttachment, uploadProjectAttachment],
	);

	const handleFolderSelect = useCallback(
		async (e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files ?? []);
			e.target.value = "";
			if (!files.length) return;

			if (isFolderUploadSizeExceeded(files)) {
				toast.error(FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE, { position: "bottom-right" });
				return;
			}

			if (!isProjectVariant || !currentProjectId) {
				toast.error("请在项目对话中上传文件夹");
				return;
			}

			try {
				const { message, cancelled } = await addUploadedFolderAttachment(currentProjectId, files);
				if (cancelled) return;
				if (message.includes("已跳过")) {
					toast.info(message, { position: "bottom-right" });
				} else {
					toast.success(message || "文件夹上传成功");
				}
			} catch (err) {
				const message = err instanceof Error ? err.message : "文件夹上传失败";
				console.error("ChatInput upload folder error:", err);
				toast.error(message, { position: "bottom-right" });
			}
		},
		[addUploadedFolderAttachment, currentProjectId, isProjectVariant],
	);

	const handleSend = useCallback(() => {
		submitMessage();
	}, [submitMessage]);

	const handlePlanAnswer = useCallback(
		(messageId: string, requestId: string, answers: string[][]) => {
			// Determine execution mode from answer: "Yes" → default, "No" → plan
			const yesAnswer = answers.some((ans) => ans.length > 0 && ans[0]?.toLowerCase() === "yes");
			setExecutionMode(yesAnswer ? "default" : "plan");
			submitQuestionAnswer(messageId, requestId, answers);
		},
		[submitQuestionAnswer, setExecutionMode],
	);

	if (pendingQuestion) {
		const isPlanConfirmation = pendingQuestion.question.interactionType === "plan_confirmation";
		return (
			<QuestionAnswerInput
				question={pendingQuestion.question}
				messageId={pendingQuestion.message.id}
				variant={variant}
				projectLayout={projectLayout}
				onAnswer={isPlanConfirmation ? handlePlanAnswer : submitQuestionAnswer}
			/>
		);
	}

	if (pendingApproval) {
		return (
			<ApprovalDecisionInput
				approval={pendingApproval.approval}
				messageId={pendingApproval.message.id}
				variant={variant}
				projectLayout={projectLayout}
				onDecide={submitApprovalDecision}
			/>
		);
	}

	return (
		<div
			data-slot="chat-input"
			className={cn(
				"bg-transparent px-5 pb-5 sm:px-6 lg:px-8",
				isProjectVariant && cn("bg-white pb-8", projectLayout.shell),
			)}
		>
			<div className={cn("mx-auto w-full max-w-[1040px]", isProjectVariant && projectLayout.inner)}>
				{showBidComparisonTips ? (
					<ComposerUsageTipsPanel
						tips={composerUsageTips}
						onApply={applyUsageTip}
						onBidComparisonClick={() => setBidComparisonOpen(true)}
					/>
				) : null}
				{showBidComparisonButtonOnly ? (
					<div className="mb-4">
						<BidComparisonEntryButton
							disabled={isGenerating}
							onClick={() => setBidComparisonOpen(true)}
						/>
					</div>
				) : null}
				{showBidComparisonDialog ? (
					<BidComparisonConfigDialog
						open={bidComparisonOpen}
						onOpenChange={setBidComparisonOpen}
						onSave={startBidComparison}
						initialProjectId={currentProjectId}
						initialTaskId={showBidComparisonButtonOnly ? activeTaskDetailTaskId : null}
						hideTargetPicker
						allowSelectTask={false}
						continueInCurrentTask={showBidComparisonButtonOnly}
						lockProjectSelection
						onProjectChange={fetchTasks}
						projects={projects.map((project) => ({
							id: project.id,
							name: project.name,
							tasks: project.tasks,
						}))}
					/>
				) : null}
				<div
					className={cn(
						// 中文注释：focus 时使用无偏移阴影，避免 shadow-xl 只在下方显影
						"relative rounded-2xl bg-white/95 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-[0_0_24px_rgba(15,23,42,0.12)] focus-within:ring-slate-200/70",
						isProjectVariant && "flex flex-col rounded-2xl bg-white px-4 py-2 ring-slate-200",
					)}
				>
					<input
						ref={fileInputRef}
						type="file"
						className="hidden"
						accept={projectAttachmentAccept}
						multiple
						onChange={handleFileSelect}
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
					{inputAttachments.length > 0 && (
						<AttachmentPreview
							attachments={inputAttachments}
							onPreview={openPendingAttachmentPreview}
							onRemove={removeAttachment}
						/>
					)}
					<div className="min-w-0">
						<StructuredComposer
							ref={composerRef}
							value={inputText}
							onChange={setInputText}
							onSubmit={submitMessage}
							onPasteFiles={handlePasteFiles}
							onFocus={() => setInputFocused(true)}
							onBlur={() => setInputFocused(false)}
							placeholder={
								isProjectVariant
									? isNewProjectTaskView
										? "在这里输入需求或描述目标。使用@召唤队友、/调用技能..."
										: "继续帮你做什么？"
									: "请描述您的问题，支持 Ctrl+V 粘贴图片。输入 @ 提及成员，/ 使用命令，# 引用工作项。"
							}
							isProjectVariant={isProjectVariant}
							assistantOptions={projectAssistantOptions}
							skillOptions={skillOptions}
							skillsLoading={skillsLoading}
							onSkillPickerOpen={handleSkillPickerOpen}
							assistantSelectionMode="single"
							prefill={activeProjectComposerPrefill}
							onPrefillConsumed={consumeProjectComposerPrefill}
						/>
					</div>
					<div
						className={cn(
							"flex items-center justify-between",
							isProjectVariant ? "border-t border-[var(--leros-chat-ai-border)] pt-3" : "px-4 pb-3",
						)}
					>
						<div className="flex items-center gap-1">
							{isProjectVariant ? (
								<ComposerActionBar
									inputValue={inputText}
									composerRef={composerRef}
									onUpload={() => fileInputRef.current?.click()}
									onUploadFolder={() => folderInputRef.current?.click()}
									assistantOptions={projectAssistantOptions}
									skillOptions={skillOptions}
									skillsLoading={skillsLoading}
									onSkillPickerOpen={handleSkillPickerOpen}
									assistantSelectionMode="single"
									executionMode={executionMode}
									setExecutionMode={setExecutionMode}
									isGenerating={isGenerating}
									connectorOptions={connectorOptions}
									connectorsLoading={connectorsLoading}
									selectedConnectorIds={projectConnectorsDisabled ? [] : selectedConnectorIds}
									onSelectConnector={handleSelectConnector}
									onRemoveConnector={handleRemoveConnector}
									connectorDisabled={projectConnectorsDisabled}
									connectorDisabledReason={PROJECT_MCP_COLLABORATION_WARNING}
								/>
							) : (
								<>
									<Tooltip>
										<TooltipTrigger
											aria-label="Plan Mode"
											aria-pressed={executionMode === "plan"}
											disabled={isGenerating}
											onClick={() =>
												setExecutionMode(executionMode === "plan" ? "default" : "plan")
											}
											className={cn(
												"inline-flex h-7 w-7 items-center justify-center rounded-md text-slate-400 transition-colors hover:text-slate-600 hover:bg-slate-100",
												executionMode === "plan" &&
													"bg-[var(--leros-primary-softer)] !text-[var(--leros-primary)] hover:bg-[var(--leros-primary-soft)]",
											)}
										>
											<ClipboardPenLine className="size-4" />
										</TooltipTrigger>
										<TooltipContent side="top">
											计划模式会先拆解任务并制定方案，提升复杂任务的执行质量
										</TooltipContent>
									</Tooltip>
									<Button
										variant="ghost"
										size="icon-sm"
										className="text-slate-400 hover:text-slate-600"
										onClick={() => fileInputRef.current?.click()}
									>
										<Paperclip className="size-4" />
									</Button>
									<Button
										variant="ghost"
										size="icon-sm"
										className="text-slate-400 hover:text-slate-600"
										aria-label="选择 AI 队友"
										onMouseDown={(event) => event.preventDefault()}
										onClick={() => composerRef.current?.openAssistantPicker()}
									>
										<AtSign className="size-4" />
									</Button>
									<div className="relative">
										<button
											type="button"
											onClick={() => setShowModelDropdown(!showModelDropdown)}
											className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-slate-500 transition-colors hover:bg-slate-100"
										>
											{currentModel?.label ?? "GPT-4"}
											<ChevronDown className="size-3" />
										</button>
										{showModelDropdown && (
											<div className="absolute bottom-full left-0 mb-1 rounded-lg border border-slate-200 bg-white shadow-lg py-1 z-10 min-w-[140px]">
												{modelOptions.map((model) => (
													<button
														key={model.id}
														type="button"
														onClick={() => {
															setSelectedModel(model.id);
															setShowModelDropdown(false);
														}}
														className={cn(
															"flex w-full items-center gap-2 px-3 py-1.5 text-sm hover:bg-slate-50 transition-colors",
															model.id === selectedModel
																? "text-blue-600 bg-blue-50/50"
																: "text-slate-600",
														)}
													>
														<span>{model.label}</span>
														<span className="text-xs text-slate-400">{model.provider}</span>
													</button>
												))}
											</div>
										)}
									</div>
								</>
							)}
						</div>
						<div className="flex items-center gap-2">
							{isGenerating ? (
								<Button
									variant={isProjectVariant ? "ghost" : "outline"}
									size={isProjectVariant ? "icon" : "sm"}
									className={cn(
										"border-red-200 text-red-500 hover:bg-red-50",
										isProjectVariant && "size-9 rounded-xl",
									)}
									onClick={cancelGeneration}
									disabled={isCancelling || isAwaitingRun}
								>
									<CircleStop className={cn("size-3.5", !isProjectVariant && "mr-1")} />
									{!isProjectVariant &&
										(isCancelling ? "停止中…" : isAwaitingRun ? "准备中…" : "停止")}
								</Button>
							) : (
								<Button
									size={isProjectVariant ? "icon" : "sm"}
									className={cn(
										// 中文注释：!text-white 覆盖 Button default variant 的 text-primary-foreground
										"bg-black !text-white shadow-sm hover:bg-blue-700 disabled:bg-[#f3f3f4] disabled:!text-slate-400",
										isProjectVariant ? "size-9 min-w-0 rounded-xl" : "h-8 min-w-[4.5rem]",
									)}
									onClick={handleSend}
									disabled={!canSend}
								>
									<SendHorizonal
										className={cn(
											"size-3.5",
											!isProjectVariant && "mr-1",
											// 中文注释：直接在图标上设色，避免 Button 默认 variant 覆盖 currentColor
											canSend
												? "fill-white stroke-white text-white"
												: "fill-none stroke-current text-current",
										)}
									/>
									{!isProjectVariant && "发送"}
								</Button>
							)}
						</div>
					</div>
				</div>
				{/* {isProjectVariant && (
					<div className="mt-3 text-center text-xs text-slate-500">
						按{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Enter</kbd>{" "}
						发送，按{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Shift</kbd>{" "}
						+{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Enter</kbd>{" "}
						换行，也支持{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Ctrl</kbd>/
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">⌘</kbd> +{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Enter</kbd>{" "}
						发送
					</div>
				)} */}
			</div>
		</div>
	);
}

type PendingApprovalRef = {
	message: Message;
	approval: ApprovalRequest;
};

function findPendingApproval(
	messageIds: string[],
	messagesMap: Record<string, Message>,
	activeSessionId: string | null,
): PendingApprovalRef | null {
	for (let index = messageIds.length - 1; index >= 0; index -= 1) {
		const message = messagesMap[messageIds[index] ?? ""];
		if (!message) continue;
		if (activeSessionId && message.conversationId !== activeSessionId) continue;

		const approval = [...(message.approvals ?? [])]
			.reverse()
			.find(
				(item) =>
					item.status === "pending" || item.status === "submitting" || item.status === "error",
			);
		if (approval) return { message, approval };
	}
	return null;
}

type PendingQuestionRef = {
	message: Message;
	question: QuestionRequest;
};

function findPendingQuestion(
	messageIds: string[],
	messagesMap: Record<string, Message>,
	_activeSessionId: string | null,
): PendingQuestionRef | null {
	// Only check the last message — a question is only "active" if it's the
	// most recent interaction and hasn't been answered yet.
	const lastId = messageIds[messageIds.length - 1];
	if (!lastId) return null;
	const lastMessage = messagesMap[lastId];
	if (!lastMessage?.questions?.length) return null;

	const question = lastMessage.questions[lastMessage.questions.length - 1];
	if (
		question &&
		(question.status === "pending" ||
			question.status === "submitting" ||
			question.status === "error")
	) {
		return { message: lastMessage, question };
	}
	return null;
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

async function resolveDocxVersionSync(
	draft: DocxSelectionComposerDraft,
): Promise<PendingDocxVersionSync | null> {
	const { file } = draft;
	const chainFilePublicId = file.publicId || file.versionPublicId;
	if (!file.projectId || !chainFilePublicId || !file.publicId) return null;

	let baselinePublicId = file.publicId;
	let baselineVersionNo = file.versionNo ?? 0;
	try {
		const response = await projectFileApi.versions(file.projectId, chainFilePublicId);
		if (response.data.code === 0) {
			const latest = getLatestProjectFileVersion(response.data.data);
			if (latest) {
				baselinePublicId = latest.public_id;
				baselineVersionNo = latest.version_no;
			}
		}
	} catch (error) {
		console.warn("Resolve DOCX version baseline error:", error);
	}

	return {
		id: `docx-selection-submit-${Date.now()}`,
		projectId: file.projectId,
		taskId: file.taskId,
		chainFilePublicId,
		expectedPreviewPublicId: file.publicId,
		baselinePublicId,
		baselineVersionNo,
		selectedVersionPublicId: draft.selectedVersionPublicId,
	};
}

function projectMemberToComposerAssistantOption(member: ProjectMember): ComposerAssistantOption {
	const id = member.publicId || String(member.memberId);
	return {
		id,
		code: id,
		name: member.name,
		roleName: member.roleName,
		// 中文注释：DetailProject 当前可能只返回成员基础信息，这里用项目队友信息补齐输入框候选项。
		description: member.description || (member.isDefault ? "默认 AI 队友" : "AI 队友"),
		avatarUrl: member.avatarUrl,
	};
}

function ApprovalDecisionInput({
	approval,
	messageId,
	variant,
	projectLayout,
	onDecide,
}: {
	approval: ApprovalRequest;
	messageId: string;
	variant: "default" | "project";
	projectLayout?: ReturnType<typeof getProjectChatLayoutClasses>;
	onDecide: (
		messageId: string,
		requestId: string,
		action: ApprovalAction,
		reason?: string,
	) => void | Promise<void>;
}) {
	const [expanded, setExpanded] = useState(false);
	const [alwaysAllow, setAlwaysAllow] = useState(false);
	const isProjectVariant = variant === "project";
	const layout = projectLayout ?? getProjectChatLayoutClasses("sidebar-expanded");
	const isSubmitting = approval.status === "submitting";
	const argumentText = approval.arguments ? JSON.stringify(approval.arguments, null, 2) : "";
	const detailText = getApprovalDetail(approval);

	const handleDecision = useCallback(
		(action: ApprovalAction) => {
			onDecide(messageId, approval.requestId, action);
		},
		[approval.requestId, messageId, onDecide],
	);

	useEffect(() => {
		if (isSubmitting) return;

		const handleKeyDown = (event: KeyboardEvent) => {
			if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
			if (event.key === "Escape") {
				event.preventDefault();
				handleDecision("deny");
				return;
			}
			if (event.key === "Enter") {
				event.preventDefault();
				handleDecision(alwaysAllow ? "always" : "approve");
			}
		};

		window.addEventListener("keydown", handleKeyDown);
		return () => window.removeEventListener("keydown", handleKeyDown);
	}, [alwaysAllow, handleDecision, isSubmitting]);

	return (
		<div
			data-slot="approval-decision-input"
			className={cn(
				"bg-transparent px-5 pb-5 sm:px-6 lg:px-8",
				isProjectVariant && cn("bg-white pb-8", layout.shell),
			)}
		>
			<div className={cn("mx-auto w-full max-w-[1040px]", isProjectVariant && layout.inner)}>
				<div className="overflow-hidden rounded-[18px] border border-slate-200 bg-white text-slate-800 shadow-[0_12px_32px_rgba(15,23,42,0.08)]">
					<div className="px-4 pb-4 pt-3.5">
						<div className="mb-3 flex items-center gap-2 text-sm text-slate-500">
							<ShieldAlert className="size-4 text-slate-500" />
							<span className="font-medium">{approval.toolName}</span>
							<ApprovalStatusBadge approval={approval} />
						</div>
						<div className="text-[15px] leading-6 text-slate-950">
							允许 Lework 执行
							<span className="mx-1 font-medium">{approval.description}</span>
							吗？
						</div>
						{detailText && (
							<div className="mt-1.5 overflow-x-auto whitespace-nowrap pb-1 text-sm leading-5 text-slate-500">
								{detailText}
							</div>
						)}
						{approval.error && (
							<div className="mt-2 flex items-center gap-1.5 text-xs text-red-600">
								<AlertCircle className="size-3.5" />
								<span>{approval.error}</span>
							</div>
						)}
					</div>

					<div className="flex flex-col gap-3 border-t border-slate-100 bg-slate-50/70 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
						<div className="flex min-w-0 items-center gap-2">
							<Checkbox
								checked={alwaysAllow}
								onCheckedChange={(checked) => setAlwaysAllow(checked === true)}
								disabled={isSubmitting}
								className="border-slate-300 bg-white data-checked:border-slate-950 data-checked:bg-slate-950"
							/>
							<span className="truncate text-sm text-slate-500">以后总是允许此工具</span>
							{argumentText && (
								<Button
									type="button"
									variant="ghost"
									size="icon-xs"
									className="text-slate-400 hover:text-slate-700"
									onClick={() => setExpanded(!expanded)}
									title="查看参数"
								>
									<ChevronDown
										className={cn("size-3.5 transition-transform", expanded && "rotate-180")}
									/>
								</Button>
							)}
						</div>
						<div className="flex shrink-0 items-center justify-end gap-2">
							<Button
								type="button"
								variant="ghost"
								size="sm"
								onClick={() => handleDecision("deny")}
								disabled={isSubmitting}
								className="text-slate-500 hover:bg-transparent hover:text-slate-950"
							>
								取消
								<span className="rounded-md bg-slate-200/80 px-1.5 py-0.5 text-xs text-slate-700">
									Esc
								</span>
							</Button>
							<Button
								type="button"
								size="sm"
								onClick={() => handleDecision(alwaysAllow ? "always" : "approve")}
								disabled={isSubmitting}
								className="rounded-full bg-slate-950 px-4 text-white hover:bg-slate-800"
							>
								{isSubmitting && <LoaderCircle className="size-3.5 animate-spin" />}
								允许
								<span className="rounded-md bg-white/15 px-1.5 py-0.5 text-xs text-white/85">
									↵
								</span>
							</Button>
						</div>
					</div>
					{expanded && argumentText && (
						<div className="border-t border-slate-100 bg-white px-4 py-3">
							<pre className="max-h-48 overflow-auto whitespace-pre-wrap text-xs leading-5 text-slate-600">
								{argumentText}
							</pre>
						</div>
					)}
				</div>
			</div>
		</div>
	);
}

function ApprovalStatusBadge({ approval }: { approval: ApprovalRequest }) {
	switch (approval.status) {
		case "submitting":
			return (
				<Badge className="bg-slate-100 text-slate-600">
					<LoaderCircle className="size-3 animate-spin" />
					提交中
				</Badge>
			);
		case "error":
			return <Badge variant="destructive">提交失败</Badge>;
		default:
			return <Badge className="bg-slate-100 text-slate-600">等待确认</Badge>;
	}
}

function getApprovalDetail(approval: ApprovalRequest): string {
	const command = approval.arguments?.command;
	if (typeof command === "string" && command.trim()) return command.trim();

	const url = approval.arguments?.url;
	if (typeof url === "string" && url.trim()) return url.trim();

	return "";
}
