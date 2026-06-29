"use client";

import { useCallback, useEffect, useState } from "react";
import type { SkillMarketplaceItem } from "@leros/store";
import { skillMarketplaceApi, installedToCardItem } from "@leros/store";
import { SkillCard } from "./SkillCard";

interface MySkillsPanelProps {
  /** Called when a skill card is clicked (for navigation to detail page) */
  onCardClick?: (skill: SkillMarketplaceItem) => void;
  refreshSeq?: number;
}

export function MySkillsPanel({ onCardClick, refreshSeq = 0 }: MySkillsPanelProps) {
  const [skills, setSkills] = useState<SkillMarketplaceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  const fetchInstalled = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const resp = await skillMarketplaceApi.installed();
      const raw = resp.data.data.skills ?? [];
      const list = raw.map(installedToCardItem);
      setSkills(list);
    } catch (err: any) {
      const msg = err?.response?.data?.message ?? err?.message ?? "加载失败";
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!mounted) return;
    fetchInstalled();
  }, [mounted, fetchInstalled, refreshSeq]);

  // Not yet mounted (SSR hydration guard)
  if (!mounted) {
    return (
      <div className="flex items-center justify-center py-16 text-sm text-[var(--leros-text-subtle)]">
        加载中...
      </div>
    );
  }

  // Loading state
  if (loading) {
    return (
      <div className="flex items-center justify-center py-16 text-sm text-[var(--leros-text-subtle)]">
        加载中...
      </div>
    );
  }

  // Error state
  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-[var(--leros-text-subtle)] gap-3">
        <p className="text-sm">{error}</p>
        <button
          type="button"
          onClick={fetchInstalled}
          className="rounded-md border border-[var(--leros-control-border)] px-3 py-1 text-xs text-[var(--leros-primary)] hover:bg-[var(--leros-primary-soft)] transition-colors"
        >
          重试
        </button>
      </div>
    );
  }

  // Empty state
  if (skills.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-[var(--leros-text-subtle)]">
        <p className="text-sm">暂无已安装的技能</p>
        <p className="text-xs mt-1">前往"技能市场"发现并安装技能</p>
      </div>
    );
  }

  // Data grid
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {skills.map((skill) => (
        <SkillCard
          key={skill.skill_id}
          skill={skill}
          variant="mine"
          onClick={onCardClick}
        />
      ))}
    </div>
  );
}
