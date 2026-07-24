"use client";

import { type FeedbackType, feedbackApi, getClientVersionReport } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Textarea } from "@leros/ui/components/ui/textarea";
import { cn } from "@leros/ui/lib/utils";
import { Loader2, MessageSquareHeart } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { FeedbackImageUpload, type FeedbackImageUploadHandle } from "./FeedbackImageUpload";
import { FeedbackTypeSelector } from "./FeedbackTypeSelector";
import { getImageFilesFromClipboard } from "./feedbackImageUtils";

const MAX_CONTENT = 300;

type FeedbackDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function FeedbackDialog({ open, onOpenChange }: FeedbackDialogProps) {
	const [type, setType] = useState<FeedbackType | null>(null);
	const [content, setContent] = useState("");
	const [attachmentIds, setAttachmentIds] = useState<string[]>([]);
	const [submitting, setSubmitting] = useState(false);
	const [uploadKey, setUploadKey] = useState(0);
	const imageUploadRef = useRef<FeedbackImageUploadHandle>(null);

	useEffect(() => {
		if (!open) return;
		setType(null);
		setContent("");
		setAttachmentIds([]);
		setUploadKey((key) => key + 1);
	}, [open]);

	const handlePasteImages = (event: React.ClipboardEvent) => {
		const files = getImageFilesFromClipboard(event.clipboardData);
		if (!files.length) return;
		event.preventDefault();
		void imageUploadRef.current?.uploadFiles(files);
	};

	const handleSubmit = async () => {
		if (!type) {
			toast.error("请选择反馈类型");
			return;
		}

		const trimmed = content.trim();
		if (!trimmed) {
			toast.error("请填写反馈内容");
			return;
		}

		setSubmitting(true);
		try {
			const clientReport = getClientVersionReport();
			const response = await feedbackApi.submit({
				type,
				content: trimmed,
				attachment_ids: attachmentIds,
				client: {
					platform: clientReport.app,
					version: clientReport.version,
				},
			});

			if (response.data.code !== 0) {
				throw new Error(response.data.message || "提交失败");
			}

			toast.success("反馈提交成功，感谢您的反馈");
			onOpenChange(false);
		} catch (error) {
			const message = error instanceof Error ? error.message : "提交失败，请稍后重试";
			toast.error(message);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				if (submitting) return;
				onOpenChange(nextOpen);
			}}
		>
			<DialogContent
				className="flex max-h-[min(88vh,720px)] flex-col gap-0 overflow-hidden border-[var(--leros-control-border)] bg-white p-0 shadow-[var(--leros-shadow-menu)] sm:max-w-[520px]"
				showCloseButton
				onPaste={handlePasteImages}
			>
				<div className="border-b border-[var(--leros-control-border)] px-6 py-5">
					<DialogHeader className="gap-2 text-left">
						<div className="flex items-center gap-3">
							<div className="flex size-10 items-center justify-center rounded-xl bg-[var(--leros-primary-subtle)] text-[var(--leros-primary)]">
								<MessageSquareHeart className="size-5" />
							</div>
							<div>
								<DialogTitle className="text-lg">意见反馈</DialogTitle>
								<DialogDescription className="mt-1 text-[var(--leros-text-muted)]">
									告诉我们你遇到的问题或建议，帮助我们做得更好
								</DialogDescription>
							</div>
						</div>
					</DialogHeader>
				</div>

				<div className="flex-1 space-y-5 overflow-y-auto px-6 py-5">
					<FeedbackTypeSelector value={type} onChange={setType} />

					<div className="space-y-2">
						<p className="text-xs font-medium text-slate-500">
							反馈内容 <span className="text-red-500">*</span>
						</p>
						<div className="relative">
							<Textarea
								placeholder="请描述你遇到的问题、建议或体验感受…"
								value={content}
								maxLength={MAX_CONTENT}
								onChange={(event) => setContent(event.target.value)}
								onPaste={handlePasteImages}
								className={cn(
									"min-h-[128px] resize-none rounded-lg border-slate-200 bg-slate-50/70 px-3 py-2.5 text-sm text-slate-800",
									"placeholder:text-slate-400 focus-visible:border-blue-300 focus-visible:ring-blue-100",
								)}
							/>
							<span className="pointer-events-none absolute bottom-2.5 right-3 text-xs text-slate-400">
								{content.length}/{MAX_CONTENT}
							</span>
						</div>
					</div>

					<FeedbackImageUpload
						key={uploadKey}
						ref={imageUploadRef}
						attachmentIds={attachmentIds}
						onAttachmentIdsChange={setAttachmentIds}
					/>
				</div>

				<DialogFooter className="border-t border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-6 py-4">
					<Button
						type="button"
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={submitting}
						className="border-[var(--leros-control-border)]"
					>
						取消
					</Button>
					<Button
						onClick={() => void handleSubmit()}
						disabled={submitting || !type || !content.trim()}
						className="min-w-[88px]"
					>
						{submitting ? (
							<>
								<Loader2 className="size-4 animate-spin" />
								提交中
							</>
						) : (
							"提交反馈"
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
