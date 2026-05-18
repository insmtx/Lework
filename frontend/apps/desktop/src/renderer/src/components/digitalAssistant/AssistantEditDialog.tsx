"use client";

import type { DigitalAssistantItem } from "@leros/store";
import { useDAStore } from "@leros/store";
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

export type AssistantEditDialogProps = {
	assistant: DigitalAssistantItem;
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantEditDialog({ assistant, open, onOpenChange }: AssistantEditDialogProps) {
	const { updateAssistant } = useDAStore((s) => s);
	const [name, setName] = useState(assistant.name);
	const [description, setDescription] = useState(assistant.description);
	const [systemPrompt, setSystemPrompt] = useState(assistant.systemPrompt);

	const handleSubmit = async () => {
		if (!name.trim()) return;
		await updateAssistant({
			id: assistant.id,
			name: name.trim(),
			description: description.trim(),
			system_prompt: systemPrompt.trim(),
		});
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>编辑 AI 员工</DialogTitle>
					<DialogDescription>修改员工信息</DialogDescription>
				</DialogHeader>
				<div className="mt-4 space-y-3">
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">名称 *</span>
						<input
							type="text"
							value={name}
							onChange={(e) => setName(e.target.value)}
							placeholder="员工名称"
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-blue-300 focus:outline-none transition-colors"
						/>
					</div>
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">描述</span>
						<input
							type="text"
							value={description}
							onChange={(e) => setDescription(e.target.value)}
							placeholder="简短描述"
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-blue-300 focus:outline-none transition-colors"
						/>
					</div>
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">系统提示词</span>
						<textarea
							value={systemPrompt}
							onChange={(e) => setSystemPrompt(e.target.value)}
							placeholder="系统提示词（可选）"
							rows={3}
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-blue-300 focus:outline-none transition-colors resize-none"
						/>
					</div>
				</div>
				<DialogFooter className="mt-4">
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						取消
					</Button>
					<button
						type="button"
						onClick={handleSubmit}
						disabled={!name.trim()}
						className="inline-flex items-center justify-center rounded-lg bg-primary text-primary-foreground h-8 px-2.5 text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 hover:bg-primary/80"
					>
						保存
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
