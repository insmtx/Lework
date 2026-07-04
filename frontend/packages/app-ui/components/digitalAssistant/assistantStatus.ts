import type { DigitalAssistantItem } from "@leros/store";

export type AssistantDisplayStatus = {
	label: string;
	className: string;
	dotClassName: string;
	title?: string;
};

const activeBusinessStatus: AssistantDisplayStatus = {
	label: "运行中",
	className: "bg-emerald-50 text-emerald-700 border-emerald-200",
	dotClassName: "bg-emerald-500",
};

const businessStatusMap: Record<string, AssistantDisplayStatus> = {
	active: activeBusinessStatus,
	inactive: {
		label: "已停用",
		className: "bg-slate-50 text-slate-500 border-slate-200",
		dotClassName: "bg-slate-400",
	},
	draft: {
		label: "草稿",
		className: "bg-amber-50 text-amber-700 border-amber-200",
		dotClassName: "bg-amber-400",
	},
};

const activeDeploymentStatusMap: Record<string, AssistantDisplayStatus> = {
	pending: {
		label: "初始化",
		className: "bg-amber-50 text-amber-700 border-amber-200",
		dotClassName: "bg-amber-400",
	},
	provisioning: {
		label: "部署中",
		className: "bg-blue-50 text-blue-700 border-blue-200",
		dotClassName: "bg-blue-500",
	},
	ready: {
		label: "可用",
		className: "bg-emerald-50 text-emerald-700 border-emerald-200",
		dotClassName: "bg-emerald-500",
	},
	failed: {
		label: "部署失败",
		className: "bg-red-50 text-red-700 border-red-200",
		dotClassName: "bg-red-500",
	},
};

export function getAssistantDisplayStatus(assistant: DigitalAssistantItem): AssistantDisplayStatus {
	if (assistant.status !== "active") {
		return (
			businessStatusMap[assistant.status] ?? {
				label: assistant.status || "未知",
				className: "bg-slate-50 text-slate-500 border-slate-200",
				dotClassName: "bg-slate-300",
			}
		);
	}

	const deploymentStatus = assistant.deploymentStatus?.trim();
	const deploymentInfo = deploymentStatus ? activeDeploymentStatusMap[deploymentStatus] : undefined;
	if (deploymentInfo) {
		return {
			label: deploymentInfo.label,
			className: deploymentInfo.className,
			dotClassName: deploymentInfo.dotClassName,
			title: assistant.deploymentError || undefined,
		};
	}

	return {
		label: activeBusinessStatus.label,
		className: activeBusinessStatus.className,
		dotClassName: activeBusinessStatus.dotClassName,
		title: assistant.deploymentError || undefined,
	};
}
