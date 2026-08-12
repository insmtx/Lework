"use client";

import { useLayoutStore } from "@leros/store";
import { ChevronRight } from "lucide-react";
import { useEffect } from "react";
import { AssistantListView } from "../digitalAssistant/AssistantListView";
import type { AppNavigation } from "../layout/LeftRail";
import { ModelManagementView } from "../system-config/ModelManagementView";
import { DepartmentTreePanel } from "./DepartmentTreePanel";
import { OrgProfilePanel } from "./OrgProfilePanel";

export type OrgAdminSection = "profile" | "departments" | "assistants" | "models";

const SECTION_LABELS: Record<OrgAdminSection, string> = {
	profile: "组织信息管理",
	departments: "通讯录",
	assistants: "AI队友",
	models: "模型管理",
};

const SECTION_TO_VIEW = {
	profile: "orgProfile",
	departments: "orgDepartments",
	assistants: "orgAssistants",
	models: "orgModels",
} as const;

type OrgAdminPageProps = {
	section: OrgAdminSection;
	navigation?: AppNavigation;
};

export function OrgAdminPage({ section, navigation }: OrgAdminPageProps) {
	const switchView = useLayoutStore((s) => s.switchView);

	useEffect(() => {
		// 中文注释：路由直达组织管理页时同步 currentView，保证侧栏处于组织管理模式。
		switchView(SECTION_TO_VIEW[section]);
	}, [section, switchView]);

	const handleBackToWorkbench = () => {
		switchView("workbench");
		if (navigation) {
			navigation.goToRoute("workbench");
		}
	};

	return (
		<div
			data-slot="org-admin-page"
			className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-[var(--leros-surface)]"
		>
			<header className="flex h-12 shrink-0 items-center border-b border-[var(--leros-control-border)] bg-[var(--leros-surface-soft)] px-5">
				<div className="flex min-w-0 items-center gap-3 text-[var(--leros-text-muted)]">
					{/* 中文注释：面包屑首项回到新建任务首页，便于从组织管理快速退出。 */}
					<button
						type="button"
						onClick={handleBackToWorkbench}
						className="text-sm font-normal text-[var(--leros-text-muted)] transition-colors hover:text-[var(--leros-primary)]"
					>
						新建任务
					</button>
					<ChevronRight className="size-4 shrink-0 text-[var(--leros-text-subtle)]" />
					<h1 className="text-sm font-semibold text-[var(--leros-text-strong)]">
						{SECTION_LABELS[section]}
					</h1>
				</div>
			</header>

			<div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
				{section === "profile" ? (
					<div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-10 py-8">
						<OrgProfilePanel />
					</div>
				) : null}
				{section === "departments" ? (
					<div className="flex min-h-0 flex-1 flex-col overflow-hidden">
						<DepartmentTreePanel />
					</div>
				) : null}
				{section === "assistants" ? <AssistantListView navigation={navigation} /> : null}
				{section === "models" ? <ModelManagementView /> : null}
			</div>
		</div>
	);
}
