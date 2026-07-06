"use client";

import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { useEffect, useState } from "react";
import { DepartmentTreePanel } from "./DepartmentTreePanel";
import { OrgAdminLayout, type OrgAdminSection } from "./OrgAdminLayout";
import { OrgProfilePanel } from "./OrgProfilePanel";

type OrgAdminDialogProps = {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	defaultSection?: OrgAdminSection;
};

export function OrgAdminDialog({
	open,
	onOpenChange,
	defaultSection = "profile",
}: OrgAdminDialogProps) {
	const [section, setSection] = useState<OrgAdminSection>(defaultSection);

	useEffect(() => {
		if (open) {
			setSection(defaultSection);
		}
	}, [defaultSection, open]);

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				className="flex h-[min(820px,85vh)] w-[min(1040px,95vw)] max-w-none flex-col gap-0 overflow-hidden p-0"
				showCloseButton
			>
				<DialogHeader className="shrink-0 border-b border-[var(--leros-control-border)] px-6 py-4 text-left">
					<DialogTitle>组织设置</DialogTitle>
					<DialogDescription>管理组织基本信息与部门结构</DialogDescription>
				</DialogHeader>
				<div className="flex min-h-0 flex-1 flex-col">
					<OrgAdminLayout activeSection={section} onNavigate={setSection} variant="dialog">
						{section === "profile" ? <OrgProfilePanel compact /> : <DepartmentTreePanel compact />}
					</OrgAdminLayout>
				</div>
			</DialogContent>
		</Dialog>
	);
}
