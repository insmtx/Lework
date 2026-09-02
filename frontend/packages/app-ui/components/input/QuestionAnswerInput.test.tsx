import "@testing-library/jest-dom/vitest";

import type { QuestionRequest } from "@leros/store/types/chat";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QuestionAnswerInput } from "./QuestionAnswerInput";

afterEach(cleanup);

function makeQuestion(overrides: Partial<QuestionRequest> = {}): QuestionRequest {
	return {
		requestId: "request-1",
		status: "pending",
		questions: [
			{
				question: "职位名称是什么？",
				options: [
					{ label: "测试工程师" },
					{ label: "测试开发工程师" },
					{ label: "其他（请说明）" },
				],
				multiple: false,
				custom: false,
			},
		],
		...overrides,
	};
}

describe("QuestionAnswerInput", () => {
	it("点击自定义选项后取消其他单选项的选中状态", async () => {
		const user = userEvent.setup();

		render(
			<QuestionAnswerInput
				question={makeQuestion({
					questions: [
						{
							question: "您说的「测试」是指？",
							options: [
								{ label: "新建一个认定管理办法" },
								{ label: "只测试技能流程" },
								{ label: "查看和学习参考文件" },
							],
							multiple: false,
							custom: true,
						},
					],
				})}
				messageId="message-1"
				variant="default"
				onAnswer={vi.fn()}
			/>,
		);

		const firstOption = screen.getByRole("radio", { name: /新建一个认定管理办法/ });
		const customInput = screen.getByPlaceholderText("输入自定义答案");

		expect(firstOption).toHaveAttribute("aria-checked", "true");

		await user.click(customInput);

		await waitFor(() => {
			expect(firstOption).toHaveAttribute("aria-checked", "false");
		});
		expect(screen.getByRole("button", { name: /提交/ })).toBeDisabled();
	});

	it("多题切换后选项不会重复累积", async () => {
		const user = userEvent.setup();

		render(
			<QuestionAnswerInput
				question={makeQuestion({
					questions: [
						{
							question: "发文机关是哪一级政府部门？",
							options: [{ label: "按实际填写" }],
							multiple: false,
							custom: true,
						},
						{
							question: "创新中心的具体名称是什么？",
							options: [{ label: "按实际命名" }],
							multiple: false,
							custom: true,
						},
					],
				})}
				messageId="message-1"
				variant="default"
				onAnswer={vi.fn()}
			/>,
		);

		expect(screen.getByRole("radio", { name: /按实际填写/ })).toBeInTheDocument();
		expect(screen.queryByRole("radio", { name: /按实际命名/ })).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(1);

		await user.click(screen.getByRole("button", { name: /下一个问题/ }));

		expect(screen.getByRole("radio", { name: /按实际命名/ })).toBeInTheDocument();
		expect(screen.queryByRole("radio", { name: /按实际填写/ })).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(1);

		await user.click(screen.getByRole("button", { name: /上一个问题/ }));

		expect(screen.getByRole("radio", { name: /按实际填写/ })).toBeInTheDocument();
		expect(screen.queryByRole("radio", { name: /按实际命名/ })).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(1);

		await user.click(screen.getByRole("button", { name: /下一个问题/ }));

		expect(screen.getByRole("radio", { name: /按实际命名/ })).toBeInTheDocument();
		expect(screen.queryByRole("radio", { name: /按实际填写/ })).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(1);
	});

	it("在自定义输入框中输入并提交自定义答案", async () => {
		const user = userEvent.setup();
		const handleAnswer = vi.fn();

		render(
			<QuestionAnswerInput
				question={makeQuestion()}
				messageId="message-1"
				variant="default"
				onAnswer={handleAnswer}
			/>,
		);

		const customInput = screen.getByPlaceholderText("输入自定义答案");
		await user.click(customInput);
		await waitFor(() => expect(customInput).toHaveFocus());
		expect(screen.getByRole("button", { name: /提交/ })).toBeDisabled();

		await user.type(customInput, "测试架构师");
		await user.click(screen.getByRole("button", { name: /提交/ }));

		expect(handleAnswer).toHaveBeenCalledWith("message-1", "request-1", [["测试架构师"]]);
	});

	it("问题标题过长时悬浮展示全文", async () => {
		const user = userEvent.setup();
		const longQuestion =
			"请确认本次要比对的招标文件版本、投标文件范围，以及是否需要把报价明细、技术偏离表和商务条款一起纳入评审结论？";

		render(
			<QuestionAnswerInput
				question={makeQuestion({
					questions: [
						{
							question: longQuestion,
							options: [{ label: "全部纳入" }, { label: "仅技术偏离表" }],
							multiple: false,
							custom: false,
						},
					],
				})}
				messageId="message-1"
				variant="default"
				onAnswer={vi.fn()}
			/>,
		);

		await user.hover(screen.getByText(longQuestion, { selector: '[data-slot="tooltip-trigger"]' }));

		expect(
			await screen.findByText(longQuestion, { selector: '[data-slot="tooltip-content"]' }),
		).toBeInTheDocument();
	});
});
