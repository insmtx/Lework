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
			<DialogContent className="max-w-[420px] p-0 sm:rounded-2xl">
				<DialogHeader className="border-b border-slate-200 px-6 py-5">
					<DialogTitle>删除自动化</DialogTitle>
					<DialogDescription>此操作不可撤销</DialogDescription>
				</DialogHeader>
				<div className="px-6 py-5 text-sm leading-6 text-slate-600">
					<p>
						确定删除自动化“
						<span className="font-semibold text-slate-800">{target?.name ?? ""}</span>
						”吗？
					</p>
					<p className="mt-2 text-xs text-slate-400">
						删除后不再产生新的周期执行，已有执行记录和项目结果将保留。
					</p>
				</div>
				<DialogFooter className="border-t border-slate-200 px-6 py-4">
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
