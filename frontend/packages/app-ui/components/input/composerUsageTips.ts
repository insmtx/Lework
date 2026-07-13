import type { Project, ProjectTask } from "@leros/store";

export type ComposerUsageTip = {
	id: string;
	label: string;
	buildWithContext: (contextLabel: string) => string;
	withoutContext: string;
};

export const COMPOSER_USAGE_TIPS: ComposerUsageTip[] = [
	{
		id: "execution-plan",
		label: "制定执行方案",
		buildWithContext: (contextLabel) =>
			`请基于当前 ${contextLabel} 的目标、对话、文件和已有成果，制定下一阶段的执行方案。\n请明确阶段目标、关键任务、负责人或适合参与的 AI 队友、预期交付物、时间节点和风险事项，并指出当前仍需补充的信息。`,
		withoutContext:
			"请根据我接下来输入的需求或上传的材料，帮我制定一份可执行的工作方案。\n请先明确目标、背景、约束条件和预期交付物，再拆解关键步骤、任务分工、时间节点和风险事项。信息不足的部分请列出待补充问题，不要自行假设。",
	},
	{
		id: "phase-report",
		label: "生成阶段汇报",
		buildWithContext: (contextLabel) =>
			`请基于当前 ${contextLabel} 的目标、任务进展、对话记录、文件和已有成果，生成一份阶段汇报。\n内容包括：总体结论、目标完成情况、关键进展、阶段成果、当前问题与风险、需要协调的事项，以及下一步计划。请突出重要变化和待决策事项。`,
		withoutContext:
			"请根据我接下来输入的信息或上传的材料，生成一份阶段汇报。\n内容包括：总体结论、目标完成情况、关键进展、阶段成果、当前问题与风险、需要协调的事项，以及下一步计划。信息不足的部分请标记为“待补充”。",
	},
	{
		id: "review-optimize",
		label: "进行评审并优化成果",
		buildWithContext: (contextLabel) =>
			`请结合当前 ${contextLabel} 的目标、背景、要求和已有上下文，评审当前交付成果。\n请判断成果是否满足目标和交付要求，输出总体结论、问题清单、修改优先级和具体优化建议，并给出优化后的关键内容。`,
		withoutContext:
			"请评审我接下来输入或上传的方案、报告、文档或其他成果。\n请从目标匹配、结构完整、内容准确、依据充分、表达清晰和可执行性等方面进行分析，输出总体结论、主要问题、修改优先级和具体建议，并优化其中的关键内容。",
	},
];

export function buildComposerContextLabel(projectName: string, task?: ProjectTask): string {
	if (task?.title) return `${projectName} ${task.title}`;
	return projectName;
}

export function resolveComposerUsageTipPrompt(
	tip: ComposerUsageTip,
	project?: Project,
	task?: ProjectTask,
): string {
	if (!project) return tip.withoutContext;
	return tip.buildWithContext(buildComposerContextLabel(project.name, task));
}

export function buildComposerUsageTips(
	project?: Project,
	task?: ProjectTask,
): Array<ComposerUsageTip & { prompt: string }> {
	return COMPOSER_USAGE_TIPS.map((tip) => ({
		...tip,
		prompt: resolveComposerUsageTipPrompt(tip, project, task),
	}));
}
