"use client";

import type { SkillMarketplaceItem } from "@leros/store";
import { pluginApi, pluginToSkillCard } from "@leros/store";
import { useCallback, useEffect, useMemo, useState } from "react";
import { SkillCard } from "./SkillCard";
import { filterSkillsByCategory, type SkillCatalogCategory } from "./skillCatalog";

interface MySkillsPanelProps {
	/** Called when a skill card is clicked (for navigation to detail page) */
	onCardClick?: (skill: SkillMarketplaceItem) => void;
	onUse?: (skill: SkillMarketplaceItem) => void;
	refreshSeq?: number;
	keyword?: string;
	category?: SkillCatalogCategory;
	relation?: "owner" | "admin" | "viewer" | "shared";
	excludeMarketplaceBased?: boolean;
	cardVariant?: "mine" | "owned";
	emptyMessage?: string;
	onCountChange?: (count: number) => void;
}

export function MySkillsPanel({
	onCardClick,
	onUse,
	refreshSeq = 0,
	keyword = "",
	category = "all",
	relation,
	excludeMarketplaceBased,
	cardVariant = "mine",
	emptyMessage,
	onCountChange,
}: MySkillsPanelProps) {
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
			const params = {
				kind: "skill",
				status: "active",
				...(keyword ? { keyword } : {}),
				...(relation ? { relation } : {}),
				...(excludeMarketplaceBased ? { exclude_marketplace_based: true } : {}),
			};
			const resp = await pluginApi.list(params);
			const list = (resp.data.data.plugins ?? []).map(pluginToSkillCard);
			setSkills(list);
		} catch (err: any) {
			const msg = err?.response?.data?.message ?? err?.message ?? "加载失败";
			setError(msg);
		} finally {
			setLoading(false);
		}
	}, [keyword, relation, excludeMarketplaceBased]);

	useEffect(() => {
		if (!mounted) return;
		fetchInstalled();
	}, [mounted, fetchInstalled, refreshSeq]);

	const visibleSkills = useMemo(() => filterSkillsByCategory(skills, category), [skills, category]);

	useEffect(() => {
		onCountChange?.(visibleSkills.length);
	}, [onCountChange, visibleSkills.length]);

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
	if (visibleSkills.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--leros-control-border)] bg-white py-20 text-[var(--leros-text-subtle)]">
				<p className="text-sm">
					{keyword || category !== "all" ? "暂无符合条件的技能" : (emptyMessage ?? "暂无组织技能")}
				</p>
				{!keyword && category === "all" && (
					<p className="mt-1 text-xs">可通过创作或导入添加组织自有技能</p>
				)}
			</div>
		);
	}

	// Data grid
	return (
		<div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
			{visibleSkills.map((skill) => (
				<SkillCard
					key={skill.skill_id}
					skill={skill}
					variant={cardVariant}
					onClick={onCardClick}
					onUse={onUse}
				/>
			))}
		</div>
	);
}
