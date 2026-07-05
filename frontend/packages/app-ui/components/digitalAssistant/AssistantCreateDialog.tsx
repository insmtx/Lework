"use client";

import {
	getFileDownloadUrl,
	getFilePublicUrlFromStorageUri,
	projectFileApi,
	useDAStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { ImagePlus } from "lucide-react";
import { type ChangeEvent, useState } from "react";
import { toast } from "sonner";
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
	const [uploadingAvatar, setUploadingAvatar] = useState(false);
	const [previewAvatar, setPreviewAvatar] = useState<string | undefined>();

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
		setPreviewAvatar(undefined);
		onOpenChange(false);
	};

	const handleClose = () => {
		setName("");
		setAvatar("");
		setDescription("");
		setSystemPrompt("");
		setPreviewAvatar(undefined);
		onOpenChange(false);
	};

	const handleDialogOpenChange = (nextOpen: boolean) => {
		if (!nextOpen) {
			handleClose();
			return;
		}
		onOpenChange(nextOpen);
	};

	const handleAvatarChange = async (event: ChangeEvent<HTMLInputElement>) => {
		const file = event.target.files?.[0];
		event.target.value = "";
		if (!file) return;
		if (!isImageFile(file)) {
			toast.error("请选择图片文件");
			return;
		}

		const previewURL = URL.createObjectURL(file);
		setPreviewAvatar(previewURL);
		setUploadingAvatar(true);
		try {
			const response = await projectFileApi.uploadLoose({ file, purpose: "avatar" });
			const uploaded = response.data;
			if (!uploaded?.public_id) throw new Error("头像上传失败");
			setAvatar(
				getFilePublicUrlFromStorageUri(uploaded.storage_uri) ??
					getFileDownloadUrl(uploaded.public_id),
			);
			setPreviewAvatar(undefined);
			toast.success("头像已上传");
		} catch (err) {
			const message = err instanceof Error ? err.message : "头像上传失败";
			toast.error(message);
			setPreviewAvatar(undefined);
		} finally {
			URL.revokeObjectURL(previewURL);
			setUploadingAvatar(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={handleDialogOpenChange}>
			<DialogContent className="sm:max-w-md" showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>新建 AI 队友</DialogTitle>
					<DialogDescription>创建一个新的数字队友</DialogDescription>
				</DialogHeader>
				<div className="mt-4 space-y-3">
					<div className="space-y-1.5">
						<span className="text-xs font-medium text-slate-700">头像</span>
						<div className="flex items-center gap-3">
							<AssistantAvatar name={name || "AI"} src={previewAvatar || avatar} />
							<label
								className={`inline-flex h-9 cursor-pointer items-center justify-center rounded-md border border-slate-200 bg-white px-3 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50 ${
									uploadingAvatar ? "cursor-not-allowed opacity-60" : ""
								}`}
							>
								<ImagePlus className="mr-2 size-4" />
								{uploadingAvatar ? "上传中" : "上传头像"}
								<input
									type="file"
									accept="image/*"
									className="sr-only"
									onChange={handleAvatarChange}
									disabled={uploadingAvatar}
								/>
							</label>
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
						disabled={!name.trim() || uploadingAvatar}
						className="inline-flex items-center justify-center rounded-lg bg-primary text-primary-foreground h-8 px-2.5 text-sm font-medium transition-all disabled:pointer-events-none disabled:opacity-50 hover:bg-primary/80"
					>
						创建
					</button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function isImageFile(file: File): boolean {
	if (file.type.startsWith("image/")) return true;
	return /\.(avif|bmp|gif|jpe?g|png|svg|webp)$/i.test(file.name);
}
