import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SkillDetailView } from "./SkillDetailView";

const { mockOfficialGet, mockInstall, mockPluginGet, mockInstallationStatus, mockDelete } =
	vi.hoisted(() => ({
		mockOfficialGet: vi.fn(),
		mockInstall: vi.fn(),
		mockPluginGet: vi.fn(),
		mockInstallationStatus: vi.fn(),
		mockDelete: vi.fn(),
	}));

vi.mock("@leros/store", () => ({
	officialPluginMarketplaceApi: {
		get: mockOfficialGet,
		install: mockInstall,
	},
	pluginApi: {
		get: mockPluginGet,
		getInstallationStatus: mockInstallationStatus,
		delete: mockDelete,
	},
}));

vi.mock("../common/MarkdownRenderer", () => ({
	MarkdownRenderer: () => <div>markdown</div>,
}));

vi.mock("./SkillFileTree", () => ({
	SkillFileTree: () => <div>files</div>,
}));

vi.mock("sonner", () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
	},
}));

const officialItem = {
	public_id: "mkt_demo",
	code: "demo",
	kind: "skill",
	name: "Demo",
	description: "Demo skill",
	author: "Lework",
	version: "2",
	category: "official",
	tags: [],
	verified: true,
	installed: true,
	installed_plugin_id: "plugin_demo",
	marketplace_available: true,
	latest_version: "2",
	update_available: true,
	organization_override: false,
	content: null,
};

const organizationPlugin = {
	public_id: "plugin_demo",
	code: "demo",
	kind: "skill",
	name: "Demo",
	description: "Demo skill",
	status: "active",
	origin: "marketplace",
	current_revision: 3,
};

const updateStatus = {
	kind: "skill",
	code: "demo",
	installed: true,
	plugin_id: "plugin_demo",
	current_version: "3",
	marketplace_based: true,
	marketplace_item_id: "mkt_demo",
	installed_marketplace_version: "1",
	marketplace_available: true,
	latest_marketplace_version: "2",
	update_available: true,
};

describe("SkillDetailView installation status", () => {
	afterEach(() => {
		cleanup();
	});

	beforeEach(() => {
		vi.clearAllMocks();
		mockOfficialGet.mockResolvedValue({ data: { data: officialItem } });
		mockPluginGet.mockResolvedValue({
			data: { data: { plugin: organizationPlugin, content: null } },
		});
		mockInstallationStatus.mockResolvedValue({ data: { data: updateStatus } });
		mockInstall.mockResolvedValue({
			data: { data: { operation: "updated", plugin: organizationPlugin } },
		});
	});

	it("uses an official Skill by code and exposes only an available update", async () => {
		const onUse = vi.fn();
		render(<SkillDetailView skillId="mkt_demo" source="official" onUse={onUse} />);

		expect(await screen.findByText("有更新")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "去使用" }));

		expect(onUse).toHaveBeenCalledWith("demo", "Demo");
		expect(screen.queryByRole("button", { name: "更多操作" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "安装技能" })).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "更新" })).toBeInTheDocument();
		expect(mockInstall).not.toHaveBeenCalled();
	});

	it("keeps the back action in a sticky bar", async () => {
		const onBack = vi.fn();
		const { container } = render(
			<SkillDetailView skillId="mkt_demo" source="official" onBack={onBack} onUse={vi.fn()} />,
		);

		await screen.findByText("Demo");
		const backBar = container.querySelector('[data-slot="skill-detail-back-bar"]');
		expect(backBar).toHaveClass("sticky", "top-0");
		expect(backBar).toHaveAttribute("data-stuck", "false");
		expect(backBar).not.toHaveTextContent("Demo");

		const scrollContainer = container.querySelector('[data-slot="skill-detail-scroll"]');
		expect(scrollContainer).not.toBeNull();
		if (!scrollContainer) throw new Error("技能详情滚动容器不存在");
		fireEvent.scroll(scrollContainer, { target: { scrollTop: 32 } });

		expect(backBar).toHaveAttribute("data-stuck", "true");
		expect(backBar).toHaveTextContent("Demo");
		expect(backBar?.querySelector("button:last-child")).toHaveTextContent("去使用");

		fireEvent.click(screen.getByRole("button", { name: "返回" }));
		expect(onBack).toHaveBeenCalledOnce();
	});

	it("shows only version and author metadata without marketplace metrics", async () => {
		const { container } = render(<SkillDetailView skillId="mkt_demo" source="official" />);

		await screen.findByText("Demo");
		const metadata = container.querySelector('[data-slot="skill-detail-metadata"]');
		expect(metadata).toHaveTextContent("版本v2作者Lework");
		expect(metadata).not.toHaveTextContent("类型");
		expect(metadata).not.toHaveTextContent("来源");
		expect(metadata).not.toHaveTextContent("分类");
		expect(screen.queryByText(/下载$/)).not.toBeInTheDocument();
		expect(screen.queryByText("4.9 评分")).not.toBeInTheDocument();
		expect(screen.queryByText("官方认证")).not.toBeInTheDocument();
	});

	it("lets long detail content expand before applying bottom spacing", async () => {
		const { container } = render(<SkillDetailView skillId="mkt_demo" source="official" />);

		await screen.findByText("Demo");
		const content = container.querySelector('[data-slot="skill-detail-content"]');
		expect(content).toHaveClass("pb-12", "lg:pb-16");
		expect(content).not.toHaveClass("min-h-0");
	});

	it("updates an installed marketplace Skill from its detail action", async () => {
		render(<SkillDetailView skillId="mkt_demo" source="official" />);

		fireEvent.click(await screen.findByRole("button", { name: "更新" }));

		await waitFor(() => expect(mockInstall).toHaveBeenCalledWith("mkt_demo"));
		expect(mockOfficialGet).toHaveBeenCalledTimes(2);
	});

	it("keeps an archived installed Skill usable without an update action", async () => {
		mockOfficialGet.mockResolvedValueOnce({
			data: {
				data: {
					...officialItem,
					marketplace_available: false,
					update_available: false,
				},
			},
		});
		const onUse = vi.fn();
		render(<SkillDetailView skillId="mkt_demo" source="official" onUse={onUse} />);

		expect(await screen.findByText("已下架")).toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "更新" })).not.toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "去使用" }));
		expect(onUse).toHaveBeenCalledWith("demo", "Demo");
	});

	it("shows a missing installed snapshot without falling back to marketplace content", async () => {
		mockOfficialGet.mockResolvedValueOnce({
			data: {
				data: {
					...officialItem,
					content: null,
				},
			},
		});
		render(<SkillDetailView skillId="mkt_demo" source="official" />);

		expect(await screen.findByText("暂无内容快照")).toBeInTheDocument();
		expect(screen.queryByText("latest marketplace content")).not.toBeInTheDocument();
	});

	it("warns when an organization Skill overrides the marketplace code", async () => {
		mockOfficialGet.mockResolvedValueOnce({
			data: {
				data: {
					...officialItem,
					installed: false,
					installed_plugin_id: undefined,
					update_available: false,
					organization_override: true,
				},
			},
		});
		render(<SkillDetailView skillId="mkt_demo" source="official" />);

		expect(await screen.findByText("组织同名版本")).toBeInTheDocument();
		expect(screen.getByText(/“去使用”将执行组织版本，市场版本不会覆盖它/)).toBeInTheDocument();
	});

	it("shows update in the organization detail overflow menu", async () => {
		render(<SkillDetailView skillId="plugin_demo" source="organization" />);

		expect(await screen.findByText("有更新")).toBeInTheDocument();
		fireEvent.click(screen.getByRole("button", { name: "更多操作" }));
		const updateItem = await screen.findByText("更新");
		fireEvent.click(updateItem);

		await waitFor(() => expect(mockInstall).toHaveBeenCalledWith("mkt_demo"));
	});
});
