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
import { AssistantAvatar } from "./AssistantAvatar";

export type AssistantCreateDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
};

export function AssistantCreateDialog({ open, onOpenChange }: AssistantCreateDialogProps) {
	const { createAssistant } = useDAStore((s) => s);
	const [name, setName] = useState("");
	const [avatar, setAvatar] = useState("");
	const [description, setDescription] = useState("");
	const [systemPrompt, setSystemPrompt] = useState("");

	const handleSubmit = async () => {
		if (!name.trim()) return;
		await createAssistant({
			name: name.trim(),
			avatar: avatar.trim() || undefined,
			description: description.trim() || undefined,
			system_prompt: systemPrompt.trim() || undefined,
		});
		setName("");
		setAvatar("");
		setDescription("");
		setSystemPrompt("");
		onOpenChange(false);
	};

	const handleClose = () => {
		setName("");
		setAvatar("");
		setDescription("");
		setSystemPrompt("");
		onOpenChange(false);
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>新建 AI 队友</DialogTitle>
					<DialogDescription>创建一个新的数字队友</DialogDescription>
				</DialogHeader>
				<div className="mt-4 space-y-3">
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">头像</span>
						<div className="flex items-center gap-3">
							<AssistantAvatar name={name || "AI"} src={avatar} />
							<input
								type="text"
								value={avatar}
								onChange={(e) => setAvatar(e.target.value)}
								placeholder="头像 URL"
								className="min-w-0 flex-1 rounded-md border border-slate-200 bg-white px-3 py-2 text-sm text-slate-800 placeholder:text-slate-400 transition-colors focus:border-blue-300 focus:outline-none"
							/>
						</div>
					</div>
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">名称 *</span>
						<input
							type="text"
							value={name}
							onChange={(e) => setName(e.target.value)}
							placeholder="队友名称"
							autoFocus
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
						<span className="text-xs font-medium text-slate-700">简介</span>
						<textarea
							value={systemPrompt}
							onChange={(e) => setSystemPrompt(e.target.value)}
							placeholder="能力边界、执行方式和输出要求"
							rows={5}
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
						disabled={!name.trim()}
						className="inline-flex items-center justify-center rounded-lg bg-primary text-primary-foreground h-8 px-2.5 text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 hover:bg-primary/80"
					>
						创建
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
