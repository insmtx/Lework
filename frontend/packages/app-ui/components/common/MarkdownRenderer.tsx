import { type ReactNode, useMemo } from "react";
import Markdown from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { PlanBlock } from "./PlanBlock";

type MarkdownRendererProps = {
	content: string;
	className?: string;
	onPlanOpen?: (fileId: string) => void;
	onPlanCopy?: (fileId: string) => Promise<void>;
};

// Parses a single :::plan{...} directive block.
// Format: :::plan{"file_id":"...","summary_lines":N,"total_lines":N}\n<summary>\n:::
const planDirectivePattern = /^:::plan(\{[^}]*\})\s*\n([\s\S]*?)\n:::/gm;

interface PlanDirectiveMeta {
	file_id: string;
	summary_lines: number;
	total_lines: number;
}

function parsePlanMeta(jsonStr: string): PlanDirectiveMeta | null {
	try {
		const parsed = JSON.parse(jsonStr);
		if (
			typeof parsed === "object" &&
			parsed !== null &&
			typeof parsed.file_id === "string" &&
			typeof parsed.summary_lines === "number" &&
			typeof parsed.total_lines === "number"
		) {
			return parsed as PlanDirectiveMeta;
		}
		return null;
	} catch {
		return null;
	}
}

function processContent(content: string): { text: string; planBlocks: PlanDirectiveMeta[] }[] {
	const segments: { text: string; planBlocks: PlanDirectiveMeta[] }[] = [];
	let lastIndex = 0;
	let match: RegExpExecArray | null;

	// Reset lastIndex
	planDirectivePattern.lastIndex = 0;

	match = planDirectivePattern.exec(content);
	while (match !== null) {
		// Push preceding text as a segment
		if (match.index > lastIndex) {
			segments.push({
				text: content.slice(lastIndex, match.index),
				planBlocks: [],
			});
		}

		const meta = parsePlanMeta(match[1] ?? "");
		const summaryContent = match[2] ?? "";
		if (meta) {
			segments.push({
				text: summaryContent,
				planBlocks: [meta],
			});
		} else {
			// Invalid directive — render as plain text.
			segments.push({
				text: match[0],
				planBlocks: [],
			});
		}

		lastIndex = match.index + match[0].length;
		match = planDirectivePattern.exec(content);
	}

	// Push remaining content.
	if (lastIndex < content.length) {
		segments.push({
			text: content.slice(lastIndex),
			planBlocks: [],
		});
	}

	return segments;
}

export function MarkdownRenderer({
	content,
	className,
	onPlanOpen,
	onPlanCopy,
}: MarkdownRendererProps) {
	const segments = useMemo(() => processContent(content), [content]);

	if (segments.length === 0) {
		return null;
	}

	// If there are no plan blocks, use the fast path with plain Markdown.
	const hasAnyPlanBlock = segments.some((s) => s.planBlocks.length > 0);
	if (!hasAnyPlanBlock) {
		return (
			<div className={className}>
				<Markdown remarkPlugins={[remarkGfm, remarkMath]} rehypePlugins={[rehypeKatex]}>
					{content}
				</Markdown>
			</div>
		);
	}

	// Render segments with plan blocks interleaved.
	const elements: ReactNode[] = [];
	segments.forEach((seg, i) => {
		if (seg.planBlocks.length > 0) {
			seg.planBlocks.forEach((meta, j) => {
				elements.push(
					<PlanBlock
						key={`plan-${i}-${j}`}
						fileId={meta.file_id}
						onOpen={onPlanOpen}
						onCopy={onPlanCopy}
					>
						<Markdown remarkPlugins={[remarkGfm, remarkMath]} rehypePlugins={[rehypeKatex]}>
							{seg.text}
						</Markdown>
					</PlanBlock>,
				);
			});
		} else if (seg.text.trim()) {
			elements.push(
				<div key={`text-${i}`}>
					<Markdown remarkPlugins={[remarkGfm, remarkMath]} rehypePlugins={[rehypeKatex]}>
						{seg.text}
					</Markdown>
				</div>,
			);
		}
	});

	return <div className={className}>{elements}</div>;
}
