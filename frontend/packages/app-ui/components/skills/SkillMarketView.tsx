"use client";

import type { SkillMarketplaceItem } from "@leros/store";
import { skillMarketplaceApi, useChatStore, useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@leros/ui/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { ChevronDown, Import, Plus } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";
import type { AppNavigation } from "../layout";
import { MarketplacePanel } from "./MarketplacePanel";
import { MySkillsPanel } from "./MySkillsPanel";
import { SkillDetailView } from "./SkillDetailView";
import { SkillImportDialog } from "./SkillImportDialog";
import { RecentSkillsPanel } from "./RecentSkillsPanel";

/** Explicit leros colors avoid oklch opacity modifiers that render incorrectly on Windows. */
const SKILL_MARKET_TAB_TRIGGER_CLASS =
	"text-xl font-bold text-[var(--leros-text-muted)] hover:text-[var(--leros-text-strong)] data-active:bg-transparent data-active:text-[var(--leros-text-strong)] dark:text-[var(--leros-text-muted)] dark:hover:text-[var(--leros-text-strong)] dark:data-active:bg-transparent dark:data-active:text-[var(--leros-text-strong)]";

export function SkillMarketView({ navigation }: { navigation?: AppNavigation }) {
	const [activeTab, setActiveTab] = useState<"marketplace" | "mine">("marketplace");
	const [selectedSkillId, setSelectedSkillId] = useState<string | null>(null);
	const [selectedSource, setSelectedSource] = useState<string>("Leros");
	const [selectedVersion, setSelectedVersion] = useState<string | undefined>(undefined);
	const [importDialogOpen, setImportDialogOpen] = useState(false);
	const [mySkillsRefreshSeq, setMySkillsRefreshSeq] = useState(0);
	const replaceSkillDirective = useChatStore((s) => s.replaceSkillDirective);
	const { activeProjectId, projects, setProjectRoute } = useLayoutStore((s) => ({
		activeProjectId: s.activeProjectId,
		projects: s.projects,
		setProjectRoute: s.setProjectRoute,
	}));

	const goUseSkill = useCallback(
		(skillId: string): boolean => {
			const targetProjectId = activeProjectId ?? projects[0]?.id;
			if (!targetProjectId) {
				toast.error("请先创建或选择项目");
				return false;
			}

			replaceSkillDirective(skillId);
			setProjectRoute(targetProjectId, "chat");
			navigation?.goToProject(targetProjectId);
			return true;
		},
		[activeProjectId, navigation, projects, replaceSkillDirective, setProjectRoute],
	);

	const handleCardClick = useCallback(
		(skill: SkillMarketplaceItem) => {
			setSelectedSkillId(skill.skill_id);
			setSelectedSource(activeTab === "mine" ? "installed" : skill.source_type || "Leros");
			setSelectedVersion(skill.version || undefined);
		},
		[activeTab],
	);

	const handleBack = useCallback(() => {
		setSelectedSkillId(null);
	}, []);

	const handleSkillClick = useCallback((skillId: string, sourceType?: string) => {
		setSelectedSkillId(skillId);
		setSelectedSource(sourceType || "Leros");
		setSelectedVersion(undefined);
	}, []);

	const handleUse = useCallback(
		(skillId: string) => {
			if (!goUseSkill(skillId)) return;
			setSelectedSkillId(null);
			setActiveTab("mine");
		},
		[goUseSkill],
	);

	const handleUninstallFromDetail = useCallback(async (name: string) => {
		try {
			await skillMarketplaceApi.uninstall({ name });
			toast.success("卸载已提交");
			setSelectedSkillId(null);
		} catch (err: any) {
			const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
			toast.error(`卸载失败：${msg}`);
		}
	}, []);

	const handleImportSuccess = useCallback(() => {
		setMySkillsRefreshSeq((seq) => seq + 1);
	}, []);

	// Show detail view when a skill is selected
	if (selectedSkillId) {
		return (
			<div
				data-slot="skill-market-view"
				className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
			>
				<SkillDetailView
					skillId={selectedSkillId}
					source={selectedSource}
					version={selectedVersion}
					onBack={handleBack}
					onSkillClick={handleSkillClick}
					onUse={handleUse}
					onUninstall={handleUninstallFromDetail}
				/>
			</div>
		);
	}

	return (
		<div
			data-slot="skill-market-view"
			className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<Tabs
				value={activeTab}
				onValueChange={(v) => setActiveTab(v as "marketplace" | "mine")}
				className="min-h-0 flex-1 flex-col"
			>
				<div className="flex items-start justify-between border-b border-[var(--leros-control-border)] px-6 py-4">
					<div>
						<TabsList variant="line" className="mb-3 -ml-1.5">
							<TabsTrigger value="marketplace" className={SKILL_MARKET_TAB_TRIGGER_CLASS}>
								技能市场
							</TabsTrigger>
							<TabsTrigger value="mine" className={SKILL_MARKET_TAB_TRIGGER_CLASS}>
								我的技能
							</TabsTrigger>
						</TabsList>
						<p className="text-sm text-[var(--leros-text-muted)]">
							{activeTab === "mine"
								? "您已安装并正在使用的技能。"
								: "探索并部署经过验证的技能，持续增强您的 AI 助手效能。"}
						</p>
					</div>
					<DropdownMenu>
						<DropdownMenuTrigger
							render={(props) => (
								<Button size="sm" {...props}>
									<Plus className="size-4 mr-1" />
									创作技能
									<ChevronDown className="size-3 ml-1 opacity-60" />
								</Button>
							)}
						/>
						<DropdownMenuContent align="end" className="w-36">
							<DropdownMenuItem onClick={() => goUseSkill("skill-creator")}>
								<Plus className="size-4 mr-2" />
								创作技能
							</DropdownMenuItem>
							<DropdownMenuItem onClick={() => setImportDialogOpen(true)}>
								<Import className="size-4 mr-2" />
								导入技能
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>

				<TabsContent value="marketplace" className="flex min-h-0 flex-1 flex-col outline-none">
					<MarketplacePanel onCardClick={handleCardClick} />
				</TabsContent>

				<TabsContent value="mine" className="min-h-0 flex-1 overflow-y-auto px-6 py-8 outline-none">
					<div className="flex flex-col gap-8">
						<RecentSkillsPanel onCardClick={handleCardClick} />
						<div>
							<h3 className="text-sm font-semibold text-[var(--leros-text-strong)] mb-4">已安装</h3>
							<MySkillsPanel onCardClick={handleCardClick} refreshSeq={mySkillsRefreshSeq} />
						</div>
					</div>
				</TabsContent>
			</Tabs>

			<SkillImportDialog
				open={importDialogOpen}
				onOpenChange={setImportDialogOpen}
				onImportSuccess={handleImportSuccess}
			/>
		</div>
	);
}
