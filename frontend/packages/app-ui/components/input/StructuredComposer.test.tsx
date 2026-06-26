import "@testing-library/jest-dom/vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { type ComposerSkillOption, StructuredComposer } from "./StructuredComposer";

class ResizeObserverMock {
	observe() {
		// 中文注释：测试环境只需要占位实现，避免命令面板依赖浏览器原生 ResizeObserver。
	}
	unobserve() {
		// 中文注释：测试环境不需要真实解绑逻辑。
	}
	disconnect() {
		// 中文注释：测试环境不需要真实断开逻辑。
	}
}

Object.defineProperty(window, "ResizeObserver", {
	writable: true,
	value: ResizeObserverMock,
});

Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
	writable: true,
	value: vi.fn(),
});

function TestHarness({
	projectSkillOptions,
	onValueChange,
}: {
	projectSkillOptions: ComposerSkillOption[];
	onValueChange?: (value: string) => void;
}) {
	const [value, setValue] = useState("");

	return (
		<StructuredComposer
			value={value}
			onChange={(nextValue) => {
				// 中文注释：用受控状态承接真实输入链路，确保测试覆盖到选择技能后的最终文本结果。
				setValue(nextValue);
				onValueChange?.(nextValue);
			}}
			onSubmit={vi.fn()}
			onPasteFiles={vi.fn()}
			onFocus={vi.fn()}
			onBlur={vi.fn()}
			placeholder="请输入"
			isProjectVariant
			projectSkillOptions={projectSkillOptions}
		/>
	);
}

describe("StructuredComposer", () => {
	it("通过 / 选择技能后会补齐尾部空格", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(
			<TestHarness
				onValueChange={handleValueChange}
				projectSkillOptions={[
					{
						code: "doc-coauthoring",
						label: "doc-coauthoring",
						description: "doc",
						keywords: [],
					},
				]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("/");
		await user.keyboard("{Enter}");

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/doc-coauthoring ");
		});
	});
});
