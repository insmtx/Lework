"use client";

import type { SkillMarketplaceItem } from "@leros/store";
import { useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@leros/ui/components/ui/tabs";
import { Import, Plus, Search } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { useAuth } from "../auth";
import type { AppNavigation } from "../layout";
import { navigateToNewTask } from "../layout/new-task-navigation";
import { buildSkillNewTaskPrefill } from "../layout/new-task-prefill";
import { useBrandIdentity } from "../private-deployment/useBrandIdentity";
import { MarketplacePanel } from "./MarketplacePanel";
import { McpConnectorPanel } from "./McpConnectorPanel";
import { MySkillsPanel } from "./MySkillsPanel";
import { SkillDetailView } from "./SkillDetailView";
import { SkillImportDialog } from "./SkillImportDialog";

type PluginTab = "mcp" | "skills";
type SkillSourceTab = "marketplace" | "mine" | "owned";

const PLUGIN_TAB_CLASS =
	"!h-full flex-none rounded-none border-x-0 border-t-0 border-b-2 border-b-transparent bg-transparent px-1 text-sm font-medium leading-none text-[var(--leros-text-muted)] shadow-none after:hidden hover:text-[var(--leros-text-strong)] data-active:!border-b-[var(--leros-primary)] data-active:!bg-transparent data-active:!text-[var(--leros-text-strong)] data-active:!shadow-none";

const SKILL_SOURCE_TAB_CLASS =
	"h-8 rounded-md border border-[var(--leros-control-border)] bg-white px-3 text-xs font-medium text-[var(--leros-text-muted)] shadow-none hover:text-[var(--leros-text-strong)] data-active:border-[var(--leros-primary)] data-active:bg-[var(--leros-primary-soft)] data-active:text-[var(--leros-primary)]";

export function SkillMarketView({ navigation }: { navigation?: AppNavigation }) {
	const { name: brandName } = useBrandIdentity();
	const { isAuthenticated, requireAuth } = useAuth();
	const [pluginTab, setPluginTab] = useState<PluginTab>("skills");
	const [skillSourceTab, setSkillSourceTab] = useState<SkillSourceTab>("marketplace");
	const [selectedSkillId, setSelectedSkillId] = useState<string | null>(null);
	const [selectedSource, setSelectedSource] = useState<"official" | "organization">("official");
	const [importDialogOpen, setImportDialogOpen] = useState(false);
	const [mySkillsRefreshSeq, setMySkillsRefreshSeq] = useState(0);
	const [ownedSkillsRefreshSeq, setOwnedSkillsRefreshSeq] = useState(0);
	const [marketplaceRefreshSeq, setMarketplaceRefreshSeq] = useState(0);
	const [keyword, setKeyword] = useState("");
	const [debouncedKeyword, setDebouncedKeyword] = useState("");
	const [marketplaceCount, setMarketplaceCount] = useState(0);
	const [mySkillsCount, setMySkillsCount] = useState(0);
	const [ownedSkillsCount, setOwnedSkillsCount] = useState(0);
	const { setWorkbenchComposerPrefill, selectWorkbenchProject, selectWorkbenchTask, switchView } =
		useLayoutStore((state) => state);

	useEffect(() => {
		const timer = window.setTimeout(() => setDebouncedKeyword(keyword.trim()), 300);
		return () => window.clearTimeout(timer);
	}, [keyword]);

	const goUseSkill = useCallback(
		(skillCode: string, displayName?: string): boolean => {
			const prefill = buildSkillNewTaskPrefill(skillCode, undefined, displayName);
			selectWorkbenchProject(null);
			selectWorkbenchTask(null);
			setWorkbenchComposerPrefill(prefill);
			navigateToNewTask(navigation, switchView);
			return true;
		},
		[
			navigation,
			selectWorkbenchProject,
			selectWorkbenchTask,
			setWorkbenchComposerPrefill,
			switchView,
		],
	);

	const handleCardClick = useCallback((skill: SkillMarketplaceItem) => {
		setSelectedSkillId(skill.skill_id);
		setSelectedSource(skill.source_type === "organization" ? "organization" : "official");
	}, []);

	const handleCardUse = useCallback(
		(skill: SkillMarketplaceItem) => {
			goUseSkill(skill.name, skill.display_name);
		},
		[goUseSkill],
	);

	const handleBack = useCallback(() => {
		setSelectedSkillId(null);
	}, []);

	const handleDetailUse = useCallback(
		(skillCode: string, displayLabel?: string) => {
			goUseSkill(skillCode, displayLabel);
		},
		[goUseSkill],
	);

	const openImportDialog = useCallback(() => {
		requireAuth(() => setImportDialogOpen(true));
	}, [requireAuth]);

	const openCreateSkill = useCallback(() => {
		requireAuth(() => {
			const prefill = buildSkillNewTaskPrefill(
				"skill-creator",
				"请创建一个用于「XXXXXX」的技能。",
			);
			selectWorkbenchProject(null);
			selectWorkbenchTask(null);
			setWorkbenchComposerPrefill(prefill);
			navigateToNewTask(navigation, switchView);
		});
	}, [
		requireAuth,
		navigation,
		selectWorkbenchProject,
		selectWorkbenchTask,
		setWorkbenchComposerPrefill,
		switchView,
	]);

	const handleSkillSourceChange = useCallback(
		(nextTab: string) => {
			if (nextTab === "mine") {
				requireAuth(() => {
					setSkillSourceTab("mine");
					setMySkillsRefreshSeq((sequence) => sequence + 1);
				});
				return;
			}
			if (nextTab === "owned") {
				requireAuth(() => {
					setSkillSourceTab("owned");
					setOwnedSkillsRefreshSeq((sequence) => sequence + 1);
				});
				return;
			}
			setSkillSourceTab("marketplace");
		},
		[requireAuth],
	);

	useEffect(() => {
		if (isAuthenticated) return;
		setSkillSourceTab("marketplace");
		setImportDialogOpen(false);
	}, [isAuthenticated]);

	const handleImportSuccess = useCallback(() => {
		setMySkillsRefreshSeq((sequence) => sequence + 1);
		setOwnedSkillsRefreshSeq((sequence) => sequence + 1);
		setMarketplaceRefreshSeq((sequence) => sequence + 1);
		setSkillSourceTab("owned");
	}, []);

	const handleOfficialInstall = useCallback(() => {
		setMySkillsRefreshSeq((sequence) => sequence + 1);
		setOwnedSkillsRefreshSeq((sequence) => sequence + 1);
		setMarketplaceRefreshSeq((sequence) => sequence + 1);
	}, []);

	if (selectedSkillId) {
		return (
			<div
				data-slot="skill-market-view"
				className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]"
			>
				<SkillDetailView
					skillId={selectedSkillId}
					source={selectedSource}
					onBack={handleBack}
					onUse={handleDetailUse}
					onOfficialInstalled={handleOfficialInstall}
				/>
			</div>
		);
	}

	const activeCount =
		skillSourceTab === "marketplace"
			? marketplaceCount
			: skillSourceTab === "owned"
				? ownedSkillsCount
				: mySkillsCount;
	const activeSectionTitle =
		skillSourceTab === "marketplace"
			? "技能市场"
			: skillSourceTab === "owned"
				? "我的"
				: "组织共享";
	const activeSectionDescription =
		skillSourceTab === "marketplace"
			? `由 ${brandName} 统一维护，未启用的技能将在首次使用时自动准备。`
			: skillSourceTab === "owned"
				? "由你拥有和管理的技能，可配置公开范围和协作权限。"
				: "组织成员创作或导入并共享使用的技能。";

	return (
		<div
			data-slot="skill-market-view"
			className="flex h-full min-h-0 flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<Tabs
				value={pluginTab}
				onValueChange={(value) => setPluginTab(value as PluginTab)}
				className="flex min-h-0 flex-1 flex-col"
			>
				<header className="grid h-14 shrink-0 grid-cols-[1fr_auto_1fr] items-stretch border-b border-[var(--leros-control-border)] bg-white px-5">
					<div className="flex items-center gap-2.5">
						<h1 className="text-lg font-semibold tracking-tight text-[var(--leros-text-strong)]">
							插件
						</h1>
						<p className="text-xs text-[var(--leros-text-muted)]">统一管理连接器和技能</p>
					</div>
					<TabsList
						variant="line"
						className="!h-full self-stretch gap-6 rounded-none bg-transparent p-0"
					>
						<TabsTrigger value="skills" className={PLUGIN_TAB_CLASS}>
							技能库
						</TabsTrigger>
						<TabsTrigger value="mcp" className={PLUGIN_TAB_CLASS}>
							MCP 连接器
						</TabsTrigger>
					</TabsList>
					<div aria-hidden="true" />
				</header>

				<TabsContent value="mcp" className="flex min-h-0 flex-1 flex-col outline-none">
					<McpConnectorPanel isAuthenticated={isAuthenticated} />
				</TabsContent>

				<TabsContent value="skills" className="flex min-h-0 flex-1 flex-col outline-none">
					<Tabs
						value={skillSourceTab}
						onValueChange={handleSkillSourceChange}
						className="flex min-h-0 flex-1 flex-col"
					>
						<section className="shrink-0 border-b border-[var(--leros-control-border)] bg-white px-5 pt-5">
							<div className="mx-auto w-full max-w-[1480px]">
								<div>
									<h2 className="text-xl font-semibold tracking-tight text-[var(--leros-text-strong)]">
										技能库
									</h2>
									<p className="mt-1 text-xs text-[var(--leros-text-muted)]">
										市场技能可直接使用；组织技能由成员共同维护和复用。
									</p>
								</div>

								<div className="mt-4 flex flex-wrap items-center gap-2 pb-4">
									<div className="relative w-[420px] min-w-[260px] max-w-full flex-none">
										<Search className="absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-[var(--leros-text-subtle)]" />
										<input
											type="search"
											aria-label="搜索技能"
											placeholder="搜索技能名称、说明"
											value={keyword}
											onChange={(event) => setKeyword(event.target.value)}
											className="h-9 w-full rounded-md border border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] pl-9 pr-3 text-xs text-[var(--leros-text)] outline-none transition-colors placeholder:text-[var(--leros-text-subtle)] focus:border-[var(--leros-primary)] focus:bg-white"
										/>
									</div>

									<TabsList className="h-auto shrink-0 gap-2 rounded-none bg-transparent p-0">
										<TabsTrigger value="marketplace" className={SKILL_SOURCE_TAB_CLASS}>
											技能市场 <span className="ml-1.5 opacity-60">{marketplaceCount}</span>
										</TabsTrigger>
										<TabsTrigger value="mine" className={SKILL_SOURCE_TAB_CLASS}>
											组织共享 <span className="ml-1.5 opacity-60">{mySkillsCount}</span>
										</TabsTrigger>
										<TabsTrigger value="owned" className={SKILL_SOURCE_TAB_CLASS}>
											我的 <span className="ml-1.5 opacity-60">{ownedSkillsCount}</span>
										</TabsTrigger>
									</TabsList>

									<div className="ml-auto flex shrink-0 items-center gap-2">
										<Button
											variant="outline"
											className="h-8 rounded-md px-3 text-xs"
											onClick={openImportDialog}
										>
											<Import className="mr-1.5 size-3.5" />
											导入技能
										</Button>
										<Button
											className="h-8 rounded-md bg-[var(--leros-primary)] px-3 text-xs text-white hover:opacity-90"
											onClick={openCreateSkill}
										>
											<Plus className="mr-1.5 size-3.5" />
											创建技能
										</Button>
									</div>
								</div>
							</div>
						</section>

						<section className="min-h-0 flex-1 overflow-y-auto bg-[var(--leros-surface-soft)] px-5 py-4">
							<div className="mx-auto w-full max-w-[1480px]">
								<div className="mb-3 flex items-start justify-between gap-4">
									<div className="flex items-baseline gap-2.5">
										<h3 className="text-sm font-semibold text-[var(--leros-text-strong)]">
											{activeSectionTitle}
										</h3>
										<p className="text-xs text-[var(--leros-text-muted)]">
											{activeSectionDescription}
										</p>
									</div>
									<span className="shrink-0 text-xs text-[var(--leros-text-subtle)]">
										共 {activeCount} 个
									</span>
								</div>

								<TabsContent value="marketplace" keepMounted className="outline-none">
									<MarketplacePanel
										isAuthenticated={isAuthenticated}
										onCardClick={handleCardClick}
										onUse={handleCardUse}
										refreshSeq={marketplaceRefreshSeq}
										keyword={debouncedKeyword}
										onCountChange={setMarketplaceCount}
									/>
								</TabsContent>

								<TabsContent value="mine" keepMounted={isAuthenticated} className="outline-none">
									<MySkillsPanel
										onCardClick={handleCardClick}
										onUse={handleCardUse}
										refreshSeq={mySkillsRefreshSeq}
										keyword={debouncedKeyword}
										relation="shared"
										excludeMarketplaceBased
										onCountChange={setMySkillsCount}
									/>
								</TabsContent>

								<TabsContent value="owned" keepMounted={isAuthenticated} className="outline-none">
									<MySkillsPanel
										onCardClick={handleCardClick}
										onUse={handleCardUse}
										refreshSeq={ownedSkillsRefreshSeq}
										keyword={debouncedKeyword}
										relation="owner"
										cardVariant="owned"
										emptyMessage="你还没有拥有的技能"
										onCountChange={setOwnedSkillsCount}
									/>
								</TabsContent>
							</div>
						</section>
					</Tabs>
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
