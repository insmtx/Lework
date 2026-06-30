import "@testing-library/jest-dom/vitest";

import type { QuestionRequest } from "@leros/store/types/chat";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PlanConfirmationInput } from "./PlanConfirmationInput";

afterEach(cleanup);

function makeQuestion(overrides: Partial<QuestionRequest> = {}): QuestionRequest {
	return {
		requestId: "question-plan",
		status: "pending",
		interactionType: "plan_confirmation",
		questions: [
			{
				header: "计划确认",
				question: "以下是当前计划，是否执行？",
				options: [{ label: "Yes" }, { label: "No" }],
				multiple: false,
				custom: false,
			},
		],
		plan: {
			content: "# Plan\n\n- Implement plan mode",
			filePath: ".opencode/plans/123-plan.md",
		},
		...overrides,
	};
}

describe("PlanConfirmationInput", () => {
	it("执行计划只提交精确的 Yes，并立即锁定", async () => {
		const user = userEvent.setup();
		const onAnswer = vi.fn(() => new Promise<void>(() => undefined));
		const onExecute = vi.fn();
		const onRevise = vi.fn();

		render(
			<PlanConfirmationInput
				question={makeQuestion()}
				messageId="message-1"
				variant="default"
				onAnswer={onAnswer}
				onExecute={onExecute}
				onRevise={onRevise}
			/>,
		);

		const execute = screen.getByRole("button", { name: "执行计划" });
		expect(screen.getByText("以下是当前计划，是否执行？")).toBeInTheDocument();
		await user.dblClick(execute);

		expect(onExecute).toHaveBeenCalledTimes(1);
		expect(onAnswer).toHaveBeenCalledTimes(1);
		expect(onAnswer).toHaveBeenCalledWith("message-1", "question-plan", [["Yes"]]);
		expect(execute).toBeDisabled();
	});

	it("调整计划只提交精确的 No，不触发执行", async () => {
		const user = userEvent.setup();
		const onAnswer = vi.fn();
		const onExecute = vi.fn();
		const onRevise = vi.fn();

		render(
			<PlanConfirmationInput
				question={makeQuestion()}
				messageId="message-1"
				variant="default"
				onAnswer={onAnswer}
				onExecute={onExecute}
				onRevise={onRevise}
			/>,
		);

		await user.click(screen.getByRole("button", { name: "调整计划" }));

		expect(onExecute).not.toHaveBeenCalled();
		expect(onRevise).toHaveBeenCalledTimes(1);
		expect(onAnswer).toHaveBeenCalledWith("message-1", "question-plan", [["No"]]);
	});

	it("计划读取失败时禁用执行但仍允许调整", () => {
		render(
			<PlanConfirmationInput
				question={makeQuestion({
					plan: { error: "read plan file: not found" },
				})}
				messageId="message-1"
				variant="default"
				onAnswer={vi.fn()}
				onExecute={vi.fn()}
				onRevise={vi.fn()}
			/>,
		);

		expect(screen.getByRole("button", { name: "执行计划" })).toBeDisabled();
		expect(screen.getByRole("button", { name: "调整计划" })).toBeEnabled();
		expect(screen.getByText("read plan file: not found")).toBeInTheDocument();
	});
});
