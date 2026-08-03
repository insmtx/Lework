"use client";

import {
	mergeSkillOptions,
	type OfficialPluginMarketplaceItem,
	officialPluginMarketplaceApi,
	type PluginComposerOption,
	type PluginListItem,
	pluginApi,
	pluginToComposerOption,
} from "@leros/store";
import { useCallback, useEffect, useState } from "react";

const SKILL_PICKER_LIMIT = 100;

type SkillPickerSourceResult<T> = {
	items: T[];
	failed: boolean;
};

type SkillPickerLoadParams = {
	projectId?: string | null;
	includeBuiltin: boolean;
};

type UseSkillPickerOptionsParams = SkillPickerLoadParams & {
	enabled?: boolean;
};

export type ResolvedOrganizationSkill = {
	pluginId: string;
	installedDuringAction: boolean;
};

export type ProjectSkillBindingSummary = {
	failedCount: number;
	installedButUnboundCount: number;
};

export class ProjectSkillBindingError extends Error {
	installedDuringAction: boolean;

	constructor(installedDuringAction: boolean) {
		super("项目技能关联失败");
		this.name = "ProjectSkillBindingError";
		this.installedDuringAction = installedDuringAction;
	}
}

async function loadSource<T>(request: Promise<T[]>): Promise<SkillPickerSourceResult<T>> {
	try {
		return { items: await request, failed: false };
	} catch {
		return { items: [], failed: true };
	}
}

export function marketplaceToSkillOption(
	item: OfficialPluginMarketplaceItem,
): PluginComposerOption {
	return {
		code: item.code,
		label: item.name || item.code,
		description: item.description ?? "",
		source: "marketplace",
		origin: "marketplace",
		pluginId: item.installed_plugin_id,
		marketplaceItemId: item.public_id,
		organizationOverride: item.organization_override,
		keywords: [item.name, item.code, item.description].filter((value): value is string =>
			Boolean(value),
		),
	};
}

export async function loadSkillPickerOptions({
	projectId,
	includeBuiltin,
}: SkillPickerLoadParams): Promise<{
	options: PluginComposerOption[];
	error: string | null;
}> {
	const projectRequest: Promise<PluginListItem[]> = projectId
		? pluginApi
				.listProject({ public_id: projectId, kind: "skill" })
				.then((response) => (response.data.code === 0 ? response.data.data : []))
		: Promise.resolve([]);
	const organizationRequest = pluginApi
		.list({ kind: "skill", status: "active", limit: SKILL_PICKER_LIMIT })
		.then((response) =>
			response.data.code === 0 ? (response.data.data.plugins ?? []) : Promise.reject(),
		);
	const marketplaceRequest = officialPluginMarketplaceApi
		.list({ kind: "skill", limit: SKILL_PICKER_LIMIT })
		.then((response) =>
			response.data.code === 0 ? (response.data.data.items ?? []) : Promise.reject(),
		);
	const builtinRequest: Promise<PluginListItem[]> = includeBuiltin
		? pluginApi
				.listBuiltinSkills()
				.then((response) =>
					response.data.code === 0 ? (response.data.data.plugins ?? []) : Promise.reject(),
				)
		: Promise.resolve([]);

	const [project, organization, marketplace, builtin] = await Promise.all([
		loadSource(projectRequest),
		loadSource(organizationRequest),
		loadSource(marketplaceRequest),
		loadSource(builtinRequest),
	]);
	const requestedGeneralSources = includeBuiltin
		? [organization, marketplace, builtin]
		: [organization, marketplace];

	return {
		options: mergeSkillOptions(
			project.items.map((item) => pluginToComposerOption(item, "project")),
			organization.items.map((item) => pluginToComposerOption(item, "organization")),
			marketplace.items.map(marketplaceToSkillOption),
			builtin.items.map((item) => pluginToComposerOption(item, "builtin")),
		),
		error: requestedGeneralSources.every((source) => source.failed) ? "技能加载失败" : null,
	};
}

export async function resolveOrganizationSkill(
	option: PluginComposerOption,
): Promise<ResolvedOrganizationSkill> {
	if (option.pluginId) {
		return { pluginId: option.pluginId, installedDuringAction: false };
	}
	if (option.source !== "marketplace" || !option.marketplaceItemId) {
		throw new Error("该技能无法关联到项目");
	}
	if (option.organizationOverride) {
		const response = await pluginApi.getInstallationStatus({ kind: "skill", code: option.code });
		const pluginId = response.data.data.plugin_id;
		if (!pluginId) {
			throw new Error("组织同名技能暂时不可用，请重试");
		}
		return { pluginId, installedDuringAction: false };
	}

	const response = await officialPluginMarketplaceApi.install(option.marketplaceItemId);
	const pluginId = response.data.data.plugin.public_id;
	if (!pluginId) {
		throw new Error("技能安装后未返回组织插件 ID");
	}
	return {
		pluginId,
		installedDuringAction: response.data.data.operation === "installed",
	};
}

export async function bindSkillToProject(
	projectId: string,
	option: PluginComposerOption,
): Promise<ResolvedOrganizationSkill> {
	const resolved = await resolveOrganizationSkill(option);
	try {
		await pluginApi.addToProject({ public_id: projectId, plugin_id: resolved.pluginId });
	} catch {
		throw new ProjectSkillBindingError(resolved.installedDuringAction);
	}
	return resolved;
}

export async function bindSkillsToProject(
	projectId: string,
	options: PluginComposerOption[],
): Promise<ProjectSkillBindingSummary> {
	let failedCount = 0;
	let installedButUnboundCount = 0;
	for (const option of options) {
		try {
			await bindSkillToProject(projectId, option);
		} catch (error) {
			failedCount += 1;
			if (error instanceof ProjectSkillBindingError && error.installedDuringAction) {
				installedButUnboundCount += 1;
			}
		}
	}
	return { failedCount, installedButUnboundCount };
}

export function useSkillPickerOptions({
	projectId,
	includeBuiltin,
	enabled = true,
}: UseSkillPickerOptionsParams): {
	skillOptions: PluginComposerOption[] | undefined;
	skillsLoading: boolean;
	skillsError: string | null;
	reloadSkillOptions: () => Promise<void>;
} {
	const [skillOptions, setSkillOptions] = useState<PluginComposerOption[] | undefined>(undefined);
	const [skillsLoading, setSkillsLoading] = useState(enabled);
	const [skillsError, setSkillsError] = useState<string | null>(null);

	const reloadSkillOptions = useCallback(async () => {
		setSkillsLoading(true);
		const result = await loadSkillPickerOptions({ projectId, includeBuiltin });
		setSkillOptions(result.options);
		setSkillsError(result.error);
		setSkillsLoading(false);
	}, [includeBuiltin, projectId]);

	useEffect(() => {
		if (!enabled) return;
		let cancelled = false;
		setSkillsLoading(true);
		void loadSkillPickerOptions({ projectId, includeBuiltin }).then((result) => {
			if (cancelled) return;
			setSkillOptions(result.options);
			setSkillsError(result.error);
			setSkillsLoading(false);
		});
		return () => {
			cancelled = true;
		};
	}, [enabled, includeBuiltin, projectId]);

	return { skillOptions, skillsLoading, skillsError, reloadSkillOptions };
}
