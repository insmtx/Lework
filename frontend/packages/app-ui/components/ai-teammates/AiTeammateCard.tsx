import { cn } from "@leros/ui/lib/utils";
import { Eye, Heart, type LucideIcon } from "lucide-react";
import { APP_LOGO_SRC } from "../../assets";

export type AiTeammateTemplateCardItem = {
	id: number;
	name: string;
	description: string;
	provider: string;
	useCount: number;
	recommendCount: number;
	icon: LucideIcon;
	iconBg: string;
	iconColor: string;
};

interface AiTeammateCardProps {
	item: AiTeammateTemplateCardItem;
	onSelect?: (item: AiTeammateTemplateCardItem) => void;
	onRecommend?: (item: AiTeammateTemplateCardItem) => void;
	recommended?: boolean;
	recommending?: boolean;
}

export function AiTeammateCard({
	item,
	onSelect,
	onRecommend,
	recommended = false,
	recommending = false,
}: AiTeammateCardProps) {
	const Icon = item.icon;

	const handleCardClick = () => {
		onSelect?.(item);
	};

	return (
		<div
			className={cn(
				"group relative flex flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-4 text-left transition-all duration-300",
				"hover:-translate-y-1 hover:border-[var(--leros-primary)] hover:shadow-lg",
			)}
		>
			<button
				type="button"
				className="absolute inset-0 rounded-xl focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--leros-primary)] focus-visible:ring-offset-2"
				onClick={handleCardClick}
				aria-label={`查看 ${item.name}`}
			/>

			<div className="pointer-events-none relative mb-3 flex items-start gap-3">
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

			<div className="pointer-events-none relative mt-auto flex items-center justify-between border-t border-[var(--leros-control-border)] pt-3">
				{/* 中文注释：卡片底部固定展示 Lework 品牌标识。 */}
				<div className="flex min-w-0 items-center gap-1.5">
					<img src={APP_LOGO_SRC} alt="" className="size-4 shrink-0 rounded object-cover" />
					<span className="truncate text-[11px] text-[var(--leros-text-muted)]">
						{item.provider}
					</span>
				</div>
				<div className="flex shrink-0 items-center gap-3 text-[11px] text-[var(--leros-text-subtle)]">
					<span className="inline-flex items-center gap-1">
						<Eye className="size-3" aria-hidden="true" />
						{formatCompactCount(item.useCount)}
					</span>
					<button
						type="button"
						className={cn(
							"pointer-events-auto inline-flex items-center gap-1 rounded px-1 py-0.5 transition-colors hover:text-rose-500 disabled:cursor-not-allowed disabled:opacity-70",
							recommended && "text-rose-500",
						)}
						onClick={() => onRecommend?.(item)}
						disabled={recommending || recommended}
						aria-label={recommended ? "已点赞" : `点赞 ${item.name}`}
						title={recommended ? "已点赞" : "点赞"}
					>
						<Heart className={cn("size-3", recommended && "fill-current")} aria-hidden="true" />
						{formatCompactCount(item.recommendCount)}
					</button>
				</div>
			</div>
		</div>
	);
}

function formatCompactCount(value: number): string {
	if (value >= 10000) return `${(value / 10000).toFixed(value >= 100000 ? 0 : 1)}w`;
	if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}k`;
	return String(value);
}
