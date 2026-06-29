import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ComposerActionBar } from "./ComposerActionBar";
import {
	type ComposerSkillOption,
	StructuredComposer,
	type StructuredComposerHandle,
} from "./StructuredComposer";

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

afterEach(() => {
	cleanup();
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

function ToolbarHarness({ onValueChange }: { onValueChange?: (value: string) => void }) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);

	return (
		<div>
			<input aria-label="技能搜索" />
			<button type="button" onClick={() => composerRef.current?.insertSkill("anysearch")}>
				anysearch
			</button>
			<button type="button" onClick={() => composerRef.current?.insertSkill("docx")}>
				docx
			</button>
			<StructuredComposer
				ref={composerRef}
				value={value}
				onChange={(nextValue) => {
					// 中文注释：模拟工具栏弹窗通过 ref 写入输入框，覆盖弹窗搜索框抢焦点后的插入链路。
					setValue(nextValue);
					onValueChange?.(nextValue);
				}}
				onSubmit={vi.fn()}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
				projectSkillOptions={[]}
			/>
		</div>
	);
}

function MentionRemoveHarness({ onValueChange }: { onValueChange?: (value: string) => void }) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);

	return (
		<div>
			<button type="button" onClick={() => composerRef.current?.insertSkill("anysearch")}>
				insert skill
			</button>
			<button type="button" onClick={() => composerRef.current?.insertAssistant("代码助手")}>
				insert assistant
			</button>
			<StructuredComposer
				ref={composerRef}
				value={value}
				onChange={(nextValue) => {
					// 中文注释：通过真实 token x 入口验证删除后 value 与 mention DOM 同步清理。
					setValue(nextValue);
					onValueChange?.(nextValue);
				}}
				onSubmit={vi.fn()}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
				projectSkillOptions={[]}
			/>
		</div>
	);
}

function ActionBarHarness({ onValueChange }: { onValueChange?: (value: string) => void }) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const projectSkillOptions: ComposerSkillOption[] = [
		{
			code: "anysearch",
			label: "anysearch",
			description: "search",
			keywords: [],
		},
		{
			code: "docx",
			label: "docx",
			description: "docx",
			keywords: [],
		},
	];

	return (
		<div>
			<StructuredComposer
				ref={composerRef}
				value={value}
				onChange={(nextValue) => {
					// 中文注释：用真实工具栏驱动受控输入框，覆盖弹窗已选区删除后的 token 同步。
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
			<ComposerActionBar
				inputValue={value}
				composerRef={composerRef}
				projectSkillOptions={projectSkillOptions}
			/>
		</div>
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
		await waitFor(() => {
			const mention = textbox.querySelector(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mention).toBeInTheDocument();
			expect(mention).toHaveAttribute("data-mention-label", "/doc-coauthoring");
		});
	});

	it("连续选择多个技能时第一个技能仍保持 mention 样式", async () => {
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
					{
						code: "weather",
						label: "weather",
						description: "weather",
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

		await user.keyboard("/");
		await user.click(await screen.findByText("/weather"));

		await waitFor(() => {
			const mentions = textbox.querySelectorAll(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			// 中文注释：这里直接验证两个技能节点仍是 mention，覆盖首个技能退化成纯文本的回归。
			expect(mentions).toHaveLength(2);
			expect(
				Array.from(mentions).map((mention) => mention.getAttribute("data-mention-label")),
			).toEqual(expect.arrayContaining(["/doc-coauthoring", "/weather"]));
		});
	});

	it("工具栏弹窗连续添加技能时第一个技能仍保持 mention 样式", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<ToolbarHarness onValueChange={handleValueChange} />);

		const searchInput = screen.getByRole("textbox", { name: "技能搜索" });
		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(searchInput);
		await user.keyboard("anysearch");
		await user.click(screen.getByRole("button", { name: "anysearch" }));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/anysearch ");
		});
		await waitFor(() => {
			const mention = textbox.querySelector(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mention).toHaveAttribute("data-mention-label", "/anysearch");
		});

		await user.click(searchInput);
		await user.click(screen.getByRole("button", { name: "docx" }));

		await waitFor(() => {
			const mentions = textbox.querySelectorAll(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mentions).toHaveLength(2);
			expect(
				Array.from(mentions).map((mention) => mention.getAttribute("data-mention-label")),
			).toEqual(expect.arrayContaining(["/anysearch", "/docx"]));
		});
	});

	it("技能 mention 上的 x 可以删除已选技能", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<MentionRemoveHarness onValueChange={handleValueChange} />);

		await user.click(screen.getByRole("button", { name: "insert skill" }));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/anysearch ");
		});

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		const removeButton = textbox.querySelector('[data-mention-remove="true"]');
		expect(removeButton).toBeInstanceOf(HTMLElement);

		await user.click(removeButton as HTMLElement);

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("");
			expect(textbox.querySelector('[data-mention-node="true"]')).not.toBeInTheDocument();
		});
	});

	it("AI 员工 mention 上的 x 可以删除已选员工", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<MentionRemoveHarness onValueChange={handleValueChange} />);

		await user.click(screen.getByRole("button", { name: "insert assistant" }));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("@代码助手 ");
		});

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		const removeButton = textbox.querySelector('[data-mention-remove="true"]');
		expect(removeButton).toBeInstanceOf(HTMLElement);

		await user.click(removeButton as HTMLElement);

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("");
			expect(textbox.querySelector('[data-mention-node="true"]')).not.toBeInTheDocument();
		});
	});

	it("工具栏弹窗不展示已选技能且仍可继续添加其他技能", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<ActionBarHarness onValueChange={handleValueChange} />);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(screen.getByRole("button", { name: "添加技能" }));
		await user.click(await screen.findByText("/anysearch"));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/anysearch ");
		});
		expect(screen.queryByText("已选技能")).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "移除技能 anysearch" })).not.toBeInTheDocument();

		await user.click(await screen.findByText("/docx"));
		await waitFor(() => {
			const mentions = textbox.querySelectorAll(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mentions).toHaveLength(2);
			expect(mentions[0]).toHaveAttribute("data-mention-label", "/anysearch");
			expect(mentions[1]).toHaveAttribute("data-mention-label", "/docx");
		});
	});
});
