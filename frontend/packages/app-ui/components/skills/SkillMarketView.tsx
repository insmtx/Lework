"use client";

import { Button } from "@leros/ui/components/ui/button";
import { cn } from "@leros/ui/lib/utils";
import { Loader2, Plus, Search, SlidersHorizontal, Star } from "lucide-react";
import { toast } from "sonner";
import { useCallback, useEffect, useRef, useState } from "react";
import { skillMarketplaceApi, type SkillMarketplaceItem } from "@leros/store";

const CATEGORIES = [
  { value: "", label: "全部" },
  { value: "analysis", label: "数据分析" },
  { value: "language", label: "自然语言" },
  { value: "vision", label: "视觉/媒体" },
  { value: "code", label: "代码生成" },
];

const PAGE_SIZE = 80;

export function SkillMarketView() {
  const [items, setItems] = useState<SkillMarketplaceItem[]>([]);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [debouncedKeyword, setDebouncedKeyword] = useState("");
  const [activeCategory, setActiveCategory] = useState("");
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const loadingRef = useRef(false);

  const [installingIds, setInstallingIds] = useState<Set<string>>(new Set());
  const [installedIds, setInstalledIds] = useState<Set<string>>(new Set());

  const handleInstall = useCallback(async (skill: SkillMarketplaceItem) => {
    const id = skill.skill_id;
    setInstallingIds((prev) => new Set(prev).add(id));
    try {
      await skillMarketplaceApi.install({
        source: skill.source_type,
        skill_id: skill.skill_id,
      });
      setInstalledIds((prev) => new Set(prev).add(id));
      toast.success("技能安装已提交");
    } catch (err: any) {
      const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
      toast.error(`安装失败：${msg}`);
    } finally {
      setInstallingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }, []);

  const [mounted, setMounted] = useState(false);

  // debounce keyword
  useEffect(() => {
    setMounted(true);
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => setDebouncedKeyword(keyword), 300);
    return () => clearTimeout(timer);
  }, [keyword]);

  // fetch on keyword/category change (reset)
  useEffect(() => {
    let cancelled = false;
    const fetchItems = async () => {
      setLoading(true);
      try {
        const resp = await skillMarketplaceApi.search({
          keyword: debouncedKeyword || undefined,
          category: activeCategory || undefined,
          limit: PAGE_SIZE,
        });
        if (cancelled) return;
        const newItems = resp.data.data.items ?? [];
        setItems(newItems);
        setHasMore(false);
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
  }, [debouncedKeyword, activeCategory]);

  // load more (scroll trigger)
  const loadMore = useCallback(async () => {
    if (loadingRef.current || !hasMore) return;
    loadingRef.current = true;
    setLoadingMore(true);
    try {
      const resp = await skillMarketplaceApi.search({
        keyword: debouncedKeyword || undefined,
        category: activeCategory || undefined,
        limit: PAGE_SIZE,
      });
      const newItems = resp.data.data.items ?? [];
      if (newItems.length === 0) {
        setHasMore(false);
      } else {
        setItems((prev) => [...prev, ...newItems]);
        setHasMore(false);
      }
    } catch (err) {
      console.error("Failed to load more skills:", err);
    } finally {
      setLoadingMore(false);
      loadingRef.current = false;
    }
  }, [debouncedKeyword, activeCategory, hasMore]);

  // scroll listener
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = container;
      if (scrollHeight - scrollTop - clientHeight < 100) {
        loadMore();
      }
    };

    container.addEventListener("scroll", handleScroll, { passive: true });
    return () => container.removeEventListener("scroll", handleScroll);
  }, [loadMore]);

  return (
    <div
      data-slot="skill-market-view"
      className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
    >
      {/* Header */}
      <div className="flex items-start justify-between border-b border-[var(--leros-control-border)] px-6 py-4">
        <div>
          <h1 className="text-2xl font-bold text-[var(--leros-text-strong)]">
            技能市场
          </h1>
          <p className="mt-1 text-sm text-[var(--leros-text-muted)]">
            探索并部署经过验证的技能，持续增强您的 AI 助手效能。
          </p>
        </div>
        <Button size="sm">
          <Plus className="size-4 mr-1" />
          创作技能
        </Button>
      </div>

      {/* Search + Filters */}
      <div className="flex items-center gap-4 border-b border-[var(--leros-control-border)] px-6 py-3">
        <div className="relative flex-1 max-w-xs">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-[var(--leros-text-subtle)]" />
          <input
            type="text"
            placeholder="搜索技能..."
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            className="w-full rounded-md border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] py-1.5 pl-7 pr-2 text-xs text-[var(--leros-text)] placeholder:text-[var(--leros-text-subtle)] focus:border-[var(--leros-primary)] focus:bg-white focus:outline-none transition-colors"
          />
        </div>
        <div className="flex items-center gap-2 overflow-x-auto no-scrollbar">
          {CATEGORIES.map((cat) => {
            const isActive = activeCategory === cat.value;
            return (
              <button
                type="button"
                key={cat.value}
                onClick={() => setActiveCategory(cat.value)}
                className={cn(
                  "whitespace-nowrap rounded-full border px-3.5 py-1 text-xs font-medium transition-colors shrink-0",
                  isActive
                    ? "border-[var(--leros-primary)] bg-[var(--leros-primary-soft)] text-[var(--leros-primary)]"
                    : "border-[var(--leros-control-border)] bg-transparent text-[var(--leros-text-muted)] hover:border-[var(--leros-text-subtle)] hover:text-[var(--leros-text)]",
                )}
              >
                {cat.label}
              </button>
            );
          })}
          <button
            type="button"
            className="flex items-center gap-1 whitespace-nowrap rounded-full border border-[var(--leros-control-border)] bg-transparent px-3.5 py-1 text-xs font-medium text-[var(--leros-text-muted)] hover:border-[var(--leros-text-subtle)] hover:text-[var(--leros-text)] transition-colors shrink-0"
          >
            <SlidersHorizontal className="size-3" />
            筛选
          </button>
        </div>
      </div>

      {/* Skill grid */}
      <div
        ref={scrollContainerRef}
        className="min-h-0 flex-1 overflow-y-auto px-6 py-8"
      >
        {!mounted || loading ? (
          <div className="flex items-center justify-center py-16 text-sm text-[var(--leros-text-subtle)]">
            加载中...
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-[var(--leros-text-subtle)]">
            <p className="text-sm">暂无符合条件的技能</p>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
              {items.map((skill) => (
                <SkillCard
                  key={skill.skill_id}
                  skill={skill}
                  onInstall={handleInstall}
                  installing={installingIds.has(skill.skill_id)}
                  installed={installedIds.has(skill.skill_id)}
                />
              ))}
            </div>
            {loadingMore && (
              <div className="flex justify-center py-8 text-xs text-[var(--leros-text-subtle)]">
                加载中...
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

// ─── Skill Card ──────────────────────────────────────────────────────────────

function SkillCard({
  skill,
  onInstall,
  installing,
  installed,
}: {
  skill: SkillMarketplaceItem;
  onInstall: (skill: SkillMarketplaceItem) => void;
  installing: boolean;
  installed: boolean;
}) {
  const isLerosAI = skill.author === "LerOS AI";

  return (
    <div
      className={cn(
        "group flex flex-col rounded-xl border border-[var(--leros-control-border)] bg-white p-4 transition-all duration-300",
        "hover:-translate-y-1 hover:border-[var(--leros-primary)] hover:shadow-lg",
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
        <span className="shrink-0 text-[10px] text-[var(--leros-text-subtle)] ml-auto">
          {skill.installs} 安装
        </span>
      </div>

      {/* Bottom: install button */}
      <div className="flex items-center justify-end pt-3 border-t border-[var(--leros-control-border)] h-10">
        <button
          type="button"
          disabled={installing || installed}
          onClick={() => onInstall(skill)}
          className={cn(
            "inline-flex items-center gap-1.5 rounded-lg px-4 py-1 text-xs font-medium transition-all duration-200",
            "opacity-0 translate-y-1 group-hover:opacity-100 group-hover:translate-y-0",
            installed
              ? "bg-green-50 text-green-600 border border-green-200 cursor-default"
              : "bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90",
            (installing || installed) && "opacity-100 translate-y-0",
          )}
        >
          {installing ? (
            <>
              <Loader2 className="size-3 animate-spin" />
              安装中
            </>
          ) : installed ? (
            "已安装"
          ) : (
            "安装技能"
          )}
        </button>
      </div>
    </div>
  );
}
