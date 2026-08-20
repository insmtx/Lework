import "@testing-library/jest-dom/vitest";
import type { SkillMarketplaceItem } from "@leros/store";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SkillCard } from "./SkillCard";

function skill(overrides: Partial<SkillMarketplaceItem> = {}): SkillMarketplaceItem {
	return {
		source_type: "official",
		skill_id: "mkt_demo",
		name: "demo",
		display_name: "Demo",
		description: "Demo skill",
		version: "2",
		author: "LeWork",
		category: "official",
		tags: [],
		icon: "",
		installs: 0,
		verified: true,
		marketplace_available: true,
		...overrides,
	};
}

describe("SkillCard marketplace state", () => {
	afterEach(cleanup);

	it("shows the update state without rendering a version", () => {
		render(<SkillCard skill={skill({ installed: true, version: "1", update_available: true })} />);

		expect(screen.queryByText("v1")).not.toBeInTheDocument();
		expect(screen.getByText("有更新")).toBeInTheDocument();
	});

	it("prioritizes archived and organization override states", () => {
		const { rerender } = render(
			<SkillCard
				skill={skill({
					installed: true,
					marketplace_available: false,
					update_available: true,
				})}
			/>,
		);
		expect(screen.getByText("已下架")).toBeInTheDocument();
		expect(screen.queryByText("有更新")).not.toBeInTheDocument();

		rerender(
			<SkillCard
				skill={skill({
					installed: false,
					organization_override: true,
				})}
			/>,
		);
		expect(screen.getByText("组织同名版本")).toBeInTheDocument();
	});

	it("does not distinguish an installed marketplace skill on the card", () => {
		render(<SkillCard skill={skill({ installed: true })} />);

		expect(screen.queryByText("已安装")).not.toBeInTheDocument();
		expect(screen.getByText("技能市场")).toBeInTheDocument();
	});

	it("uses a marketplace skill directly without opening its detail", () => {
		const onClick = vi.fn();
		const onUse = vi.fn();
		const item = skill();
		render(<SkillCard skill={item} onClick={onClick} onUse={onUse} />);

		fireEvent.click(screen.getByRole("button", { name: "使用" }));

		expect(onUse).toHaveBeenCalledWith(item);
		expect(onClick).not.toHaveBeenCalled();
		expect(screen.queryByText("使用时自动准备最新版本")).not.toBeInTheDocument();
		expect(screen.queryByText("已安装，可直接使用")).not.toBeInTheDocument();
	});
});

describe("SkillCard organization role capsule", () => {
	afterEach(cleanup);

	it("shows an owner capsule on a mine card", () => {
		render(
			<SkillCard
				variant="mine"
				skill={skill({ source_type: "organization", permission: { role: "owner" } })}
			/>,
		);
		expect(screen.getByText("所有者")).toBeInTheDocument();
	});

	it("shows an admin capsule on a mine card", () => {
		render(
			<SkillCard
				variant="mine"
				skill={skill({ source_type: "organization", permission: { role: "admin" } })}
			/>,
		);
		expect(screen.getByText("管理员")).toBeInTheDocument();
	});

	it("does not show a role capsule for a viewer", () => {
		render(
			<SkillCard
				variant="mine"
				skill={skill({ source_type: "organization", permission: { role: "viewer" } })}
			/>,
		);
		expect(screen.queryByText("所有者")).not.toBeInTheDocument();
		expect(screen.queryByText("管理员")).not.toBeInTheDocument();
	});
});
