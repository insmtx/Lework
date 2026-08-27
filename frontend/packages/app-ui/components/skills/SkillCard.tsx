"use client";

import type { SkillMarketplaceItem } from "@leros/store";
import { Tooltip, TooltipContent, TooltipTrigger } from "@leros/ui/components/ui/tooltip";
import { cn } from "@leros/ui/lib/utils";
import type { KeyboardEvent, MouseEvent } from "react";

interface SkillCardProps {
	skill: SkillMarketplaceItem;
	variant?: "marketplace" | "mine" | "owned";
	/** Called when the card body is clicked (for navigation to detail page) */
	onClick?: (skill: SkillMarketplaceItem) => void;
	/** Uses the Skill directly without installing it from the browser. */
	onUse?: (skill: SkillMarketplaceItem) => void;
}

export function SkillCard({ skill, variant = "marketplace", onClick, onUse }: SkillCardProps) {
	const isMine = variant === "mine" || variant === "owned";
	const showVisibility = variant === "owned";
	const displayName = skill.display_name || skill.name;
	const marketplaceStatus =
		!isMine && skill.installed && skill.marketplace_available === false
			? { label: "已下架", className: "border-slate-200 bg-slate-100 text-slate-600" }
			: !isMine && skill.update_available
				? { label: "有更新", className: "border-amber-200 bg-amber-50 text-amber-700" }
				: !isMine && skill.organization_override
					? {
							label: "组织同名版本",
							className: "border-blue-200 bg-blue-50 text-blue-700",
						}
					: null;
	const ownedStatus =
		showVisibility && skill.visibility
			? {
					label: skill.visibility === "public" ? "公开" : "私有",
					className:
						skill.visibility === "public"
							? "border-green-200 bg-green-50 text-green-700"
							: "border-slate-200 bg-slate-100 text-slate-600",
				}
			: null;
	const roleStatus =
		variant === "mine" && skill.permission
			? skill.permission.role === "owner"
				? { label: "所有者", className: "border-amber-200 bg-amber-50 text-amber-700" }
				: skill.permission.role === "admin"
					? { label: "管理员", className: "border-violet-200 bg-violet-50 text-violet-700" }
					: null
			: null;

	const handleCardClick = () => {
		onClick?.(skill);
	};

	const handleCardKeyDown = (event: KeyboardEvent<HTMLElement>) => {
		if (event.key === "Enter" || event.key === " ") {
			event.preventDefault();
			handleCardClick();
		}
	};

	const handleUse = (event: MouseEvent<HTMLButtonElement>) => {
		event.stopPropagation();
		onUse?.(skill);
	};

	const description = skill.description?.trim() || "暂无技能说明";

	return (
		<Tooltip>
			{/* 中文注释：卡片本身可能是可点击 article，用 render 承接悬浮，避免再套一层 button。 */}
			<TooltipTrigger
				render={
					<article
						onClick={handleCardClick}
						onKeyDown={onClick ? handleCardKeyDown : undefined}
						role={onClick ? "button" : undefined}
						tabIndex={onClick ? 0 : undefined}
						className={cn(
							"group flex min-h-[168px] flex-col rounded-lg border border-[var(--leros-control-border)] bg-white p-4 text-left transition-all duration-200",
							onClick
								? "cursor-pointer hover:-translate-y-0.5 hover:border-[var(--leros-primary)] hover:shadow-[0_12px_28px_rgba(76,78,230,0.08)]"
								: "cursor-default",
						)}
					/>
				}
			>
				<div className="flex items-start justify-between gap-3">
					<div className="flex min-w-0 items-center gap-2.5">
						{skill.icon ? (
							<img
								src={skill.icon}
								alt={displayName}
								className="size-9 shrink-0 rounded-md object-cover"
							/>
						) : (
							<div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-[var(--leros-primary-soft)] text-sm font-semibold text-[var(--leros-primary)]">
								{displayName.charAt(0).toUpperCase()}
							</div>
						)}
						<div className="min-w-0">
							<h3 className="truncate text-[13px] font-semibold text-[var(--leros-text-strong)]">
								{displayName}
							</h3>
							<p className="mt-0.5 truncate text-[11px] text-[var(--leros-text-subtle)]">
								由 {skill.author || skill.source_type} 提供
							</p>
						</div>
					</div>
					<div className="flex shrink-0 items-center gap-1">
						<span
							className={cn(
								"rounded-md border px-1.5 py-0.5 text-[10px] font-medium",
								marketplaceStatus?.className ??
									ownedStatus?.className ??
									"border-transparent bg-[var(--leros-primary-soft)] text-[var(--leros-primary)]",
							)}
						>
							{marketplaceStatus?.label ?? ownedStatus?.label ?? (isMine ? "组织技能" : "技能市场")}
						</span>
						{roleStatus && (
							<span
								className={cn(
									"rounded-md border px-1.5 py-0.5 text-[10px] font-medium",
									roleStatus.className,
								)}
							>
								{roleStatus.label}
							</span>
						)}
					</div>
				</div>

				<p className="mt-2.5 h-10 line-clamp-2 overflow-hidden text-[12px] leading-5 text-[var(--leros-text-muted)]">
					{description}
				</p>

				{((skill.tags?.length ?? 0) > 0 || onUse) && (
					<div className="mt-auto flex min-h-7 items-center justify-between gap-3 pt-2.5">
						<div className="flex min-w-0 flex-wrap items-center gap-1.5">
							{(skill.tags ?? []).slice(0, 3).map((tag: string) => (
								<span
									key={tag}
									className="rounded-md border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-1.5 py-0.5 text-[10px] text-[var(--leros-text-muted)]"
								>
									{tag}
								</span>
							))}
						</div>
						{onUse && (
							<button
								type="button"
								onClick={handleUse}
								className="h-7 shrink-0 rounded-md bg-[var(--leros-primary)] px-2.5 text-[11px] font-medium text-white transition-opacity hover:opacity-90"
							>
								使用
							</button>
						)}
					</div>
				)}
			</TooltipTrigger>
			<TooltipContent
				side="bottom"
				align="start"
				className="max-w-sm whitespace-normal text-left leading-5"
			>
				{description}
			</TooltipContent>
		</Tooltip>
	);
}
