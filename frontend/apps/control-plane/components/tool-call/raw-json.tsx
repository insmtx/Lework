"use client";

import { clipText } from "@lework/tool-view";

const RAW_LIMIT = 6000;

/** 原始 state 默认收起，作为排查入口而不是主视图。 */
export function RawJson({ text }: { text: string }) {
	if (text === "") return null;

	return (
		<details className="group">
			<summary className="cursor-pointer list-none font-mono text-[10px] uppercase tracking-wide text-muted-foreground hover:text-foreground">
				<span className="group-open:hidden">▸ </span>
				<span className="hidden group-open:inline">▾ </span>
				Raw data
			</summary>
			<pre className="mt-1 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
				{clipText(text, RAW_LIMIT)}
			</pre>
		</details>
	);
}
