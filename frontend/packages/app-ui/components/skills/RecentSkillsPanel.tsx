"use client";

import { useCallback, useEffect, useState } from "react";
import type { SkillInstalledItem, SkillMarketplaceItem } from "@leros/store";
import { installedToCardItem, skillMarketplaceApi } from "@leros/store";

interface RecentSkillsPanelProps {
  onCardClick?: (skill: SkillMarketplaceItem) => void;
}

export function RecentSkillsPanel({ onCardClick }: RecentSkillsPanelProps) {
  const [recent, setRecent] = useState<SkillInstalledItem[]>([]);
  const [loaded, setLoaded] = useState(false);

  const fetchRecent = useCallback(async () => {
    try {
      const resp = await skillMarketplaceApi.listRecentUsed(6);
      setRecent(resp.data.data ?? []);
    } catch {
      // silently ignore
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    fetchRecent();
  }, [fetchRecent]);

  if (!loaded) return null;

  return (
    <div>
      <h3 className="text-sm font-semibold text-[var(--leros-text-strong)] mb-4">最近使用</h3>
      {recent.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          {recent.map((item) => {
            const displayName = item.display_name || item.name;
            return (
              <button
                key={item.name}
                type="button"
                onClick={() => onCardClick?.(installedToCardItem(item))}
                className="group flex items-center gap-3 rounded-lg border border-[var(--leros-control-border)] bg-white p-3 text-left transition-all hover:-translate-y-0.5 hover:border-[var(--leros-primary)] hover:shadow-sm"
              >
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-primary-soft)] text-[var(--leros-primary)] text-sm font-bold group-hover:bg-[var(--leros-primary)] group-hover:text-white transition-colors">
                  {displayName.charAt(0).toUpperCase()}
                </div>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium text-[var(--leros-text-strong)]">
                    {displayName}
                  </p>
                  <p className="truncate text-[11px] text-[var(--leros-text-muted)]">
                    {item.description}
                  </p>
                </div>
              </button>
            );
          })}
        </div>
      ) : (
        <p className="text-xs text-[var(--leros-text-subtle)] py-4">暂无最近使用的技能</p>
      )}
    </div>
  );
}
