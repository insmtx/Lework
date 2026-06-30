"use client";

import { type ProjectSkill, useChatStore, useLayoutStore } from "@leros/store";
import type {
	ApprovalAction,
	ApprovalRequest,
	Attachment,
	ComposerToken,
	Message,
	MessageMetadata,
	QuestionRequest,
} from "@leros/store/types/chat";
import { Badge } from "@leros/ui/components/ui/badge";
import { Button } from "@leros/ui/components/ui/button";
import { Checkbox } from "@leros/ui/components/ui/checkbox";
import { cn } from "@leros/ui/lib/utils";
import {
	AlertCircle,
	AtSign,
	ChevronDown,
	CircleStop,
	LoaderCircle,
	Paperclip,
	ClipboardList,
	SendHorizonal,
	ShieldAlert,
	X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import type { AppNavigation } from "../layout";
import {
	getProjectChatLayoutClasses,
	type ProjectChatLayoutMode,
} from "../layout/project-chat-layout";
import { ComposerActionBar } from "./ComposerActionBar";
import { PlanConfirmationInput } from "./PlanConfirmationInput";
import { QuestionAnswerInput } from "./QuestionAnswerInput";
import {
	type ComposerSkillOption,
	StructuredComposer,
	type StructuredComposerHandle,
} from "./StructuredComposer";

// 只放开当前已有稳定预览能力的文档类型，避免上传后落到不可预览的兜底体验。
export const PROJECT_ATTACHMENT_ACCEPT = "image/*,.pdf,.txt,.md,.json,.xlsx,.xls,.csv,.docx,.pptx";

export function ChatInput({
	variant = "default",
	projectLayoutMode = "sidebar-expanded",
	navigation,
}: {
	variant?: "default" | "project";
	/** 项目页聊天区布局：随右侧栏展开/收起切换宽度与留白 */
	projectLayoutMode?: ProjectChatLayoutMode;
	navigation?: AppNavigation;
}) {
	const {
		activeSessionId,
		inputText,
		inputAttachments,
		isGenerating,
		messagesMap,
		messageIds,
		selectedModel,
		executionMode,
		modelOptions,
		setInputText,
		sendMessage,
		sendProjectMessage,
		submitApprovalDecision,
		submitQuestionAnswer,
		cancelGeneration,
		addAttachment,
		addUploadedAttachment,
		removeAttachment,
		setInputFocused,
		setSelectedModel,
		setExecutionMode,
	} = useChatStore((s) => s);
	const { activeProjectId, activeTaskDetailProjectId, currentView, projects } = useLayoutStore(
		(s) => s,
	);

	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const previousProjectSkillLabelsRef = useRef<string[] | null>(null);
	const [showModelDropdown, setShowModelDropdown] = useState(false);

	const currentModel = modelOptions.find((m) => m.id === selectedModel);
	const isProjectVariant = variant === "project";
	const projectLayout = getProjectChatLayoutClasses(projectLayoutMode);
	const canSend = Boolean(inputText.trim());
	const pendingApproval = findPendingApproval(messageIds, messagesMap, activeSessionId);
	const pendingQuestion = findPendingQuestion(messageIds, messagesMap, activeSessionId);
	const currentProjectId = activeTaskDetailProjectId ?? activeProjectId;
	const currentProject = projects.find((project) => project.id === currentProjectId);
	const projectSkillOptions = useMemo<ComposerSkillOption[] | undefined>(() => {
		if (!isProjectVariant) return undefined;
		return (currentProject?.skills ?? []).map(projectSkillToComposerOption);
	}, [currentProject?.skills, isProjectVariant]);
	const projectSkillLabels = useMemo(
		() => projectSkillOptions?.map((skill) => skill.label) ?? [],
		[projectSkillOptions],
	);

	useEffect(() => {
		if (!isProjectVariant) {
			previousProjectSkillLabelsRef.current = null;
			return;
		}

		const previousLabels = previousProjectSkillLabelsRef.current;
		previousProjectSkillLabelsRef.current = projectSkillLabels;
		if (!previousLabels) return;

		const currentLabels = new Set(projectSkillLabels);
		const removedLabels = previousLabels.filter((label) => !currentLabels.has(label));
		if (removedLabels.length === 0) return;

		const nextInput = removeSkillDirectives(inputText, removedLabels);
		if (nextInput !== inputText) {
			// 中文注释：项目维度移除技能后，同步清理输入框中已经插入的对应技能指令。
			setInputText(nextInput);
		}
	}, [inputText, isProjectVariant, projectSkillLabels, setInputText]);

	const submitMessage = useCallback(async () => {
		// 中文注释：输入区先做一次生成态拦截，避免回车绕过按钮态再次触发发送。
		if (isGenerating) return;
		// 仅上传附件而无文字时接口会报错，因此必须输入内容才可发送
		const trimmedInput = inputText.trim();
		if (trimmedInput) {
			const composerMetadata = buildComposerMetadata(
				inputText,
				composerRef.current?.getComposerTokens() ?? [],
			);
			if (isProjectVariant && currentView === "project") {
				const taskEntry = await sendProjectMessage(
					trimmedInput,
					activeProjectId,
					inputAttachments,
					composerMetadata,
				);
				if (taskEntry?.project_id && taskEntry?.task_id) {
					// 中文注释：项目首页创建出真实任务后，立即跳到任务详情页，避免仍停留在项目首页的新建任务视图。
					navigation?.goToTaskDetail(
						taskEntry.project_id,
						taskEntry.task_id,
						taskEntry.session_id ?? null,
					);
				}
				return;
			}
			sendMessage(trimmedInput, inputAttachments, composerMetadata);
		}
	}, [
		inputText,
		inputAttachments,
		isProjectVariant,
		currentView,
		activeProjectId,
		isGenerating,
		navigation,
		sendMessage,
		sendProjectMessage,
	]);

	const handlePasteFiles = useCallback(
		(e: React.ClipboardEvent<HTMLElement>) => {
			const files = Array.from(e.clipboardData.files);
			for (const file of files) {
				if (file.type.startsWith("image/") || file.type.startsWith("text/")) {
					addAttachment(file);
				}
			}
		},
		[addAttachment],
	);

	const handleFileSelect = useCallback(
		async (e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files ?? []);
			const projectId = activeProjectId;
			for (const file of files) {
				if (isProjectVariant && projectId) {
					try {
						const { message } = await addUploadedAttachment(projectId, file);
						toast.success(message || "文件上传成功");
					} catch (err) {
						const message = err instanceof Error ? err.message : "文件上传失败";
						console.error("ChatInput upload project attachment error:", err);
						toast.error(message);
					}
					continue;
				}
				addAttachment(file);
			}
			e.target.value = "";
		},
		[activeProjectId, addAttachment, addUploadedAttachment, isProjectVariant],
	);

	const handleSend = useCallback(() => {
		submitMessage();
	}, [submitMessage]);

	if (pendingQuestion) {
		if (pendingQuestion.question.interactionType === "plan_confirmation") {
			return (
				<PlanConfirmationInput
					question={pendingQuestion.question}
					messageId={pendingQuestion.message.id}
					variant={variant}
					projectLayout={projectLayout}
					onAnswer={submitQuestionAnswer}
					onExecute={() => setExecutionMode("default")}
					onRevise={() => setExecutionMode("plan")}
				/>
			);
		}
		return (
			<QuestionAnswerInput
				question={pendingQuestion.question}
				messageId={pendingQuestion.message.id}
				variant={variant}
				projectLayout={projectLayout}
				onAnswer={submitQuestionAnswer}
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
				{inputAttachments.length > 0 && (
					<AttachmentPreview attachments={inputAttachments} onRemove={removeAttachment} />
				)}
				<div
					className={cn(
						// 中文注释：focus 时使用无偏移阴影，避免 shadow-xl 只在下方显影
						"relative rounded-2xl bg-white/95 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-[0_0_24px_rgba(15,23,42,0.12)] focus-within:ring-slate-200/70",
						isProjectVariant && "rounded-2xl bg-white px-4 py-2 ring-slate-200",
					)}
				>
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
								? "这会儿帮你做些什么？@引用项目"
								: "请描述您的问题，支持 Ctrl+V 粘贴图片。输入 @ 提及成员，/ 使用命令，# 引用工作项。"
						}
						isProjectVariant={isProjectVariant}
						projectSkillOptions={projectSkillOptions}
					/>
					<input
						ref={fileInputRef}
						type="file"
						className="hidden"
						accept={PROJECT_ATTACHMENT_ACCEPT}
						multiple
						onChange={handleFileSelect}
					/>
					<div
						className={cn(
							"flex items-center justify-between px-4 pb-3",
							isProjectVariant && "px-0 pb-0",
						)}
					>
						<div className="flex items-center gap-1">
							{isProjectVariant ? (
								<ComposerActionBar
									inputValue={inputText}
									composerRef={composerRef}
									onUpload={() => fileInputRef.current?.click()}
									projectSkillOptions={projectSkillOptions}
								/>
							) : (
								<>
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
							<Button
								type="button"
								variant={executionMode === "plan" ? "secondary" : "ghost"}
								size="sm"
								disabled={isGenerating}
								aria-pressed={executionMode === "plan"}
								onClick={() =>
									setExecutionMode(executionMode === "plan" ? "default" : "plan")
								}
								className={cn(
									"h-8 gap-1.5 text-xs",
									executionMode === "plan" && "bg-blue-50 text-blue-700 hover:bg-blue-100",
								)}
							>
								<ClipboardList className="size-3.5" />
								Plan Mode
							</Button>
							{isGenerating ? (
								<Button
									variant={isProjectVariant ? "ghost" : "outline"}
									size={isProjectVariant ? "icon" : "sm"}
									className={cn(
										"border-red-200 text-red-500 hover:bg-red-50",
										isProjectVariant && "size-9 rounded-xl",
									)}
									onClick={cancelGeneration}
								>
									<CircleStop className={cn("size-3.5", !isProjectVariant && "mr-1")} />
									{!isProjectVariant && "停止"}
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
	activeSessionId: string | null,
): PendingQuestionRef | null {
	for (let index = messageIds.length - 1; index >= 0; index -= 1) {
		const message = messagesMap[messageIds[index] ?? ""];
		if (!message) continue;
		if (activeSessionId && message.conversationId !== activeSessionId) continue;

		const question = [...(message.questions ?? [])]
			.reverse()
			.find(
				(item) =>
					item.status === "pending" || item.status === "submitting" || item.status === "error",
			);
		if (question) return { message, question };
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

function projectSkillToComposerOption(skill: ProjectSkill): ComposerSkillOption {
	return {
		code: skill.code,
		label: skill.name,
		description: skill.description || skill.category || "项目技能",
		keywords: [
			skill.name,
			skill.code,
			skill.description,
			skill.category,
			skill.source,
			skill.trust,
		].filter((item): item is string => Boolean(item)),
	};
}

function removeSkillDirectives(value: string, removedLabels: string[]): string {
	let nextValue = value;
	for (const label of removedLabels) {
		nextValue = nextValue.replace(new RegExp(`(^|\\s)/${escapeRegExp(label)}(?=\\s|$)`, "g"), "$1");
	}
	return nextValue.replace(/[ \t]{2,}/g, " ").trimStart();
}

function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
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

function AttachmentPreview({
	attachments,
	onRemove,
}: {
	attachments: Attachment[];
	onRemove: (id: string) => void;
}) {
	return (
		<div data-slot="attachment-preview" className="mb-3 flex flex-wrap gap-2">
			{attachments.map((att) => (
				<div
					key={att.id}
					className="flex items-center gap-2 rounded-lg bg-white/90 px-3 py-2 text-sm shadow-sm ring-1 ring-slate-200/70"
				>
					{att.type === "image" && att.url ? (
						<img src={att.url} alt={att.name} className="size-8 rounded object-cover" />
					) : (
						<Paperclip className="size-3.5 text-slate-400" />
					)}
					<span className="text-slate-600 truncate max-w-[120px]">{att.name}</span>
					<button
						type="button"
						onClick={() => onRemove(att.id)}
						className="text-slate-400 hover:text-slate-600 transition-colors"
					>
						<X className="size-3.5" />
					</button>
				</div>
			))}
		</div>
	);
}
