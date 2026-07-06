"use client";

import { cn } from "@leros/ui/lib/utils";
import { Building2, Contact } from "lucide-react";
import type { ReactNode } from "react";

export type OrgAdminSection = "profile" | "departments";

type OrgAdminLayoutProps = {
	activeSection: OrgAdminSection;
	onNavigate: (section: OrgAdminSection) => void;
	children: ReactNode;
	variant?: "page" | "dialog";
};

const NAV_ITEMS: Array<{ id: OrgAdminSection; label: string; icon: typeof Building2 }> = [
	{ id: "profile", label: "组织管理", icon: Building2 },
	{ id: "departments", label: "部门管理", icon: Contact },
];

export function OrgAdminLayout({
	activeSection,
	onNavigate,
	children,
	variant = "page",
}: OrgAdminLayoutProps) {
	const isDialog = variant === "dialog";

	return (
		<div className="flex h-full min-h-0 flex-1 bg-[var(--leros-surface-soft,#f7f8fd)]">
			<aside
				className={cn(
					"flex shrink-0 flex-col border-r border-[var(--leros-control-border)] bg-[var(--leros-surface)] px-3 py-4",
					isDialog ? "w-[200px]" : "w-[220px] py-5",
				)}
			>
				{!isDialog && (
					<p className="mb-4 px-2 text-xs font-semibold uppercase tracking-wide text-[var(--leros-text-subtle)]">
						组织设置
					</p>
				)}
				<nav className="space-y-1">
					{NAV_ITEMS.map((item) => {
						const Icon = item.icon;
						const active = activeSection === item.id;
						return (
							<button
								key={item.id}
								type="button"
								onClick={() => onNavigate(item.id)}
								className={cn(
									"flex w-full items-center gap-2.5 rounded-xl px-3 py-2.5 text-left text-sm font-medium transition-colors",
									active
										? "bg-[var(--leros-primary-softer)] text-[var(--leros-primary)]"
										: "text-[var(--leros-text-muted)] hover:bg-slate-100 hover:text-[var(--leros-text)]",
								)}
							>
								<Icon className="size-4 shrink-0" />
								<span>{item.label}</span>
							</button>
						);
					})}
				</nav>
			</aside>
			<main
				className={cn(
					"flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto",
					isDialog ? "p-4 md:p-5" : "p-6 md:p-8",
				)}
			>
				{children}
			</main>
		</div>
	);
}
