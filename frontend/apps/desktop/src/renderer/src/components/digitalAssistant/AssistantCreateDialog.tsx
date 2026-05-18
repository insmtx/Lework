"use client";

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

export type AssistantCreateDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantCreateDialog({ open, onOpenChange }: AssistantCreateDialogProps) {
	const { createAssistant } = useDAStore((s) => s);
	const [name, setName] = useState("");
	const [code, setCode] = useState("");
	const [description, setDescription] = useState("");
	const [systemPrompt, setSystemPrompt] = useState("");

	const handleSubmit = async () => {
		if (!name.trim() || !code.trim()) return;
		await createAssistant({
			code: code.trim(),
			name: name.trim(),
			description: description.trim() || undefined,
			system_prompt: systemPrompt.trim() || undefined,
		});
		setName("");
		setCode("");
		setDescription("");
		setSystemPrompt("");
		onOpenChange(false);
	};

	const handleClose = () => {
		setName("");
		setCode("");
		setDescription("");
		setSystemPrompt("");
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>新建 AI 员工</DialogTitle>
					<DialogDescription>创建一个新的数字助手</DialogDescription>
				</DialogHeader>
				<div className="mt-4 space-y-3">
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">名称 *</span>
						<input
							type="text"
							value={name}
							onChange={(e) => setName(e.target.value)}
							placeholder="员工名称"
							autoFocus
							className="w-full rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 focus:border-blue-300 focus:outline-none transition-colors"
						/>
					</div>
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">编码 *</span>
						<input
							type="text"
							value={code}
							onChange={(e) => setCode(e.target.value)}
							placeholder="唯一编码（如 code-review-bot）"
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
					<Button variant="outline" onClick={handleClose}>
						取消
					</Button>
					<button
						type="button"
						onClick={handleSubmit}
						disabled={!name.trim() || !code.trim()}
						className="inline-flex items-center justify-center rounded-lg bg-primary text-primary-foreground h-8 px-2.5 text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 hover:bg-primary/80"
					>
						创建
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
