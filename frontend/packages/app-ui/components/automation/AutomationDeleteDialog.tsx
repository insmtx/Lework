"use client";

import type { AutomationItem } from "@leros/store";
import { useAutomationStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { useState } from "react";
import { toast } from "sonner";

export type AutomationDeleteDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	target: AutomationItem | null;
};

export function AutomationDeleteDialog({
	open,
	onOpenChange,
	target,
}: AutomationDeleteDialogProps) {
	const { deleteAutomation } = useAutomationStore((s) => s);
	const [submitting, setSubmitting] = useState(false);

	const handleDelete = async () => {
		if (!target || submitting) return;
		setSubmitting(true);
		try {
			const ok = await deleteAutomation(target.publicId);
			if (!ok) {
				toast.error("删除失败，请稍后重试");
				return;
			}
			toast.success("自动化已删除");
			onOpenChange(false);
		} finally {
			setSubmitting(false);
		}
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
					<DialogTitle>删除自动化</DialogTitle>
					<DialogDescription>
						确定要删除 <strong>{target?.name ?? ""}</strong>{" "}
						吗？此操作不可撤销。删除后不再产生新的周期执行，已有执行记录和项目结果将保留。
					</DialogDescription>
				</DialogHeader>
				<DialogFooter className="mt-4">
					<Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
						取消
					</Button>
					<Button variant="destructive" onClick={handleDelete} disabled={submitting}>
						{submitting ? "删除中…" : "删除"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
