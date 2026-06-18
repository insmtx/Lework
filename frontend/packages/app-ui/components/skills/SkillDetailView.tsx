"use client";

import { Button } from "@leros/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { cn } from "@leros/ui/lib/utils";
import type { SkillDetailData, SkillMarketplaceItem } from "@leros/store";
import { skillMarketplaceApi } from "@leros/store";
import {
  ArrowLeft,
  Download,
  Ellipsis,
  Loader2,
  RefreshCw,
  Star,
  CheckCircle,
  Trash2,
  Verified,
  FileText,
  Calendar,
  FolderOpen,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { MarkdownRenderer } from "../common/MarkdownRenderer";

interface SkillDetailViewProps {
  skillId: string;
  /** Which source to query — "Leros" for marketplace, "installed" for installed skills */
  source?: string;
  /** Called when the user wants to navigate back to the marketplace */
  onBack?: () => void;
  /** Called when a related skill card is clicked */
  onSkillClick?: (skillId: string, sourceType?: string) => void;
  /** Called when user clicks "去使用" for an installed skill */
  onUse?: (skillId: string) => void;
  /** Called when user clicks "卸载" from the dropdown menu */
  onUninstall?: (name: string) => void;
}

export function SkillDetailView({
  skillId,
  source = "Leros",
  onBack,
  onSkillClick,
  onUse,
  onUninstall,
}: SkillDetailViewProps) {
  const [skill, setSkill] = useState<SkillDetailData | null>(null);
  const [relatedSkills, setRelatedSkills] = useState<SkillMarketplaceItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState(false);
  const [activeTab, setActiveTab] = useState("overview");
  const [mounted, setMounted] = useState(false);

  // Gate fetch on mounted to avoid StrictMode double-fire
  useEffect(() => {
    setMounted(true);
  }, []);

  const fetchSkill = useCallback(async () => {
    setLoading(true);
    setError(null);
    let cancelled = false;
    try {
      // Fetch skill detail via the dedicated API
      const resp = await skillMarketplaceApi.getDetail({
        source,
        skill_id: skillId,
      });
      if (cancelled) return;
      const detail = resp.data.data;
      setSkill(detail);

      // For installed skills, they're already installed
      if (detail.source === "installed") {
        setInstalled(true);
      }

      // Fetch related skills from the marketplace (only for marketplace skills)
      if (detail.category) {
        try {
          const relatedResp = await skillMarketplaceApi.search({
            category: detail.category,
            limit: 5,
          });
          if (cancelled) return;
          const relatedItems = (relatedResp.data.data.items ?? []).filter(
            (item) => item.skill_id !== detail.skill_id,
          );
          setRelatedSkills(relatedItems.slice(0, 4));
        } catch {
          // Related skills are non-critical; silently ignore failures
        }
      }
    } catch (err: any) {
      if (cancelled) return;
      const msg = err?.response?.data?.message ?? err?.message ?? "加载失败";
      setError(msg);
    } finally {
      if (!cancelled) setLoading(false);
    }
  }, [skillId, source]);

  useEffect(() => {
    if (!mounted) return;
    fetchSkill();
  }, [mounted, fetchSkill]);

  const handleInstall = useCallback(async () => {
    if (!skill) return;
    setInstalling(true);
    try {
      await skillMarketplaceApi.install({
        source: skill.source_type,
        skill_id: skill.skill_id,
      });
      setInstalled(true);
      toast.success("技能安装已提交");
    } catch (err: any) {
      const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
      toast.error(`安装失败：${msg}`);
    } finally {
      setInstalling(false);
    }
  }, [skill]);

  // Convert SkillDetailData to a card-compatible shape for related-skill badges
  const isLerosOfficial = skill?.verified && skill?.source_type === "Leros";

  // Loading state
  if (loading) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center bg-[var(--leros-app-bg)]">
        <div className="flex flex-col items-center gap-3 text-[var(--leros-text-subtle)]">
          <Loader2 className="size-6 animate-spin" />
          <span className="text-sm">加载技能详情...</span>
        </div>
      </div>
    );
  }

  // Error state
  if (error || !skill) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center bg-[var(--leros-app-bg)]">
        <div className="flex flex-col items-center gap-4 text-[var(--leros-text-subtle)]">
          <p className="text-sm">{error ?? "技能不存在"}</p>
          <div className="flex gap-2">
            {onBack && (
              <button
                type="button"
                onClick={onBack}
                className="inline-flex items-center gap-1.5 rounded-md border border-[var(--leros-control-border)] px-3 py-1.5 text-xs text-[var(--leros-text-muted)] hover:bg-[var(--leros-surface-soft)] transition-colors"
              >
                <ArrowLeft className="size-3.5" />
                返回市场
              </button>
            )}
            <button
              type="button"
              onClick={fetchSkill}
              className="rounded-md border border-[var(--leros-control-border)] px-3 py-1.5 text-xs text-[var(--leros-primary)] hover:bg-[var(--leros-primary-soft)] transition-colors"
            >
              重试
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)] overflow-y-auto">
      {/* Top section: back + header + metrics (full width) */}
      <div className="px-16 pt-4">
        {/* Back button */}
        {onBack && (
          <button
            type="button"
            onClick={onBack}
            className="inline-flex items-center gap-1 text-xs text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)] transition-colors mb-4"
          >
            <ArrowLeft className="size-3.5" />
            返回
          </button>
        )}

        {/* Skill Header */}
        <div className="flex items-start justify-between mb-5">
          <div className="flex gap-4">
            {/* Icon */}
            {skill.icon ? (
              <img
                src={skill.icon}
                alt={skill.name}
                className="h-14 w-14 shrink-0 rounded-xl object-cover shadow-sm"
              />
            ) : (
              <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-[var(--leros-primary-soft)] text-[var(--leros-primary)] shadow-sm">
                <span className="text-[28px] font-bold">
                  {skill.name.charAt(0).toUpperCase()}
                </span>
              </div>
            )}
            <div>
              <h1 className="text-xl font-bold leading-tight text-[var(--leros-text-strong)] mb-1">
                {skill.name}
              </h1>
              {skill.category && (
                <span className="inline-flex px-2 py-0.5 rounded bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] text-[11px] font-medium border border-[var(--leros-control-border)]">
                  {skill.category}
                </span>
              )}
              {skill.description && (
                <p className="mt-2 text-xs leading-relaxed text-[var(--leros-text-muted)] max-w-2xl">
                  {skill.description}
                </p>
              )}
            </div>
          </div>

          {/* Action buttons — install for marketplace, use+menu for installed */}
          {skill.source === "installed" ? (
            <div className="flex items-center gap-1.5">
              <Button
                size="sm"
                onClick={() => onUse?.(skill.skill_id)}
                className="rounded-lg px-4 py-2 text-xs font-medium shadow-sm bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90 hover:shadow-md transition-all"
              >
                去使用
              </Button>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={(props) => (
                    <Button
                      size="sm"
                      variant="ghost"
                      {...props}
                      className="rounded-lg px-2 py-2 hover:bg-[var(--leros-surface-soft)]"
                    >
                      <Ellipsis className="size-4" />
                    </Button>
                  )}
                />
                <DropdownMenuContent align="end" className="w-32">
                  <DropdownMenuItem
                    onClick={() => onUninstall?.(skill.skill_id)}
                    className="text-xs text-red-600 focus:text-red-600"
                  >
                    <Trash2 className="size-3.5 mr-2" />
                    卸载
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          ) : (
            <Button
              size="sm"
              onClick={handleInstall}
              disabled={installing || installed}
              className={cn(
                "rounded-lg px-4 py-2 text-xs font-medium shadow-sm transition-all",
                installed
                  ? "bg-green-50 text-green-600 border border-green-200 hover:bg-green-50"
                  : "bg-[var(--leros-primary)] text-white hover:bg-[var(--leros-primary)]/90 hover:shadow-md",
              )}
            >
              {installing ? (
                <>
                  <Loader2 className="size-3.5 mr-1.5 animate-spin" />
                  安装中...
                </>
              ) : installed ? (
                <>
                  <CheckCircle className="size-3.5 mr-1.5" />
                  已安装
                </>
              ) : (
                <>
                  <Download className="size-3.5 mr-1.5" />
                  安装技能
                </>
              )}
            </Button>
          )}
        </div>

        {/* Metrics Banner */}
        <div className="flex items-center gap-6 mb-4 py-0.5">
          <div className="flex items-center gap-1.5 text-[var(--leros-text-muted)] text-xs">
            <Download className="size-3.5" />
            <span>{skill.installs ? `${(skill.installs / 1000).toFixed(1)}K` : "—"} 下载</span>
          </div>
          <div className="flex items-center gap-1.5 text-[var(--leros-text-muted)] text-xs">
            <Star
              className="size-3.5 text-[var(--leros-primary)]"
              fill="currentColor"
            />
            <span>4.9 评分</span>
          </div>
          {isLerosOfficial && (
            <div className="flex items-center gap-1.5 text-[var(--leros-text-muted)] text-xs">
              <Verified className="size-3.5" />
              <span>官方认证</span>
            </div>
          )}
        </div>
      </div>

      {/* Bottom section: tabs + sidebar side by side */}
      <div className="flex min-h-0 flex-1">
        {/* Left: tabbed content */}
        <div className="flex-1 min-w-0 px-16">
          <Tabs
            value={activeTab}
            onValueChange={setActiveTab}
            className="w-full"
          >
            <div className="border-b border-[var(--leros-control-border)] mb-5">
              <TabsList variant="line" className="gap-6">
                <TabsTrigger
                  value="overview"
                  className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
                >
                  概述
                </TabsTrigger>
                <TabsTrigger
                  value="files"
                  className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
                >
                  文件
                </TabsTrigger>
                <TabsTrigger
                  value="versions"
                  className="pb-3 text-xs font-medium data-active:text-[var(--leros-primary)]"
                >
                  历史版本
                </TabsTrigger>
              </TabsList>
            </div>

            {/* Overview Tab — markdown-rendered SKILL.md body */}
            <TabsContent value="overview" className="outline-none">
              {skill.skill_md ? (
                <MarkdownRenderer
                  content={skill.skill_md}
                  className="prose prose-sm max-w-none prose-headings:text-[var(--leros-text-strong)] prose-p:text-xs prose-p:leading-relaxed prose-p:text-[var(--leros-text-muted)] prose-code:text-[11px] prose-pre:rounded-lg prose-pre:bg-[var(--leros-surface-soft)]"
                />
              ) : (
                <div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
                  <FileText className="size-6 mb-2 opacity-40" />
                  <p className="text-xs">暂无概述内容</p>
                </div>
              )}
            </TabsContent>

            {/* Files Tab */}
            <TabsContent value="files" className="outline-none">
              {skill.files && skill.files.length > 0 ? (
                <ul className="space-y-1">
                  {skill.files.map((file) => (
                    <li
                      key={file}
                      className="flex items-center gap-2 rounded-md px-3 py-2 text-xs text-[var(--leros-text-muted)] hover:bg-[var(--leros-surface-soft)] transition-colors"
                    >
                      <FileText className="size-3.5 shrink-0 text-[var(--leros-text-subtle)]" />
                      <span className="font-mono">{file}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
                  <FolderOpen className="size-6 mb-2 opacity-40" />
                  <p className="text-xs">暂无文件</p>
                  <p className="text-[11px] mt-1">安装后可查看技能包含的所有文件</p>
                </div>
              )}
            </TabsContent>

            {/* Versions Tab */}
            <TabsContent value="versions" className="outline-none">
              <div className="flex flex-col items-center justify-center py-10 text-[var(--leros-text-subtle)]">
                <Calendar className="size-6 mb-2 opacity-40" />
                <p className="text-xs">版本历史</p>
                {skill.version && (
                  <span className="mt-2 inline-flex items-center rounded-md border border-[var(--leros-control-border)] px-2.5 py-0.5 text-[11px] font-medium text-[var(--leros-text-muted)]">
                    当前版本: v{skill.version}
                  </span>
                )}
              </div>
            </TabsContent>

          </Tabs>
        </div>

        {/* Right Sidebar — top aligns with tab bar, no left border */}
        <aside className="w-64 shrink-0 flex flex-col gap-4 px-12 py-3">
          {/* Related Skills */}
          {relatedSkills.length > 0 && (
            <section>
              <div className="flex items-center justify-between mb-3">
                <h5 className="text-[11px] font-semibold uppercase tracking-wider text-[var(--leros-text-subtle)]">
                  相关推荐
                </h5>
                <button
                  type="button"
                  onClick={fetchSkill}
                  className="text-[var(--leros-text-subtle)] hover:text-[var(--leros-text-muted)] transition-colors"
                  title="刷新"
                >
                  <RefreshCw className="size-3.5" />
                </button>
              </div>
              <div className="space-y-2.5">
                {relatedSkills.map((related) => (
                  <button
                    type="button"
                    key={related.skill_id}
                    onClick={() => onSkillClick?.(related.skill_id, related.source_type)}
                    className="w-full text-left p-3 bg-[var(--leros-surface)] border border-[var(--leros-control-border)] rounded-xl hover:border-[var(--leros-primary)]/50 transition-all cursor-pointer group"
                  >
                    <div className="flex gap-2.5">
                      {related.icon ? (
                        <img
                          src={related.icon}
                          alt={related.name}
                          className="h-8 w-8 shrink-0 rounded-lg object-cover"
                        />
                      ) : (
                        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-[var(--leros-surface-soft)] text-[var(--leros-text-muted)] group-hover:bg-[var(--leros-primary-soft)] group-hover:text-[var(--leros-primary)] transition-colors">
                          <span className="text-sm font-bold">
                            {related.name.charAt(0).toUpperCase()}
                          </span>
                        </div>
                      )}
                      <div className="min-w-0">
                        <h6 className="text-xs font-semibold text-[var(--leros-text-strong)] truncate">
                          {related.name}
                        </h6>
                        <div className="flex items-center gap-1.5 mt-0.5 text-[10px] text-[var(--leros-text-subtle)]">
                          <span className="flex items-center gap-0.5">
                            <Star className="size-2.5 fill-amber-500 text-amber-500" />
                            4.5
                          </span>
                          <span>•</span>
                          <span>
                            {related.installs >= 1000
                              ? `${(related.installs / 1000).toFixed(1)}K`
                              : related.installs}{" "}
                            次安装
                          </span>
                        </div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </section>
          )}

          {/* Technical Meta Info */}
          <section className="bg-[var(--leros-surface-soft)]/50 p-4 rounded-xl border border-dashed border-[var(--leros-control-border)]">
            <h5 className="text-[11px] font-semibold uppercase tracking-wider text-[var(--leros-text-subtle)] mb-3">
              元数据
            </h5>
            <div className="space-y-2 text-[11px]">
              <div className="flex justify-between">
                <span className="text-[var(--leros-text-subtle)]">版本</span>
                <span className="text-[var(--leros-text-strong)] font-medium">
                  {skill.version ? `v${skill.version}` : "—"}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-[var(--leros-text-subtle)]">类型</span>
                <span className="text-[var(--leros-text-strong)] font-medium">
                  {skill.verified ? "官方核心技能" : "社区技能"}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-[var(--leros-text-subtle)]">来源</span>
                <span className="text-[var(--leros-text-strong)] font-medium capitalize">
                  {skill.source_type}
                </span>
              </div>
              <div className="flex justify-between">
                <span className="text-[var(--leros-text-subtle)]">分类</span>
                <span className="text-[var(--leros-text-strong)] font-medium">
                  {skill.category || "—"}
                </span>
              </div>
              {skill.author && (
                <div className="flex justify-between">
                  <span className="text-[var(--leros-text-subtle)]">作者</span>
                  <span className="text-[var(--leros-text-strong)] font-medium">
                    {skill.author}
                  </span>
                </div>
              )}
            </div>
          </section>
        </aside>
      </div>
    </div>
  );
}
