"use client";

import { Button } from "@lework/ui/components/ui/button";
import { ChevronLeft, Download, FileText } from "lucide-react";
import type { FileReadResult } from "../types";
import { formatBytes } from "./file-paths";

export function FilePreview({ file, onBack }: { file: FileReadResult; onBack?: () => void }) {
	return (
		<div className="flex h-full flex-col">
			<div className="flex items-center gap-2 border-b px-3 py-2">
				{onBack ? (
					<Button variant="ghost" size="icon-xs" onClick={onBack} aria-label="Back to files">
						<ChevronLeft className="size-4" />
					</Button>
				) : null}
				<code className="min-w-0 flex-1 truncate text-xs">{file.path}</code>
				<span className="text-[10px] text-muted-foreground">{formatBytes(file.size)}</span>
				{file.encoding === "base64" && !file.truncated ? (
					<a
						className="inline-flex h-7 items-center gap-1 rounded-md border px-2 text-xs hover:bg-muted"
						href={`data:application/octet-stream;base64,${file.content}`}
						download={file.path.split("/").pop() || "download"}
					>
						<Download className="size-3" /> Download
					</a>
				) : null}
			</div>
			{file.truncated ? (
				<div className="border-b bg-amber-50 px-3 py-1.5 text-[11px] text-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
					Showing the first 512 KiB of this file.
				</div>
			) : null}
			{file.encoding === "utf-8" ? (
				<pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words bg-background p-4 font-mono text-xs leading-relaxed">
					{file.content}
				</pre>
			) : (
				<div className="m-auto p-6 text-center text-sm text-muted-foreground">
					<FileText className="mx-auto mb-2 size-8 opacity-50" />
					Binary file ({formatBytes(file.size)})
				</div>
			)}
		</div>
	);
}
