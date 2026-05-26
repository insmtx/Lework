"use client";

import type { ProjectTask } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";

export type TaskDeleteDialogProps = {
	task: ProjectTask;
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function TaskDeleteDialog({ task, open, onOpenChange }: TaskDeleteDialogProps) {
	const { deleteTask } = useLayoutStore((s) => s);

	const handleDelete = async () => {
		await deleteTask(task.id);
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>删除任务</DialogTitle>
					<DialogDescription>
						确定要删除 <strong>{task.title}</strong> 吗？此操作不可撤销。
					</DialogDescription>
				</DialogHeader>
				<DialogFooter className="mt-4">
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<button
						type="button"
						onClick={handleDelete}
						className="inline-flex items-center justify-center rounded-lg bg-destructive/10 text-destructive hover:bg-destructive/20 h-8 px-2.5 text-sm font-medium transition-all"
					>
						删除
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
