"use client";

import { SkillDetailView } from "@leros/app-ui";
import { skillMarketplaceApi, useChatStore, useLayoutStore } from "@leros/store";
import { useParams, useRouter } from "next/navigation";
import { useCallback } from "react";
import { toast } from "sonner";

export default function SkillDetailPage() {
	const params = useParams<{ skillId: string }>();
	const router = useRouter();
	const skillId = params.skillId;
	const replaceSkillDirective = useChatStore((s) => s.replaceSkillDirective);
	const { activeProjectId, projects, setProjectRoute } = useLayoutStore((s) => ({
		activeProjectId: s.activeProjectId,
		projects: s.projects,
		setProjectRoute: s.setProjectRoute,
	}));

	const handleUse = useCallback(
		(nextSkillId: string) => {
			const targetProjectId = activeProjectId ?? projects[0]?.id;
			if (!targetProjectId) {
				toast.error("请先创建或选择项目");
				return;
			}

			replaceSkillDirective(nextSkillId);
			setProjectRoute(targetProjectId, "chat");
			router.push(`/projects/${targetProjectId}`);
		},
		[activeProjectId, projects, replaceSkillDirective, router, setProjectRoute],
	);

	const handleUninstall = useCallback(
		async (name: string) => {
			try {
				await skillMarketplaceApi.uninstall({ name });
				toast.success("卸载已提交");
				router.push("/skills");
			} catch (err: any) {
				const msg = err?.response?.data?.message ?? err?.message ?? "未知错误";
				toast.error(`卸载失败：${msg}`);
			}
		},
		[router],
	);

	return (
		<SkillDetailView
			skillId={skillId}
			onBack={() => router.push("/skills")}
			onSkillClick={(id) => router.push(`/skills/${id}`)}
			onUse={handleUse}
			onUninstall={handleUninstall}
		/>
	);
}
