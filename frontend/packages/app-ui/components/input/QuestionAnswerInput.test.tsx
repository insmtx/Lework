import "@testing-library/jest-dom/vitest";

import type { QuestionRequest } from "@leros/store/types/chat";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { QuestionAnswerInput } from "./QuestionAnswerInput";

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
	it("点击其他选项后显示输入框，并提交自定义答案", async () => {
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

		expect(screen.queryByRole("textbox", { name: "其他答案" })).not.toBeInTheDocument();

		await user.click(screen.getByRole("button", { name: /其他（请说明）/ }));

		const customInput = screen.getByRole("textbox", { name: "其他答案" });
		await waitFor(() => expect(customInput).toHaveFocus());
		expect(screen.getByRole("button", { name: /提交/ })).toBeDisabled();

		await user.type(customInput, "测试架构师");
		await user.click(screen.getByRole("button", { name: /提交/ }));

		expect(handleAnswer).toHaveBeenCalledWith("message-1", "request-1", [["测试架构师"]]);
	});
});
