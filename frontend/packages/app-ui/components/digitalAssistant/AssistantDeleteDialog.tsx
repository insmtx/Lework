"use client";

import type { DigitalAssistantItem } from "@lework/store";
import { useDAStore } from "@lework/store";
import { Button } from "@lework/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@lework/ui/components/ui/dialog";
import { useState } from "react";
import { toast } from "sonner";

export type AssistantDeleteDialogProps = {
	assistant: DigitalAssistantItem;
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantDeleteDialog({
	assistant,
	open,
	onOpenChange,
}: AssistantDeleteDialogProps) {
	const { deleteAssistant } = useDAStore((s) => s);
	const [submitting, setSubmitting] = useState(false);

	const handleDelete = async () => {
		if (submitting) return;
		setSubmitting(true);
		const deleted = await deleteAssistant(assistant.id);
		setSubmitting(false);
		if (!deleted) {
			toast.error("AI 队友删除失败，请稍后重试");
			return;
		}
		onOpenChange(false);
	};

	return (
		<Dialog
			open={open}
			onOpenChange={(nextOpen) => {
				if (!nextOpen && !submitting) onOpenChange(false);
			}}
		>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>删除 AI 队友</DialogTitle>
					<DialogDescription>
						确定要删除 <strong>{assistant.name}</strong> 吗？此操作不可撤销。
					</DialogDescription>
				</DialogHeader>
				<DialogFooter className="mt-4">
					<Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
						取消
					</Button>
					<button
						type="button"
						onClick={handleDelete}
						disabled={submitting}
						className="inline-flex h-8 items-center justify-center rounded-lg bg-destructive/10 px-2.5 text-sm font-medium text-destructive transition-all hover:bg-destructive/20 disabled:pointer-events-none disabled:opacity-50"
					>
						{submitting ? "删除中…" : "删除"}
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
