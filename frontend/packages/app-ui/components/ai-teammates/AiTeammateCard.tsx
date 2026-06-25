"use client";

import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { Eye, Heart } from "lucide-react";
import type { KeyboardEvent, MouseEvent } from "react";
import { APP_LOGO_SRC } from "../../assets";
import { notifyFeatureUnavailable } from "./feature-unavailable";
import type { AiTeammateItem } from "./mock-data";

interface AiTeammateCardProps {
	item: AiTeammateItem;
	onSelect?: () => void;
}

export function AiTeammateCard({ item, onSelect }: AiTeammateCardProps) {
	const Icon = item.icon;

	const handleCardClick = () => {
		onSelect?.();
	};

	const handleCardKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
		if (event.key === "Enter" || event.key === " ") {
			event.preventDefault();
			handleCardClick();
		}
	};

	const handleAddClick = (event: MouseEvent<HTMLButtonElement>) => {
		event.stopPropagation();
		notifyFeatureUnavailable();
	};

	return (
		// biome-ignore lint/a11y/useSemanticElements: 卡片内含独立「添加」按钮，外层使用 div 避免 button 嵌套。
		<div
			role="button"
			tabIndex={0}
			onClick={handleCardClick}
			onKeyDown={handleCardKeyDown}
			className={cn(
				"group relative flex cursor-pointer flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-4 text-left transition-all duration-300",
				"hover:-translate-y-1 hover:border-[var(--leros-primary)] hover:shadow-lg",
			)}
		>
			{/* 中文注释：悬停时在卡片右上角展示添加按钮，默认隐藏。 */}
			<Button
				type="button"
				size="sm"
				className="absolute right-3 top-3 z-10 h-7 rounded-full px-3 text-xs opacity-0 transition-opacity duration-300 group-hover:opacity-100"
				onClick={handleAddClick}
			>
				添加
			</Button>

			<div className="mb-3 flex items-start gap-3 pr-16">
				<div
					className={cn(
						"flex h-11 w-11 shrink-0 items-center justify-center rounded-xl",
						item.iconBg,
						item.iconColor,
					)}
				>
					<Icon className="size-5" aria-hidden="true" />
				</div>
				<div className="min-w-0 flex-1">
					<h3 className="truncate text-sm font-semibold text-[var(--leros-text-strong)]">
						{item.name}
					</h3>
					<p className="mt-2 line-clamp-2 min-h-[2.5rem] text-xs leading-relaxed text-[var(--leros-text-muted)]">
						{item.description}
					</p>
				</div>
			</div>

			<div className="mt-auto flex items-center justify-between border-t border-[var(--leros-control-border)] pt-3">
				{/* 中文注释：卡片底部固定展示 Lework 品牌标识。 */}
				<div className="flex min-w-0 items-center gap-1.5">
					<img src={APP_LOGO_SRC} alt="" className="size-4 shrink-0 rounded object-cover" />
					<span className="truncate text-[11px] text-[var(--leros-text-muted)]">Lework</span>
				</div>
				<div className="flex shrink-0 items-center gap-3 text-[11px] text-[var(--leros-text-subtle)]">
					<span className="inline-flex items-center gap-1">
						<Eye className="size-3" aria-hidden="true" />
						{item.views}
					</span>
					<span className="inline-flex items-center gap-1">
						<Heart className="size-3" aria-hidden="true" />
						{item.likes}
					</span>
				</div>
			</div>
		</div>
	);
}
