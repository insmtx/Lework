"use client";

import type { Attachment } from "@leros/store/types/chat";
import { Folder, Loader2, X } from "lucide-react";
import { ProjectFileTypeIcon } from "../layout/project-file-type-icon";

export function AttachmentPreview({
	attachments,
	onPreview,
	onRemove,
}: {
	attachments: Attachment[];
	onPreview: (attachment: Attachment) => void;
	onRemove: (id: string) => void;
}) {
	return (
		<div data-slot="attachment-preview" className="mb-3 flex flex-wrap gap-2">
			{attachments.map((attachment) => (
				<div
					key={attachment.id}
					className="group flex items-center gap-1 rounded-lg bg-white/90 p-1 text-sm shadow-sm ring-1 ring-slate-200/70 transition-colors hover:bg-blue-50/60 hover:ring-blue-200"
				>
					{attachment.type === "folder" ? (
						<div className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1 text-left">
							<div className="relative flex size-8 shrink-0 items-center justify-center rounded bg-slate-50 text-slate-500">
								<Folder className="size-5" aria-hidden="true" />
								{attachment.uploadStatus === "uploading" && (
									<span className="absolute inset-0 flex items-center justify-center rounded bg-white/75">
										<Loader2
											className="size-4 animate-spin text-blue-500"
											aria-label="文件夹上传中"
										/>
									</span>
								)}
							</div>
							<span className="max-w-[160px] truncate text-slate-600">{attachment.name}</span>
						</div>
					) : (
						<button
							type="button"
							onClick={() => {
								if (attachment.uploadStatus === "uploading") return;
								onPreview(attachment);
							}}
							disabled={attachment.uploadStatus === "uploading"}
							className="flex min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-left disabled:cursor-default"
							title={attachment.uploadStatus === "uploading" ? "文件上传中" : "点击预览"}
						>
							{attachment.type === "image" && attachment.url ? (
								<div className="relative size-8 shrink-0">
									<img
										src={attachment.url}
										alt={attachment.name}
										className="size-8 rounded object-cover"
									/>
									{attachment.uploadStatus === "uploading" && (
										<span className="absolute inset-0 flex items-center justify-center rounded bg-white/75">
											<Loader2
												className="size-4 animate-spin text-blue-500"
												aria-label="文件上传中"
											/>
										</span>
									)}
								</div>
							) : (
								<div className="relative flex size-8 shrink-0 items-center justify-center rounded bg-slate-50">
									<ProjectFileTypeIcon
										fileName={attachment.name}
										className="size-6 object-contain"
									/>
									{attachment.uploadStatus === "uploading" && (
										<span className="absolute inset-0 flex items-center justify-center rounded bg-white/75">
											<Loader2
												className="size-4 animate-spin text-blue-500"
												aria-label="文件上传中"
											/>
										</span>
									)}
								</div>
							)}
							<span className="max-w-[160px] truncate text-slate-600">{attachment.name}</span>
						</button>
					)}
					<button
						type="button"
						onClick={() => onRemove(attachment.id)}
						className="text-slate-400 transition-colors hover:text-slate-600"
						aria-label={`移除 ${attachment.name}`}
					>
						<X className="size-3.5" />
					</button>
				</div>
			))}
		</div>
	);
}
