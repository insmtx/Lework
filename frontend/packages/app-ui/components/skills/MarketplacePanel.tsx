"use client";

import {
	type OfficialPluginMarketplaceItem,
	officialPluginMarketplaceApi,
	type SkillMarketplaceItem,
} from "@leros/store";
import { useEffect, useMemo, useState } from "react";
import { SkillCard } from "./SkillCard";
import { filterSkillsByCategory, type SkillCatalogCategory } from "./skillCatalog";

const PAGE_SIZE = 90;

function officialToSkillCard(item: OfficialPluginMarketplaceItem): SkillMarketplaceItem {
	return {
		source_type: "official",
		skill_id: item.public_id,
		name: item.code,
		display_name: item.name,
		description: item.description ?? "",
		version: item.version,
		author: item.author,
		category: item.category,
		tags: item.tags,
		icon: item.icon ?? "",
		installs: 0,
		verified: item.verified,
		installed: item.installed,
		marketplace_available: item.marketplace_available,
		latest_version: item.latest_version,
		update_available: item.update_available,
		organization_override: item.organization_override,
	};
}

interface MarketplacePanelProps {
	/** Called when a skill card is clicked (for navigation to detail page) */
	onCardClick?: (skill: SkillMarketplaceItem) => void;
	onUse?: (skill: SkillMarketplaceItem) => void;
	isAuthenticated?: boolean;
	/** Changes after an official plugin installation so the catalogue is reloaded. */
	refreshSeq?: number;
	keyword?: string;
	category?: SkillCatalogCategory;
	onCountChange?: (count: number) => void;
}

export function MarketplacePanel({
	onCardClick,
	onUse,
	isAuthenticated = true,
	refreshSeq = 0,
	keyword = "",
	category = "all",
	onCountChange,
}: MarketplacePanelProps) {
	const [items, setItems] = useState<SkillMarketplaceItem[]>([]);
	const [loading, setLoading] = useState(true);
	const [mounted, setMounted] = useState(false);

	useEffect(() => {
		setMounted(true);
	}, []);

	const searchKeyword = isAuthenticated ? keyword : "";
	// Fetch the official plugin catalogue on keyword change.
	useEffect(() => {
		if (!mounted) return;
		if (!isAuthenticated) {
			setItems([]);
			setLoading(false);
			return;
		}
		let cancelled = false;
		const fetchItems = async () => {
			setLoading(true);
			try {
				const resp = await officialPluginMarketplaceApi.list({
					kind: "skill",
					keyword: searchKeyword || undefined,
					limit: PAGE_SIZE,
				});
				if (cancelled) return;
				const newItems = (resp.data.data.items ?? []).map(officialToSkillCard);
				setItems(newItems);
			} catch (err) {
				if (!cancelled) console.error("Failed to fetch skills:", err);
			} finally {
				if (!cancelled) setLoading(false);
			}
		};
		fetchItems();
		return () => {
			cancelled = true;
		};
	}, [mounted, isAuthenticated, searchKeyword, refreshSeq]);

	const visibleItems = useMemo(() => filterSkillsByCategory(items, category), [items, category]);

	useEffect(() => {
		onCountChange?.(visibleItems.length);
	}, [onCountChange, visibleItems.length]);

	return (
		<div>
			{!mounted || (isAuthenticated && loading) ? (
				<div className="flex items-center justify-center py-20 text-sm text-[var(--leros-text-subtle)]">
					加载中...
				</div>
			) : visibleItems.length === 0 ? (
				<div className="flex flex-col items-center justify-center rounded-2xl border border-dashed border-[var(--leros-control-border)] bg-white py-20 text-[var(--leros-text-subtle)]">
					<p className="text-sm">暂无符合条件的技能</p>
				</div>
			) : (
				<div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
					{visibleItems.map((skill) => (
						<SkillCard key={skill.skill_id} skill={skill} onClick={onCardClick} onUse={onUse} />
					))}
				</div>
			)}
		</div>
	);
}
