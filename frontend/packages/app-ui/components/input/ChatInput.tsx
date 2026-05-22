"use client";

import { useChatStore } from "@leros/store";
import type { Attachment } from "@leros/store/types/chat";
import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import {
	AtSign,
	ChevronDown,
	CircleStop,
	ImageIcon,
	Paperclip,
	SendHorizonal,
	X,
} from "lucide-react";
import { useCallback, useRef, useState } from "react";

export function ChatInput({ variant = "default" }: { variant?: "default" | "project" }) {
	const {
		inputText,
		inputAttachments,
		isGenerating,
		selectedModel,
		modelOptions,
		setInputText,
		sendMessage,
		cancelGeneration,
		addAttachment,
		removeAttachment,
		setInputFocused,
		setSelectedModel,
	} = useChatStore((s) => s);

	const textareaRef = useRef<HTMLTextAreaElement>(null);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const [showModelDropdown, setShowModelDropdown] = useState(false);

	const currentModel = modelOptions.find((m) => m.id === selectedModel);
	const isProjectVariant = variant === "project";

	const adjustHeight = useCallback(() => {
		const textarea = textareaRef.current;
		if (!textarea) return;
		textarea.style.height = "auto";
		const maxHeight = 200;
		textarea.style.height = `${Math.min(textarea.scrollHeight, maxHeight)}px`;
	}, []);

	const submitMessage = useCallback(() => {
		if (inputText.trim() || inputAttachments.length > 0) {
			sendMessage(inputText, inputAttachments);
			if (textareaRef.current) {
				textareaRef.current.style.height = "auto";
			}
		}
	}, [inputText, inputAttachments, sendMessage]);

	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent<HTMLTextAreaElement>) => {
			const submitByEnter = !isProjectVariant && e.key === "Enter" && !e.shiftKey;
			const submitByShortcut = isProjectVariant && e.key === "Enter" && (e.metaKey || e.ctrlKey);

			if (submitByEnter || submitByShortcut) {
				e.preventDefault();
				submitMessage();
			}
		},
		[isProjectVariant, submitMessage],
	);

	const handleTextareaChange = useCallback(
		(e: React.ChangeEvent<HTMLTextAreaElement>) => {
			setInputText(e.target.value);
			adjustHeight();
		},
		[setInputText, adjustHeight],
	);

	const handlePaste = useCallback(
		(e: React.ClipboardEvent) => {
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
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const files = Array.from(e.target.files ?? []);
			for (const file of files) {
				addAttachment(file);
			}
			e.target.value = "";
		},
		[addAttachment],
	);

	const handleSend = useCallback(() => {
		submitMessage();
	}, [submitMessage]);

	return (
		<div
			data-slot="chat-input"
			className={cn(
				"bg-transparent px-5 pb-5 sm:px-6 lg:px-8",
				isProjectVariant && "bg-white px-8 pb-8 sm:px-8 lg:px-8",
			)}
		>
			<div className={cn("mx-auto w-full max-w-[1040px]", isProjectVariant && "max-w-[780px]")}>
				{inputAttachments.length > 0 && (
					<AttachmentPreview attachments={inputAttachments} onRemove={removeAttachment} />
				)}
				<div
					className={cn(
						"relative rounded-2xl bg-white/95 shadow-sm ring-1 ring-slate-200/70 transition-all focus-within:shadow-md focus-within:ring-blue-300/70",
						isProjectVariant && "rounded-2xl bg-white px-6 py-5 ring-slate-200",
					)}
				>
					<textarea
						ref={textareaRef}
						value={inputText}
						onChange={handleTextareaChange}
						onKeyDown={handleKeyDown}
						onPaste={handlePaste}
						onFocus={() => setInputFocused(true)}
						onBlur={() => setInputFocused(false)}
						placeholder={
							isProjectVariant
								? "让 AI 编码、分析或规划..."
								: "请描述您的问题，支持 Ctrl+V 粘贴图片。输入 @ 提及成员，/ 使用命令，# 引用工作项。"
						}
						className={cn(
							"min-h-[116px] max-h-[220px] w-full resize-none rounded-2xl bg-transparent px-5 py-4 text-sm text-slate-700 focus:outline-none placeholder:text-slate-400",
							isProjectVariant &&
								"min-h-[92px] rounded-none px-0 py-0 text-base placeholder:text-slate-500",
						)}
						rows={1}
					/>
					<input
						ref={fileInputRef}
						type="file"
						className="hidden"
						accept="image/*,.pdf,.txt,.md,.json,.csv"
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
							<Button
								variant="ghost"
								size="icon-sm"
								className="text-slate-400 hover:text-slate-600"
								onClick={() => fileInputRef.current?.click()}
							>
								<Paperclip className="size-4" />
							</Button>
							{isProjectVariant ? (
								<Button
									variant="ghost"
									size="icon-sm"
									className="text-slate-500 hover:text-slate-700"
									onClick={() => fileInputRef.current?.click()}
								>
									<ImageIcon className="size-4" />
								</Button>
							) : (
								<>
									<Button
										variant="ghost"
										size="icon-sm"
										className="text-slate-400 hover:text-slate-600"
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
										isProjectVariant && "size-11 rounded-2xl",
									)}
									onClick={cancelGeneration}
								>
									<CircleStop className={cn("size-4", !isProjectVariant && "mr-1")} />
									{!isProjectVariant && "停止"}
								</Button>
							) : (
								<Button
									size={isProjectVariant ? "icon" : "sm"}
									className={cn(
										"h-9 min-w-20 bg-blue-600 text-white shadow-sm hover:bg-blue-700 disabled:bg-slate-200 disabled:text-slate-400",
										isProjectVariant && "size-11 min-w-0 rounded-2xl",
									)}
									onClick={handleSend}
									disabled={!inputText.trim() && inputAttachments.length === 0}
								>
									<SendHorizonal className={cn("size-4", !isProjectVariant && "mr-1")} />
									{!isProjectVariant && "发送"}
								</Button>
							)}
						</div>
					</div>
				</div>
				{isProjectVariant && (
					<div className="mt-3 text-center text-xs text-slate-500">
						按 <kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">⌘</kbd> +{" "}
						<kbd className="rounded border border-slate-300 bg-slate-100 px-1.5 py-0.5">Enter</kbd>{" "}
						发送
					</div>
				)}
			</div>
		</div>
	);
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
