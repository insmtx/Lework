import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { PlanBlock } from "./PlanBlock";

describe("PlanBlock", () => {
	it("shows only the overview and opens the file preview on click", async () => {
		const user = userEvent.setup();
		const onOpen = vi.fn();

		render(
			<PlanBlock fileId="file_plan_1" onOpen={onOpen}>
				<div>计划概览</div>
			</PlanBlock>,
		);

		expect(screen.getByText("计划")).toBeInTheDocument();
		expect(screen.getByText("计划概览")).toBeInTheDocument();
		expect(screen.queryByText(/行/)).not.toBeInTheDocument();
		expect(onOpen).not.toHaveBeenCalled();
		expect(screen.getByTestId("plan-overview-viewport")).toHaveClass("h-56", "overflow-hidden");
		expect(screen.getByTestId("plan-overview-content")).toHaveClass("absolute", "min-h-[28rem]");
		expect(screen.getByTestId("plan-overview-fade").className).not.toContain("shadow-");

		await user.click(screen.getByRole("button", { name: "打开完整计划" }));
		expect(onOpen).toHaveBeenCalledWith("file_plan_1");
	});

	it("copies the full plan without opening the preview", async () => {
		const user = userEvent.setup();
		const onOpen = vi.fn();
		const onCopy = vi.fn().mockResolvedValue(undefined);

		render(
			<PlanBlock fileId="file_plan_1" onOpen={onOpen} onCopy={onCopy}>
				<div>计划概览</div>
			</PlanBlock>,
		);

		await user.click(screen.getByRole("button", { name: "复制完整计划" }));
		expect(onCopy).toHaveBeenCalledWith("file_plan_1");
		expect(onOpen).not.toHaveBeenCalled();
	});
});
