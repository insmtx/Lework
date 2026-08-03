"use client";

import { SkillDetailView } from "@leros/app-ui";
import { useChatStore, useLayoutStore } from "@leros/store";
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

	return (
		<SkillDetailView
			skillId={skillId}
			source="official"
			onBack={() => router.push("/skills")}
			onUse={handleUse}
		/>
	);
}
