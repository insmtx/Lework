"use client";

import { cn } from "@leros/ui/lib/utils";
import { Star } from "lucide-react";
import type { SkillMarketplaceItem } from "@leros/store";

interface SkillCardProps {
  skill: SkillMarketplaceItem;
  variant?: "marketplace" | "mine";
  /** Called when the card body is clicked (for navigation to detail page) */
  onClick?: (skill: SkillMarketplaceItem) => void;
}

export function SkillCard({
  skill,
  variant = "marketplace",
  onClick,
}: SkillCardProps) {
  const isLerosAI = skill.author === "Leros AI";
  const isMine = variant === "mine";

  const handleCardClick = () => {
    onClick?.(skill);
  };

  return (
    <div
      role={onClick ? "button" : undefined}
      tabIndex={onClick ? 0 : undefined}
      onKeyDown={
        onClick
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                handleCardClick();
              }
            }
          : undefined
      }
      onClick={handleCardClick}
      className={cn(
        "group flex flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-4 transition-all duration-300",
        "hover:-translate-y-1 hover:border-[var(--leros-primary)] hover:shadow-lg",
        onClick && "cursor-pointer",
      )}
    >
      {/* Top: avatar + info + rating */}
      <div className="flex items-start justify-between mb-3">
        <div className="flex items-center gap-3">
          {skill.icon ? (
            <img
              src={skill.icon}
              alt={skill.name}
              className="h-9 w-9 shrink-0 rounded-lg object-cover"
            />
          ) : (
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-soft)] text-[var(--leros-primary)] text-sm font-bold transition-all duration-300 group-hover:bg-[var(--leros-primary)] group-hover:text-white">
              {skill.name.charAt(0).toUpperCase()}
            </div>
          )}
          <div>
            <div className="flex items-center gap-1 mb-0.5">
              <h3 className="text-sm font-semibold text-[var(--leros-text-strong)] truncate max-w-[140px]">
                {skill.name}
              </h3>
              {isLerosAI && (
                <span
                  className="inline-flex shrink-0 text-[var(--leros-primary)]"
                  title="已验证"
                >
                  <svg
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
        <div className="flex shrink-0 items-center gap-1 rounded bg-amber-50 px-1.5 py-0.5 border border-amber-100">
          <Star className="size-3 fill-amber-500 text-amber-500" />
          <span className="text-[10px] font-bold text-amber-700">4.5</span>
        </div>
      </div>

      {/* Description */}
      <p className="flex-1 text-xs text-[var(--leros-text-muted)] mb-3 leading-relaxed line-clamp-2">
        {skill.description}
      </p>

      {/* Tags + install count */}
      <div className="flex items-center gap-1.5 mb-3">
        <div className="flex flex-wrap gap-1.5 flex-1 min-w-0">
          {(skill.tags ?? []).map((tag: string) => (
            <span
              key={tag}
              className="px-2 py-0.5 rounded border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] text-[10px] font-medium uppercase tracking-tight text-[var(--leros-text-muted)]"
            >
              {tag}
            </span>
          ))}
        </div>
        {!isMine && (
          <span className="shrink-0 text-[10px] text-[var(--leros-text-subtle)] ml-auto">
            {skill.installs} 安装
          </span>
        )}
      </div>

    </div>
  );
}
