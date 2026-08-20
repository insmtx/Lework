"use client";

import { SkillDetailView, buildSkillWorkbenchPrefill } from "@leros/app-ui";
import { useLayoutStore } from "@leros/store";
import { useParams, useRouter } from "next/navigation";
import { useCallback } from "react";
import { toast } from "sonner";

export default function SkillDetailPage() {
	const params = useParams<{ skillId: string }>();
	const router = useRouter();
	const skillId = params.skillId;
	const { activeProjectId, projects, setProjectRoute, setProjectComposerPrefill } = useLayoutStore(
		(s) => ({
			activeProjectId: s.activeProjectId,
			projects: s.projects,
			setProjectRoute: s.setProjectRoute,
			setProjectComposerPrefill: s.setProjectComposerPrefill,
		}),
	);

	const handleUse = useCallback(
		(nextSkillId: string, displayLabel?: string) => {
			const targetProjectId = activeProjectId ?? projects[0]?.id;
			if (!targetProjectId) {
				toast.error("请先创建或选择项目");
				return;
			}

			const prefill = buildSkillWorkbenchPrefill(nextSkillId, undefined, displayLabel);
			setProjectComposerPrefill({
				projectId: targetProjectId,
				value: prefill.value,
				tokens: prefill.tokens,
			});
			setProjectRoute(targetProjectId, "chat");
			router.push(`/projects/${targetProjectId}`);
		},
		[activeProjectId, projects, router, setProjectComposerPrefill, setProjectRoute],
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
