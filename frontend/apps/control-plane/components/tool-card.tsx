"use client";

import type { ContentPart } from "../lib/types";

interface ToolState {
	status?: string;
	title?: string;
	input?: unknown;
	output?: unknown;
	content?: unknown;
	error?: string;
}

function pretty(value: unknown): string {
	if (value === undefined || value === null) return "";
	if (typeof value === "string") return value;
	try {
		return JSON.stringify(value, null, 2);
	} catch {
		return String(value);
	}
}

function clip(value: string, limit = 6000): string {
	return value.length > limit ? `${value.slice(0, limit)}\n… truncated` : value;
}

export function ToolCard({ part }: { part: ContentPart }) {
	const state = (part.state ?? {}) as ToolState;
	const status = state.status ?? "running";
	const input = pretty(state.input);
	const output = pretty(state.output ?? state.content);
	const error = pretty(state.error ?? state.title);

	return (
		<details className="group rounded-lg border bg-background/80 text-xs shadow-sm">
			<summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2">
				<span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
					tool
				</span>
				<span className="font-mono font-medium">{part.tool || "unknown"}</span>
				<span className="ml-auto text-[10px] text-muted-foreground">{status}</span>
			</summary>
			<div className="space-y-2 border-t px-3 py-2">
				{input ? (
					<div>
						<div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
							Input
						</div>
						<pre className="max-h-48 overflow-auto rounded bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
							{clip(input)}
						</pre>
					</div>
				) : null}
				{output ? (
					<div>
						<div className="mb-1 text-[10px] uppercase tracking-wide text-muted-foreground">
							Output
						</div>
						<pre className="max-h-64 overflow-auto rounded bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
							{clip(output)}
						</pre>
					</div>
				) : null}
				{error ? (
					<div>
						<div className="mb-1 text-[10px] uppercase tracking-wide text-destructive">Error</div>
						<pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-destructive/5 p-2 font-mono text-[11px] leading-relaxed text-destructive">
							{clip(error)}
						</pre>
					</div>
				) : null}
			</div>
		</details>
	);
}

export function ToolCardList({ parts }: { parts: ContentPart[] }) {
	const tools = parts.filter((part) => part.type === "tool");
	if (tools.length === 0) return null;
	return (
		<div className="mt-2 space-y-1.5">
			{tools.map((part, index) => (
				<ToolCard key={part.callID || `${part.tool || "tool"}-${index}`} part={part} />
			))}
		</div>
	);
}
