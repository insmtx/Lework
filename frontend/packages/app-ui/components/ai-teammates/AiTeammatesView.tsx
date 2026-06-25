"use client";

import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { AiTeammateCard } from "./AiTeammateCard";
import { notifyFeatureUnavailable } from "./feature-unavailable";
import { AI_TEAMMATE_CATEGORIES, AI_TEAMMATE_ITEMS, type AiTeammateCategory } from "./mock-data";

export function AiTeammatesView() {
	const [keyword, setKeyword] = useState("");
	const [debouncedKeyword, setDebouncedKeyword] = useState("");
	const [activeCategory, setActiveCategory] = useState<"" | AiTeammateCategory>("");

	useEffect(() => {
		const timer = window.setTimeout(() => setDebouncedKeyword(keyword.trim()), 300);
		return () => window.clearTimeout(timer);
	}, [keyword]);

	const filteredItems = useMemo(() => {
		const normalizedKeyword = debouncedKeyword.toLowerCase();

		return AI_TEAMMATE_ITEMS.filter((item) => {
			const matchesCategory = !activeCategory || item.category === activeCategory;
			if (!matchesCategory) return false;
			if (!normalizedKeyword) return true;

			return (
				item.name.toLowerCase().includes(normalizedKeyword) ||
				item.description.toLowerCase().includes(normalizedKeyword) ||
				item.provider.toLowerCase().includes(normalizedKeyword)
			);
		});
	}, [activeCategory, debouncedKeyword]);

	return (
		<div
			data-slot="ai-teammates-view"
			className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<div className="border-b border-[var(--leros-control-border)] px-6 py-5">
				<div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
					<h1 className="text-xl font-bold text-[var(--leros-text-strong)]">AI队友</h1>
					<div className="flex w-full flex-col gap-3 sm:flex-row sm:items-center lg:w-auto">
						<div className="relative flex-1 sm:min-w-[220px] sm:max-w-xs">
							<Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-[var(--leros-text-subtle)]" />
							<input
								type="text"
								value={keyword}
								onChange={(event) => setKeyword(event.target.value)}
								placeholder="搜索 AI 队友"
								className="w-full rounded-md border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] py-1.5 pl-7 pr-2 text-xs text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] transition-colors focus:border-[var(--leros-primary)] focus:bg-white focus:outline-none"
							/>
						</div>
						<Button
							type="button"
							size="sm"
							className="shrink-0 rounded-full px-4"
							onClick={notifyFeatureUnavailable}
						>
							我的队友
						</Button>
					</div>
				</div>

				<div className="mt-4 flex items-center gap-2 overflow-x-auto no-scrollbar">
					{AI_TEAMMATE_CATEGORIES.map((category) => {
						const isActive = activeCategory === category.value;

						return (
							<button
								type="button"
								key={category.label}
								onClick={() => setActiveCategory(category.value)}
								className={cn(
									"shrink-0 whitespace-nowrap rounded-full border px-3.5 py-1 text-xs font-medium transition-colors",
									isActive
										? "border-[var(--leros-primary)] bg-[var(--leros-primary-soft)] text-[var(--leros-primary)]"
										: "border-[var(--leros-control-border)] bg-transparent text-[var(--leros-text-muted)] hover:border-[var(--leros-text-subtle)] hover:text-[var(--leros-text)]",
								)}
							>
								{category.label}
							</button>
						);
					})}
				</div>
			</div>

			<div className="min-h-0 flex-1 overflow-y-auto px-6 py-8">
				{filteredItems.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-16 text-[var(--leros-text-subtle)]">
						<p className="text-sm">暂无符合条件的 AI 队友</p>
					</div>
				) : (
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
						{filteredItems.map((item) => (
							<AiTeammateCard key={item.id} item={item} onSelect={notifyFeatureUnavailable} />
						))}
					</div>
				)}
			</div>
		</div>
	);
}
