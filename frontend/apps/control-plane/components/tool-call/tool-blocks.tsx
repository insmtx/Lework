"use client";

import { clipText, type DiffLine, type ToolBlock } from "@lework/tool-view";
import { cn } from "@lework/ui/lib/utils";

/** 单段 diff 最多展示 300 行；超出的部分明确标注截断而不是静默丢弃。 */
const DIFF_LINE_LIMIT = 300;

function BlockLabel({ label }: { label?: string }) {
	if (!label) return null;
	return (
		<div className="mb-1 font-mono text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
			{label}
		</div>
	);
}

function diffPrefix(kind: DiffLine["kind"]): string {
	if (kind === "add") return "+";
	if (kind === "del") return "-";
	if (kind === "context") return " ";
	return "";
}

function TerminalBlock({ block }: { block: Extract<ToolBlock, { kind: "terminal" }> }) {
	const meta = [block.exit !== undefined ? `exit ${block.exit}` : "", block.duration ?? ""]
		.filter((part) => part !== "")
		.join(" · ");
	const failed = block.exit !== undefined && block.exit !== 0;

	return (
		<div>
			<BlockLabel label={block.label} />
			<div className="rounded-md bg-muted/60 p-2 font-mono text-[11px] leading-relaxed">
				<div className="font-semibold break-words">$ {block.command}</div>
				{block.workdir ? (
					<div className="text-[10px] text-muted-foreground">{block.workdir}</div>
				) : null}
				{block.output ? (
					<pre className="mt-1 whitespace-pre-wrap break-words font-mono">
						{clipText(block.output)}
					</pre>
				) : null}
				{meta ? (
					<div
						className={cn(
							"mt-1 text-[10px]",
							failed ? "text-destructive" : "text-muted-foreground",
						)}
					>
						{meta}
					</div>
				) : null}
			</div>
		</div>
	);
}

function CodeBlock({ block }: { block: Extract<ToolBlock, { kind: "code" }> }) {
	return (
		<div>
			<BlockLabel label={block.label} />
			<pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
				{clipText(block.text)}
			</pre>
		</div>
	);
}

function DiffBlock({ block }: { block: Extract<ToolBlock, { kind: "diff" }> }) {
	const lines = block.lines.slice(0, DIFF_LINE_LIMIT);
	const truncated = block.lines.length > DIFF_LINE_LIMIT;

	return (
		<div>
			<BlockLabel label={block.label} />
			<div className="max-h-72 overflow-auto rounded-md bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
				{lines.map((line, index) => (
					<div
						key={`${index}-${line.kind}`}
						className={cn(
							"whitespace-pre-wrap break-words",
							line.kind === "add" && "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
							line.kind === "del" && "bg-destructive/10 text-destructive",
							line.kind === "meta" && "text-muted-foreground",
						)}
					>
						{diffPrefix(line.kind)}
						{line.text === "" ? " " : line.text}
					</div>
				))}
				{truncated ? <div className="text-muted-foreground">… truncated</div> : null}
			</div>
		</div>
	);
}

function ListBlock({ block }: { block: Extract<ToolBlock, { kind: "list" }> }) {
	return (
		<div>
			<BlockLabel label={block.label} />
			{block.items.length === 0 ? (
				<div className="rounded-md bg-muted/50 p-2 text-[11px] text-muted-foreground">
					No matches
				</div>
			) : (
				<div className="space-y-0.5 rounded-md bg-muted/50 p-2 font-mono text-[11px] leading-relaxed">
					{block.items.map((item, index) => (
						<div key={`${index}-${item}`} className="truncate" title={item}>
							• {item}
						</div>
					))}
				</div>
			)}
		</div>
	);
}

function ChecklistBlock({ block }: { block: Extract<ToolBlock, { kind: "checklist" }> }) {
	return (
		<div>
			<BlockLabel label={block.label} />
			<div className="space-y-0.5 text-[11px] leading-relaxed">
				{block.items.map((item, index) => (
					<div
						key={`${index}-${item.text}`}
						className={cn("break-words", item.done && "text-muted-foreground line-through")}
					>
						{item.done ? "☑" : "☐"} {item.text}
					</div>
				))}
			</div>
		</div>
	);
}

function FieldsBlock({ block }: { block: Extract<ToolBlock, { kind: "fields" }> }) {
	return (
		<div>
			<BlockLabel label={block.label} />
			<dl className="grid grid-cols-[minmax(5rem,auto)_minmax(0,1fr)] gap-x-3 gap-y-1">
				{block.fields.map((field) => (
					<div key={field.label} className="contents">
						<dt className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground">
							{field.label}
						</dt>
						<dd className="break-words font-mono text-[11px]">{clipText(field.value, 1200)}</dd>
					</div>
				))}
			</dl>
		</div>
	);
}

function TextBlock({ block }: { block: Extract<ToolBlock, { kind: "text" }> }) {
	return (
		<div>
			<BlockLabel label={block.label} />
			<p
				className={cn(
					"whitespace-pre-wrap break-words text-[11px] leading-relaxed",
					block.tone === "muted" && "text-muted-foreground",
					block.tone === "error" && "text-destructive",
				)}
			>
				{clipText(block.text)}
			</p>
		</div>
	);
}

/** ToolBlocks 只认识抽象块，不关心工具名，新增工具只要产出块就能复用排版。 */
export function ToolBlocks({ blocks }: { blocks: ToolBlock[] }) {
	return (
		<div className="space-y-2">
			{blocks.map((block, index) => {
				switch (block.kind) {
					case "terminal":
						return <TerminalBlock key={index} block={block} />;
					case "code":
						return <CodeBlock key={index} block={block} />;
					case "diff":
						return <DiffBlock key={index} block={block} />;
					case "list":
						return <ListBlock key={index} block={block} />;
					case "checklist":
						return <ChecklistBlock key={index} block={block} />;
					case "fields":
						return <FieldsBlock key={index} block={block} />;
					default:
						return <TextBlock key={index} block={block} />;
				}
			})}
		</div>
	);
}
