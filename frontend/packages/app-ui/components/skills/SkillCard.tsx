"use client";

import type { SkillMarketplaceItem } from "@leros/store";
import { cn } from "@leros/ui/lib/utils";
import { Star } from "lucide-react";
import type { KeyboardEvent } from "react";

interface SkillCardProps {
	skill: SkillMarketplaceItem;
	variant?: "marketplace" | "mine";
	/** Called when the card body is clicked (for navigation to detail page) */
	onClick?: (skill: SkillMarketplaceItem) => void;
}

export function SkillCard({ skill, variant = "marketplace", onClick }: SkillCardProps) {
	const isLerosAI = skill.author === "Lework";
	const isMine = variant === "mine";
	const displayName = skill.display_name || skill.name;

	const handleCardClick = () => {
		onClick?.(skill);
	};

	const handleCardKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
		if (event.key === "Enter" || event.key === " ") {
			event.preventDefault();
			handleCardClick();
		}
	};

	return (
		<button
			type="button"
			onClick={handleCardClick}
			onKeyDown={onClick ? handleCardKeyDown : undefined}
			disabled={!onClick}
			className={cn(
				"group flex flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-4 text-left transition-all duration-300",
				onClick
					? "cursor-pointer hover:-translate-y-1 hover:border-[var(--leros-primary)] hover:shadow-lg"
					: "cursor-default",
			)}
		>
			{/* Top: avatar + info + rating */}
			<div className="mb-3 flex items-start justify-between">
				<div className="flex items-center gap-3">
					{skill.icon ? (
						<img
							src={skill.icon}
							alt={displayName}
							className="h-9 w-9 shrink-0 rounded-lg object-cover"
						/>
					) : (
						<div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-soft)] text-[var(--leros-primary)] text-sm font-bold transition-all duration-300 group-hover:bg-[var(--leros-primary)] group-hover:text-white">
							{displayName.charAt(0).toUpperCase()}
						</div>
					)}
					<div>
						<div className="mb-0.5 flex items-center gap-1">
							<h3 className="max-w-[140px] truncate text-sm font-semibold text-[var(--leros-text-strong)]">
								{displayName}
							</h3>
							{isLerosAI && (
								<span className="inline-flex shrink-0 text-[var(--leros-primary)]" title="已验证">
									<svg
										role="img"
										aria-label="已验证"
										width="12"
										height="12"
										viewBox="0 0 24 24"
										fill="currentColor"
									>
										<path d="M12 2L15.09 8.26L22 9.27L17 14.14L18.18 21.02L12 17.77L5.82 21.02L7 14.14L2 9.27L8.91 8.26L12 2Z" />
									</svg>
								</span>
							)}
						</div>
						<p className="text-[11px] text-[var(--leros-text-subtle)]">
							由 {skill.author || skill.source_type} 提供
						</p>
					</div>
				</div>
				<div className="flex shrink-0 items-center gap-1 rounded border border-amber-100 bg-amber-50 px-1.5 py-0.5">
					<Star className="size-3 fill-amber-500 text-amber-500" />
					<span className="text-[10px] font-bold text-amber-700">4.5</span>
				</div>
			</div>

			{/* Description */}
			<p className="mb-3 flex-1 line-clamp-2 text-xs leading-relaxed text-[var(--leros-text-muted)]">
				{skill.description}
			</p>

			{/* Tags + install count */}
			<div className="mb-3 flex items-center gap-1.5">
				<div className="flex min-w-0 flex-1 flex-wrap gap-1.5">
					{(skill.tags ?? []).map((tag: string) => (
						<span
							key={tag}
							className="rounded border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-2 py-0.5 text-[10px] font-medium uppercase tracking-tight text-[var(--leros-text-muted)]"
						>
							{tag}
						</span>
					))}
				</div>
				{!isMine && (
					<span className="ml-auto shrink-0 text-[10px] text-[var(--leros-text-subtle)]">
						{skill.installs} 安装
					</span>
				)}
			</div>
		</button>
	);
}
