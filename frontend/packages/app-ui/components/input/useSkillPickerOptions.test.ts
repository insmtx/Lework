import {
	mergeSkillOptions,
	type OfficialPluginMarketplaceItem,
	officialPluginMarketplaceApi,
	type PluginComposerOption,
	type PluginListItem,
	pluginApi,
	pluginToComposerOption,
} from "@leros/store";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	bindSkillsToProject,
	bindSkillToProject,
	loadSkillPickerOptions,
	marketplaceToSkillOption,
	useSkillPickerOptions,
} from "./useSkillPickerOptions";

function plugin(code: string, name: string, origin = "org"): PluginListItem {
	return {
		public_id: `plugin_${code}`,
		code,
		kind: "skill",
		name,
		description: `${name} description`,
		status: "active",
		origin,
		current_revision: 1,
	};
}

function marketplaceItem(
	code: string,
	name: string,
	overrides: Partial<OfficialPluginMarketplaceItem> = {},
): OfficialPluginMarketplaceItem {
	return {
		public_id: `market_${code}`,
		code,
		kind: "skill",
		name,
		description: `${name} description`,
		author: "Leros",
		version: "1",
		category: "general",
		tags: [],
		verified: true,
		installed: false,
		marketplace_available: true,
		update_available: false,
		organization_override: false,
		...overrides,
	};
}

function option(
	code: string,
	label: string,
	source: NonNullable<PluginComposerOption["source"]>,
): PluginComposerOption {
	return { code, label, description: "", keywords: [], source };
}

afterEach(() => {
	vi.restoreAllMocks();
});

describe("Skill picker option merging", () => {
	it("prefers translated marketplace display names", () => {
		const result = marketplaceToSkillOption(
			marketplaceItem("market", "English Skill", { display_name: "中文技能" }),
		);

		expect(result.label).toBe("中文技能");
		expect(result.keywords).toContain("中文技能");
	});

	it("prefers translated organization display names", () => {
		const organizationSkill = plugin("organization", "English Skill");
		organizationSkill.display_name = "中文技能";

		const result = pluginToComposerOption(organizationSkill, "organization");

		expect(result.label).toBe("中文技能");
		expect(result.keywords).toContain("中文技能");
	});

	it("keeps project, organization, marketplace, and builtin priority by code only", () => {
		const result = mergeSkillOptions(
			[option("shared", "Project", "project")],
			[
				option("SHARED", "Organization", "organization"),
				option("org-only", "Same label", "organization"),
			],
			[
				option("shared", "Marketplace", "marketplace"),
				option("market-only", "Same label", "marketplace"),
			],
			[
				option("market-only", "Builtin duplicate", "builtin"),
				option("builtin-only", "Builtin", "builtin"),
			],
		);

		expect(result.map((item) => item.code)).toEqual([
			"shared",
			"org-only",
			"market-only",
			"builtin-only",
		]);
		expect(result[0]?.source).toBe("project");
		expect(result[1]?.label).toBe(result[2]?.label);
	});

	it("loads all chat sources and degrades when one source fails", async () => {
		vi.spyOn(pluginApi, "listProject").mockResolvedValue({
			data: { code: 0, message: "success", data: [plugin("project", "Project")] },
		} as Awaited<ReturnType<typeof pluginApi.listProject>>);
		vi.spyOn(pluginApi, "list").mockRejectedValue(new Error("organization unavailable"));
		vi.spyOn(officialPluginMarketplaceApi, "list").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: { items: [marketplaceItem("market", "Market")] },
			},
		} as Awaited<ReturnType<typeof officialPluginMarketplaceApi.list>>);
		vi.spyOn(pluginApi, "listBuiltinSkills").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: { plugins: [plugin("builtin", "Builtin", "builtin_worker")] },
			},
		} as Awaited<ReturnType<typeof pluginApi.listBuiltinSkills>>);

		const result = await loadSkillPickerOptions({
			projectId: "project_1",
			includeBuiltin: true,
		});

		expect(result.error).toBeNull();
		expect(result.options.map((item) => item.code)).toEqual(["project", "market", "builtin"]);
		expect(result.options[2]?.source).toBe("builtin");
	});

	it("omits builtin Skills from project pickers without installing marketplace entries", async () => {
		vi.spyOn(pluginApi, "list").mockResolvedValue({
			data: { code: 0, message: "success", data: { plugins: [] } },
		} as unknown as Awaited<ReturnType<typeof pluginApi.list>>);
		vi.spyOn(officialPluginMarketplaceApi, "list").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: { items: [marketplaceItem("market", "Market")] },
			},
		} as Awaited<ReturnType<typeof officialPluginMarketplaceApi.list>>);
		const builtin = vi.spyOn(pluginApi, "listBuiltinSkills");
		const install = vi.spyOn(officialPluginMarketplaceApi, "install");

		const result = await loadSkillPickerOptions({ includeBuiltin: false });

		expect(result.options.map((item) => item.code)).toEqual(["market"]);
		expect(builtin).not.toHaveBeenCalled();
		expect(install).not.toHaveBeenCalled();
	});

	it("loads Skills after authentication enables the picker", async () => {
		const organization = vi.spyOn(pluginApi, "list").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: { plugins: [plugin("org", "Organization")] },
			},
		} as Awaited<ReturnType<typeof pluginApi.list>>);
		vi.spyOn(officialPluginMarketplaceApi, "list").mockResolvedValue({
			data: { code: 0, message: "success", data: { items: [] } },
		} as unknown as Awaited<ReturnType<typeof officialPluginMarketplaceApi.list>>);
		vi.spyOn(pluginApi, "listBuiltinSkills").mockResolvedValue({
			data: { code: 0, message: "success", data: { plugins: [] } },
		} as unknown as Awaited<ReturnType<typeof pluginApi.listBuiltinSkills>>);

		const { result, rerender } = renderHook(
			({ enabled }) =>
				useSkillPickerOptions({
					includeBuiltin: true,
					enabled,
				}),
			{ initialProps: { enabled: false } },
		);

		expect(organization).not.toHaveBeenCalled();
		expect(result.current.skillsLoading).toBe(false);

		rerender({ enabled: true });

		await waitFor(() => {
			expect(result.current.skillOptions?.map((item) => item.code)).toEqual(["org"]);
		});
		expect(organization).toHaveBeenCalledOnce();
	});
});

describe("project Skill binding", () => {
	it("installs an uninstalled marketplace Skill before binding it", async () => {
		const install = vi.spyOn(officialPluginMarketplaceApi, "install").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					operation: "installed",
					plugin: plugin("market", "Market"),
				},
			},
		} as Awaited<ReturnType<typeof officialPluginMarketplaceApi.install>>);
		const bind = vi.spyOn(pluginApi, "addToProject").mockResolvedValue({
			data: { code: 0, message: "success", data: null },
		} as Awaited<ReturnType<typeof pluginApi.addToProject>>);

		const result = await bindSkillToProject(
			"project_1",
			marketplaceToSkillOption(marketplaceItem("market", "Market")),
		);

		expect(result).toEqual({ pluginId: "plugin_market", installedDuringAction: true });
		expect(install).toHaveBeenCalledWith("market_market");
		expect(bind).toHaveBeenCalledWith({
			public_id: "project_1",
			plugin_id: "plugin_market",
		});
	});

	it("binds an organization Skill without installing it", async () => {
		const install = vi.spyOn(officialPluginMarketplaceApi, "install");
		const bind = vi.spyOn(pluginApi, "addToProject").mockResolvedValue({
			data: { code: 0, message: "success", data: null },
		} as Awaited<ReturnType<typeof pluginApi.addToProject>>);

		await bindSkillToProject("project_1", {
			...option("org", "Organization", "organization"),
			pluginId: "plugin_org",
		});

		expect(install).not.toHaveBeenCalled();
		expect(bind).toHaveBeenCalledWith({
			public_id: "project_1",
			plugin_id: "plugin_org",
		});
	});

	it("binds an organization override instead of overwriting it with the marketplace version", async () => {
		const install = vi.spyOn(officialPluginMarketplaceApi, "install");
		vi.spyOn(pluginApi, "getInstallationStatus").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					kind: "skill",
					code: "shared",
					installed: true,
					plugin_id: "plugin_custom",
					marketplace_based: false,
					marketplace_available: true,
					update_available: false,
				},
			},
		} as Awaited<ReturnType<typeof pluginApi.getInstallationStatus>>);
		const bind = vi.spyOn(pluginApi, "addToProject").mockResolvedValue({
			data: { code: 0, message: "success", data: null },
		} as Awaited<ReturnType<typeof pluginApi.addToProject>>);

		await bindSkillToProject(
			"project_1",
			marketplaceToSkillOption(
				marketplaceItem("shared", "Shared", { organization_override: true }),
			),
		);

		expect(install).not.toHaveBeenCalled();
		expect(bind).toHaveBeenCalledWith({
			public_id: "project_1",
			plugin_id: "plugin_custom",
		});
	});

	it("reports when installation succeeded but project binding failed", async () => {
		vi.spyOn(officialPluginMarketplaceApi, "install").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					operation: "installed",
					plugin: plugin("market", "Market"),
				},
			},
		} as Awaited<ReturnType<typeof officialPluginMarketplaceApi.install>>);
		vi.spyOn(pluginApi, "addToProject").mockRejectedValue(new Error("bind failed"));

		await expect(
			bindSkillToProject(
				"project_1",
				marketplaceToSkillOption(marketplaceItem("market", "Market")),
			),
		).rejects.toMatchObject({ installedDuringAction: true });
	});

	it("keeps successful bindings and summarizes partial failures", async () => {
		vi.spyOn(officialPluginMarketplaceApi, "install").mockResolvedValue({
			data: {
				code: 0,
				message: "success",
				data: {
					operation: "installed",
					plugin: plugin("market", "Market"),
				},
			},
		} as Awaited<ReturnType<typeof officialPluginMarketplaceApi.install>>);
		vi.spyOn(pluginApi, "addToProject")
			.mockResolvedValueOnce({
				data: { code: 0, message: "success", data: null },
			} as Awaited<ReturnType<typeof pluginApi.addToProject>>)
			.mockRejectedValueOnce(new Error("bind failed"));

		const result = await bindSkillsToProject("project_1", [
			{
				...option("org", "Organization", "organization"),
				pluginId: "plugin_org",
			},
			marketplaceToSkillOption(marketplaceItem("market", "Market")),
		]);

		expect(result).toEqual({ failedCount: 1, installedButUnboundCount: 1 });
	});
});
