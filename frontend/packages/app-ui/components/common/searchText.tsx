import type { ReactNode } from "react";

/** 中文注释：弹窗列表关键词高亮，与全局任务搜索、输入框指令选择器保持一致。 */
export function renderHighlightedText(text: string, keyword: string): ReactNode {
	const normalizedKeyword = keyword.trim();
	if (!normalizedKeyword) return text;

	const lowerText = text.toLocaleLowerCase();
	const lowerKeyword = normalizedKeyword.toLocaleLowerCase();
	const segments: ReactNode[] = [];
	let searchStart = 0;
	let matchIndex = lowerText.indexOf(lowerKeyword, searchStart);

	while (matchIndex !== -1) {
		if (matchIndex > searchStart) {
			segments.push(text.slice(searchStart, matchIndex));
		}
		const matchEnd = matchIndex + normalizedKeyword.length;
		segments.push(
			<mark
				key={`${matchIndex}-${matchEnd}`}
				className="rounded-sm bg-[var(--leros-primary)]/12 px-0.5 text-[var(--leros-primary)] [box-decoration-break:clone]"
			>
				{text.slice(matchIndex, matchEnd)}
			</mark>,
		);
		searchStart = matchEnd;
		matchIndex = lowerText.indexOf(lowerKeyword, searchStart);
	}

	if (searchStart < text.length) {
		segments.push(text.slice(searchStart));
	}

	return segments;
}
