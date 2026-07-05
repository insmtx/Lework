"use client";

import {
	type BackendAITeammateTemplate,
	digitalAssistantApi,
	useAppStore,
	useDAStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import {
	BarChart3,
	BrainCircuit,
	FileCheck2,
	FileText,
	Gavel,
	LineChart,
	type LucideIcon,
	Search,
	Users,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { AiTeammateCard, type AiTeammateTemplateCardItem } from "./AiTeammateCard";
import { AiTeammateDetailDialog, type AiTeammateDetailDialogItem } from "./AiTeammateDetailDialog";

type CategoryOption = {
	value: string;
	label: string;
};

type CategoryVisual = {
	icon: LucideIcon;
	iconBg: string;
	iconColor: string;
	label: string;
};

const CATEGORY_VISUALS: Record<string, CategoryVisual> = {
	bidding: {
		label: "招投标",
		icon: FileCheck2,
		iconBg: "bg-sky-100",
		iconColor: "text-sky-600",
	},
	office: {
		label: "办公协同",
		icon: FileText,
		iconBg: "bg-amber-100",
		iconColor: "text-amber-700",
	},
	data: {
		label: "数据分析",
		icon: BarChart3,
		iconBg: "bg-cyan-100",
		iconColor: "text-cyan-700",
	},
	hr: {
		label: "人力资源",
		icon: Users,
		iconBg: "bg-rose-100",
		iconColor: "text-rose-600",
	},
	legal: {
		label: "法务合规",
		icon: Gavel,
		iconBg: "bg-stone-100",
		iconColor: "text-stone-700",
	},
	finance: {
		label: "金融投资",
		icon: LineChart,
		iconBg: "bg-emerald-100",
		iconColor: "text-emerald-700",
	},
};

const DEFAULT_CATEGORY_VISUAL: CategoryVisual = {
	label: "通用能力",
	icon: BrainCircuit,
	iconBg: "bg-indigo-100",
	iconColor: "text-indigo-600",
};

const RECOMMENDED_TEMPLATE_STORAGE_KEY = "leros.aiTeammates.recommendedTemplateIds";

export function AiTeammatesView() {
	const { assistants, assistantsLoaded, createAssistantFromTemplate, fetchAssistants } = useDAStore(
		(s) => s,
	);
	const [templates, setTemplates] = useState<BackendAITeammateTemplate[]>([]);
	const [loading, setLoading] = useState(true);
	const [adoptingTemplateId, setAdoptingTemplateId] = useState<number | null>(null);
	const [recommendingTemplateId, setRecommendingTemplateId] = useState<number | null>(null);
	const [recommendedTemplateIds, setRecommendedTemplateIds] = useState<Set<number>>(
		() => new Set(),
	);
	const [selectedTemplate, setSelectedTemplate] = useState<AiTeammateDetailDialogItem | null>(null);
	const [keyword, setKeyword] = useState("");
	const [debouncedKeyword, setDebouncedKeyword] = useState("");
	const [activeCategory, setActiveCategory] = useState("");

	useEffect(() => {
		fetchAssistants();
	}, [fetchAssistants]);

	useEffect(() => {
		setRecommendedTemplateIds(readRecommendedTemplateIds());
	}, []);

	useEffect(() => {
		let cancelled = false;

		const fetchTemplates = async () => {
			try {
				setLoading(true);
				const res = await digitalAssistantApi.listTemplates({
					status: "active",
					list_all: true,
					limit: 100,
				});
				if (cancelled) return;
				setTemplates(res.data.data?.items ?? []);
			} catch (err) {
				if (cancelled) return;
				console.error("fetch ai teammate templates error:", err);
				toast.error("AI 队友模板加载失败");
			} finally {
				if (!cancelled) setLoading(false);
			}
		};

		fetchTemplates();
		return () => {
			cancelled = true;
		};
	}, []);

	useEffect(() => {
		const timer = window.setTimeout(() => setDebouncedKeyword(keyword.trim()), 300);
		return () => window.clearTimeout(timer);
	}, [keyword]);

	const categories = useMemo<CategoryOption[]>(() => {
		const seen = new Set<string>();
		const options: CategoryOption[] = [{ value: "", label: "全部" }];

		for (const item of templates) {
			const category = item.category?.trim();
			if (!category || seen.has(category)) continue;
			seen.add(category);
			options.push({
				value: category,
				label: CATEGORY_VISUALS[category]?.label ?? category,
			});
		}

		return options;
	}, [templates]);

	const filteredItems = useMemo(() => {
		const normalizedKeyword = debouncedKeyword.toLowerCase();

		return templates.filter((item) => {
			const matchesCategory = !activeCategory || item.category === activeCategory;
			if (!matchesCategory) return false;
			if (!normalizedKeyword) return true;

			const searchable = [
				item.name,
				item.description,
				item.provider,
				...(item.expertise ?? []),
				...(item.tags ?? []),
			]
				.filter(Boolean)
				.join(" ")
				.toLowerCase();

			return searchable.includes(normalizedKeyword);
		});
	}, [activeCategory, debouncedKeyword, templates]);

	const cardItems = useMemo<AiTeammateDetailDialogItem[]>(() => {
		return filteredItems.map((item) => {
			const visual = CATEGORY_VISUALS[item.category ?? ""] ?? DEFAULT_CATEGORY_VISUAL;

			return {
				id: item.id,
				name: item.name,
				description: item.description ?? "",
				provider: item.provider || "Lework",
				useCount: item.use_count ?? 0,
				recommendCount: item.recommend_count ?? 0,
				icon: visual.icon,
				iconBg: visual.iconBg,
				iconColor: visual.iconColor,
				categoryLabel: visual.label,
				template: item,
			};
		});
	}, [filteredItems]);

	const adoptedTemplateIds = useMemo(() => {
		const ids = new Set<number>();
		for (const assistant of assistants) {
			if (assistant.templateId) ids.add(assistant.templateId);
		}
		return ids;
	}, [assistants]);

	const handleSelectTemplate = (item: AiTeammateTemplateCardItem) => {
		const detailItem = cardItems.find((cardItem) => cardItem.id === item.id);
		if (detailItem) setSelectedTemplate(detailItem);
	};

	const handleRecommendTemplate = async (item: AiTeammateTemplateCardItem) => {
		if (recommendingTemplateId) return;
		if (recommendedTemplateIds.has(item.id)) {
			toast.info("已经点赞过这个 AI 队友了");
			return;
		}

		setRecommendingTemplateId(item.id);
		setRecommendedTemplateIds((current) => {
			const next = new Set(current);
			next.add(item.id);
			writeRecommendedTemplateIds(next);
			return next;
		});
		setTemplates((current) => incrementTemplateRecommendCount(current, item.id, 1));
		setSelectedTemplate((current) =>
			current?.id === item.id
				? { ...current, recommendCount: current.recommendCount + 1 }
				: current,
		);

		try {
			await digitalAssistantApi.incrementTemplateRecommendCount({ id: item.id });
		} catch (err) {
			console.error("recommend ai teammate template error:", err);
			setTemplates((current) => incrementTemplateRecommendCount(current, item.id, -1));
			setSelectedTemplate((current) =>
				current?.id === item.id
					? { ...current, recommendCount: Math.max(0, current.recommendCount - 1) }
					: current,
			);
			setRecommendedTemplateIds((current) => {
				const next = new Set(current);
				next.delete(item.id);
				writeRecommendedTemplateIds(next);
				return next;
			});
			toast.error("点赞失败，请稍后再试");
		} finally {
			setRecommendingTemplateId(null);
		}
	};

	const handleAdoptTemplate = async (item: AiTeammateDetailDialogItem) => {
		if (adoptingTemplateId) return;
		if (adoptedTemplateIds.has(item.id)) {
			toast.info("该 AI 队友已在「我的队友」中");
			return;
		}

		setAdoptingTemplateId(item.id);
		try {
			if (!assistantsLoaded) {
				await fetchAssistants();
			}

			const latestAdopted = new Set(
				useAppStore
					.getState()
					.assistants.map((assistant) => assistant.templateId)
					.filter((templateId): templateId is number => Boolean(templateId)),
			);
			if (latestAdopted.has(item.id)) {
				toast.info("该 AI 队友已在「我的队友」中");
				setSelectedTemplate(null);
				return;
			}

			const createdAssistant = await createAssistantFromTemplate({ template_id: item.id });
			if (!createdAssistant) throw new Error("No assistant returned");

			setTemplates((current) =>
				current.map((template) =>
					template.id === item.id
						? { ...template, use_count: (template.use_count ?? 0) + 1 }
						: template,
				),
			);

			toast.success(`已添加到我的队友：${createdAssistant.name}`);
			setSelectedTemplate(null);
		} catch (err) {
			console.error("adopt ai teammate error:", err);
			toast.error("添加到我的队友失败");
		} finally {
			setAdoptingTemplateId(null);
		}
	};

	const navigateToAssistants = () => {
		if (window.location.hash) {
			window.location.hash = "/assistants";
			return;
		}
		window.location.href = "/assistants";
	};

	return (
		<div
			data-slot="ai-teammates-view"
			className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<div className="border-b border-[var(--leros-control-border)] px-6 py-5">
				<div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
					<h1 className="text-xl font-semibold text-[var(--leros-text-strong)]">AI队友</h1>
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
							onClick={navigateToAssistants}
						>
							我的队友
						</Button>
					</div>
				</div>

				<div className="mt-4 flex items-center gap-2 overflow-x-auto no-scrollbar">
					{categories.map((category) => {
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
				{loading ? (
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
						{Array.from({ length: 8 }).map((_, index) => (
							<div
								key={index}
								className="h-36 animate-pulse rounded-xl border border-[var(--leros-control-border)] bg-white"
							/>
						))}
					</div>
				) : cardItems.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-16 text-[var(--leros-text-subtle)]">
						<p className="text-sm">暂无符合条件的 AI 队友</p>
					</div>
				) : (
					<div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
						{cardItems.map((item) => (
							<AiTeammateCard
								key={item.id}
								item={item}
								onSelect={handleSelectTemplate}
								onRecommend={handleRecommendTemplate}
								recommended={recommendedTemplateIds.has(item.id)}
								recommending={recommendingTemplateId === item.id}
							/>
						))}
					</div>
				)}
			</div>

			<AiTeammateDetailDialog
				open={!!selectedTemplate}
				item={selectedTemplate}
				onOpenChange={(open) => {
					if (!open) setSelectedTemplate(null);
				}}
				onAdopt={handleAdoptTemplate}
				onRecommend={handleRecommendTemplate}
				adopting={selectedTemplate ? adoptingTemplateId === selectedTemplate.id : false}
				adopted={selectedTemplate ? adoptedTemplateIds.has(selectedTemplate.id) : false}
				recommended={selectedTemplate ? recommendedTemplateIds.has(selectedTemplate.id) : false}
				recommending={selectedTemplate ? recommendingTemplateId === selectedTemplate.id : false}
			/>
		</div>
	);
}

function incrementTemplateRecommendCount(
	items: BackendAITeammateTemplate[],
	templateID: number,
	step: 1 | -1,
): BackendAITeammateTemplate[] {
	return items.map((template) =>
		template.id === templateID
			? { ...template, recommend_count: Math.max(0, (template.recommend_count ?? 0) + step) }
			: template,
	);
}

function readRecommendedTemplateIds(): Set<number> {
	if (typeof window === "undefined") return new Set();
	try {
		const raw = window.localStorage.getItem(RECOMMENDED_TEMPLATE_STORAGE_KEY);
		const values = raw ? (JSON.parse(raw) as unknown) : [];
		if (!Array.isArray(values)) return new Set();
		return new Set(values.filter((value): value is number => Number.isSafeInteger(value)));
	} catch {
		return new Set();
	}
}

function writeRecommendedTemplateIds(ids: Set<number>) {
	if (typeof window === "undefined") return;
	window.localStorage.setItem(RECOMMENDED_TEMPLATE_STORAGE_KEY, JSON.stringify([...ids]));
}
