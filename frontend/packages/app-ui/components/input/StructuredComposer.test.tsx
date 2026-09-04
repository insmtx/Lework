import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ComposerActionBar } from "./ComposerActionBar";
import {
	type ComposerAssistantOption,
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

function placeCaretAtEnd(element: HTMLElement) {
	const range = document.createRange();
	range.selectNodeContents(element);
	range.collapse(false);
	const selection = window.getSelection();
	selection?.removeAllRanges();
	selection?.addRange(range);
}

function TestHarness({
	skillOptions,
	assistantOptions,
	onValueChange,
	onAssistantPickerOpen,
	onSkillPickerOpen,
}: {
	skillOptions: ComposerSkillOption[];
	assistantOptions?: ComposerAssistantOption[];
	onValueChange?: (value: string) => void;
	onAssistantPickerOpen?: () => Promise<boolean> | undefined;
	onSkillPickerOpen?: () => void;
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
			assistantOptions={assistantOptions}
			onAssistantPickerOpen={onAssistantPickerOpen}
			skillOptions={skillOptions}
			onSkillPickerOpen={onSkillPickerOpen}
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
				skillOptions={[]}
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
				assistantOptions={[
					{
						id: "assistant-code",
						code: "code-assistant",
						name: "代码助手",
						description: "代码开发",
						avatarUrl: "https://example.com/code-assistant.png",
					},
				]}
				skillOptions={[]}
			/>
		</div>
	);
}

function SingleAssistantHarness({ onValueChange }: { onValueChange?: (value: string) => void }) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);

	return (
		<div>
			<button type="button" onClick={() => composerRef.current?.insertAssistant("浠ｇ爜鍔╂墜")}>
				insert first assistant
			</button>
			<button type="button" onClick={() => composerRef.current?.insertAssistant("浜у搧鍔╂墜")}>
				insert second assistant
			</button>
			<StructuredComposer
				ref={composerRef}
				value={value}
				onChange={(nextValue) => {
					// 中文注释：单选模式下再次选择 AI 员工时，应直接替换旧 token，而不是并列追加。
					setValue(nextValue);
					onValueChange?.(nextValue);
				}}
				onSubmit={vi.fn()}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="璇疯緭鍏?"
				isProjectVariant
				assistantSelectionMode="single"
				skillOptions={[]}
			/>
		</div>
	);
}

function ProjectTriggerHarness({
	onValueChange,
	onProjectTrigger,
}: {
	onValueChange?: (value: string) => void;
	onProjectTrigger?: (query: string) => void;
}) {
	const [value, setValue] = useState("");
	const clearTriggerRef = useRef<(() => void) | null>(null);
	const dismissTriggerRef = useRef<(() => void) | null>(null);

	return (
		<div>
			<button type="button" onClick={() => clearTriggerRef.current?.()}>
				select project
			</button>
			<button type="button" onClick={() => dismissTriggerRef.current?.()}>
				dismiss project menu
			</button>
			<StructuredComposer
				value={value}
				onChange={(nextValue) => {
					// 中文注释：项目触发器选择完成后应从正文中移除 # 搜索文本。
					setValue(nextValue);
					onValueChange?.(nextValue);
				}}
				onSubmit={vi.fn()}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
				skillOptions={[]}
				onProjectTrigger={(query, clearTrigger, dismissTrigger) => {
					clearTriggerRef.current = clearTrigger;
					dismissTriggerRef.current = dismissTrigger;
					onProjectTrigger?.(query);
				}}
			/>
		</div>
	);
}

function ActionBarHarness({
	onValueChange,
	onAssistantPickerOpen,
	onSkillPickerOpen,
	assistantOptions = [],
}: {
	onValueChange?: (value: string) => void;
	onAssistantPickerOpen?: () => Promise<boolean> | undefined;
	onSkillPickerOpen?: () => void;
	assistantOptions?: ComposerAssistantOption[];
}) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const skillOptions: ComposerSkillOption[] = [
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
				assistantOptions={assistantOptions}
				skillOptions={skillOptions}
			/>
			<ComposerActionBar
				inputValue={value}
				composerRef={composerRef}
				assistantOptions={assistantOptions}
				onAssistantPickerOpen={onAssistantPickerOpen}
				skillOptions={skillOptions}
				onSkillPickerOpen={onSkillPickerOpen}
			/>
		</div>
	);
}

function ActionBarCodeHarness({ onValueChange }: { onValueChange?: (value: string) => void }) {
	const [value, setValue] = useState("");
	const composerRef = useRef<StructuredComposerHandle | null>(null);
	const skillOptions: ComposerSkillOption[] = [
		{
			code: "market-code",
			label: "市场展示名称",
			description: "market",
			keywords: [],
			source: "marketplace",
		},
		{
			code: "builtin-code",
			label: "内置展示名称",
			description: "builtin",
			keywords: [],
			source: "builtin",
		},
	];

	return (
		<div>
			<StructuredComposer
				ref={composerRef}
				value={value}
				onChange={(nextValue) => {
					setValue(nextValue);
					onValueChange?.(nextValue);
				}}
				onSubmit={vi.fn()}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
				skillOptions={skillOptions}
			/>
			<ComposerActionBar inputValue={value} composerRef={composerRef} skillOptions={skillOptions} />
		</div>
	);
}

describe("StructuredComposer", () => {
	it("中文输入法组合中按 Enter 不会触发发送", () => {
		const handleSubmit = vi.fn();

		render(
			<StructuredComposer
				value=""
				onChange={vi.fn()}
				onSubmit={handleSubmit}
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		fireEvent.compositionStart(textbox);
		fireEvent.keyDown(textbox, { key: "Enter", code: "Enter", isComposing: true });

		expect(handleSubmit).not.toHaveBeenCalled();
	});

	it("submitDisabled 时按 Enter 不会触发发送", () => {
		const handleSubmit = vi.fn();

		render(
			<StructuredComposer
				value="已输入内容"
				onChange={vi.fn()}
				onSubmit={handleSubmit}
				submitDisabled
				onPasteFiles={vi.fn()}
				onFocus={vi.fn()}
				onBlur={vi.fn()}
				placeholder="请输入"
				isProjectVariant
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		fireEvent.keyDown(textbox, { key: "Enter", code: "Enter" });

		expect(handleSubmit).not.toHaveBeenCalled();
	});

	it("通过 / 选择技能后会补齐尾部空格", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(
			<TestHarness
				onValueChange={handleValueChange}
				skillOptions={[
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

	it("打开技能选择器时刷新一次，关闭后重开时再次刷新", async () => {
		const user = userEvent.setup();
		const onSkillPickerOpen = vi.fn();

		render(
			<TestHarness
				onSkillPickerOpen={onSkillPickerOpen}
				skillOptions={[{ code: "docx", label: "docx", description: "", keywords: [] }]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("/");
		await waitFor(() => expect(onSkillPickerOpen).toHaveBeenCalledTimes(1));

		await user.keyboard("{Enter}");
		await user.click(textbox);
		placeCaretAtEnd(textbox);
		await user.keyboard("/");
		await waitFor(() => expect(onSkillPickerOpen).toHaveBeenCalledTimes(2));
	});

	it("技能搜索框内继续输入时不重复刷新", async () => {
		const user = userEvent.setup();
		const onSkillPickerOpen = vi.fn();

		render(
			<TestHarness
				onSkillPickerOpen={onSkillPickerOpen}
				skillOptions={[{ code: "docx", label: "docx", description: "", keywords: [] }]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("/");
		await waitFor(() => expect(onSkillPickerOpen).toHaveBeenCalledTimes(1));

		await user.type(screen.getByPlaceholderText("搜索技能"), "doc");
		expect(onSkillPickerOpen).toHaveBeenCalledTimes(1);
	});

	it("输入框打开 AI 队友选择器时刷新并隐藏旧候选", async () => {
		const user = userEvent.setup();
		let resolveRefresh!: (succeeded: boolean) => void;
		const onAssistantPickerOpen = vi.fn(
			() =>
				new Promise<boolean>((resolve) => {
					resolveRefresh = resolve;
				}),
		);

		render(
			<TestHarness
				onAssistantPickerOpen={onAssistantPickerOpen}
				assistantOptions={[
					{
						id: "assistant-old",
						code: "assistant-old",
						name: "旧队友",
						description: "旧数据",
					},
				]}
				skillOptions={[]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("@");

		await screen.findByText("加载 AI 队友...");
		expect(onAssistantPickerOpen).toHaveBeenCalledTimes(1);
		expect(screen.queryByText("旧队友")).not.toBeInTheDocument();

		resolveRefresh(true);
		await screen.findByText("旧队友");
	});

	it("技能展示名称与 code 不同时插入展示名，并把 code 放进 token id", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(
			<TestHarness
				onValueChange={handleValueChange}
				skillOptions={[
					{
						code: "doc-coauthoring",
						label: "文档协作",
						description: "协作文档",
						keywords: [],
						source: "marketplace",
					},
				]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("/");
		expect(await screen.findByText("文档协作")).toBeInTheDocument();
		await user.keyboard("{Enter}");

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/文档协作 ");
		});
		await waitFor(() => {
			const mention = textbox.querySelector(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mention).toBeInTheDocument();
			expect(mention).toHaveAttribute("data-mention-label", "/文档协作");
			expect(mention).toHaveAttribute("data-mention-id", "doc-coauthoring");
			expect(mention).toHaveTextContent("文档协作");
		});
	});

	it("完整列表中的技能显示对应来源标签", async () => {
		const user = userEvent.setup();

		render(
			<TestHarness
				skillOptions={[
					{
						code: "organization",
						label: "组织技能",
						description: "",
						keywords: [],
						source: "organization",
					},
					{
						code: "marketplace",
						label: "市场技能",
						description: "",
						keywords: [],
						source: "marketplace",
					},
					{
						code: "builtin",
						label: "内置技能",
						description: "",
						keywords: [],
						source: "builtin",
					},
				]}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("/");

		expect(await screen.findByText("内置技能")).toBeInTheDocument();
		expect(screen.getAllByText("系统")).toHaveLength(1);
		expect(screen.getAllByText("市场")).toHaveLength(1);
		expect(screen.getAllByText("组织")).toHaveLength(1);
	});

	it("添加技能按钮使用 code 插入市场技能并显示来源标签", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<ActionBarCodeHarness onValueChange={handleValueChange} />);

		await user.click(screen.getByRole("button", { name: "添加技能" }));
		expect(await screen.findByText("市场展示名称")).toBeInTheDocument();
		expect(screen.getAllByText("系统")).toHaveLength(1);
		expect(screen.getAllByText("市场")).toHaveLength(1);
		await user.click(screen.getByText("市场展示名称"));

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/市场展示名称 ");
		});
		await waitFor(() => {
			const mention = screen
				.getByRole("textbox", { name: "请输入" })
				.querySelector('[data-mention-node="true"][data-mention-kind="skill"]');
			expect(mention).toHaveAttribute("data-mention-label", "/市场展示名称");
			expect(mention).toHaveAttribute("data-mention-id", "market-code");
			expect(mention).toHaveTextContent("市场展示名称");
		});

		await waitFor(() => {
			expect(screen.getByText("内置展示名称")).toBeInTheDocument();
			expect(screen.getAllByText("市场展示名称")).toHaveLength(1);
		});
	});

	it("工具栏打开技能弹窗时触发刷新", async () => {
		const user = userEvent.setup();
		const onSkillPickerOpen = vi.fn();

		render(<ActionBarHarness onSkillPickerOpen={onSkillPickerOpen} />);

		await user.click(screen.getByRole("button", { name: "添加技能" }));
		await waitFor(() => expect(onSkillPickerOpen).toHaveBeenCalledTimes(1));
	});

	it("点击召唤 AI 队友时刷新并隐藏旧候选", async () => {
		const user = userEvent.setup();
		let resolveRefresh!: (succeeded: boolean) => void;
		const onAssistantPickerOpen = vi.fn(
			() =>
				new Promise<boolean>((resolve) => {
					resolveRefresh = resolve;
				}),
		);

		render(
			<ActionBarHarness
				onAssistantPickerOpen={onAssistantPickerOpen}
				assistantOptions={[
					{
						id: "assistant-old",
						code: "assistant-old",
						name: "旧队友",
						description: "旧数据",
					},
				]}
			/>,
		);

		await user.click(screen.getByRole("button", { name: "召唤AI队友" }));
		await screen.findByText("加载 AI 队友...");
		expect(onAssistantPickerOpen).toHaveBeenCalledTimes(1);
		expect(screen.queryByText("旧队友")).not.toBeInTheDocument();

		resolveRefresh(true);
		await screen.findByText("旧队友");
	});

	it("连续选择多个技能时第一个技能仍保持 mention 样式", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(
			<TestHarness
				onValueChange={handleValueChange}
				skillOptions={[
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

		await user.click(textbox);
		await user.keyboard("/");
		await user.click((await screen.findAllByText("weather"))[0] as HTMLElement);

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

	it("在 Shift+Enter 换行后的空行添加技能时不会把浏览器占位换行当作正文", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<ActionBarHarness onValueChange={handleValueChange} />);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		// Chromium 在 contenteditable 的行尾会用第二个 <br> 保留光标位置。
		textbox.innerHTML = "已有文字<br><br>";
		fireEvent.input(textbox);
		await user.click(screen.getByRole("button", { name: "添加技能" }));
		await user.click(await screen.findByText("anysearch"));

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("已有文字\n/anysearch ");
			const lineBreak = textbox.querySelector("br");
			const mention = lineBreak?.nextSibling;
			expect(mention).toBeInstanceOf(HTMLElement);
			expect(mention as HTMLElement).toHaveAttribute("data-mention-node", "true");
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
		await waitFor(() => {
			expect(removeButton?.querySelector("img")).toHaveAttribute(
				"src",
				"https://example.com/code-assistant.png",
			);
		});

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
		await user.click(await screen.findByText("anysearch"));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/anysearch ");
		});
		expect(screen.queryByText("已选技能")).not.toBeInTheDocument();

		const remainingSkill = (await screen.findAllByText("docx"))[0] as HTMLElement;
		expect(screen.queryAllByText("anysearch")).toHaveLength(1);
		await user.click(remainingSkill);
		await waitFor(() => {
			const mentions = textbox.querySelectorAll(
				'[data-mention-node="true"][data-mention-kind="skill"]',
			);
			expect(mentions).toHaveLength(2);
			expect(mentions[0]).toHaveAttribute("data-mention-label", "/anysearch");
			expect(mentions[1]).toHaveAttribute("data-mention-label", "/docx");
		});
	});

	it("removes a mention and trailing space with one Backspace", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<MentionRemoveHarness onValueChange={handleValueChange} />);

		await user.click(screen.getByRole("button", { name: "insert skill" }));
		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("/anysearch ");
		});

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		placeCaretAtEnd(textbox);
		await user.keyboard("{Backspace}");

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("");
			expect(textbox.querySelector('[data-mention-node="true"]')).not.toBeInTheDocument();
		});
	});

	it("uses # as a project task trigger and clears it after selection", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();
		const handleProjectTrigger = vi.fn();

		render(
			<ProjectTriggerHarness
				onValueChange={handleValueChange}
				onProjectTrigger={handleProjectTrigger}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("#leros");

		await waitFor(() => {
			expect(handleProjectTrigger).toHaveBeenLastCalledWith("leros");
		});

		await user.click(screen.getByRole("button", { name: "select project" }));

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("");
		});
	});

	it("keeps # as plain text after dismissing the project menu", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();
		const handleProjectTrigger = vi.fn();

		render(
			<ProjectTriggerHarness
				onValueChange={handleValueChange}
				onProjectTrigger={handleProjectTrigger}
			/>,
		);

		const textbox = screen.getByRole("textbox", { name: "请输入" });
		await user.click(textbox);
		await user.keyboard("#leros");

		await waitFor(() => {
			expect(handleProjectTrigger).toHaveBeenLastCalledWith("leros");
		});
		handleProjectTrigger.mockClear();

		await user.click(screen.getByRole("button", { name: "dismiss project menu" }));
		await user.click(textbox);
		placeCaretAtEnd(textbox);
		await user.keyboard(" continue");

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("#leros continue");
		});
		expect(handleProjectTrigger).not.toHaveBeenCalled();
	});

	it("单选模式下再次选择 AI 员工会替换旧选择", async () => {
		const user = userEvent.setup();
		const handleValueChange = vi.fn();

		render(<SingleAssistantHarness onValueChange={handleValueChange} />);

		await user.click(screen.getByRole("button", { name: "insert first assistant" }));
		await user.click(screen.getByRole("button", { name: "insert second assistant" }));

		await waitFor(() => {
			expect(handleValueChange).toHaveBeenLastCalledWith("@浜у搧鍔╂墜 ");
		});
		expect(handleValueChange).not.toHaveBeenLastCalledWith("@浠ｇ爜鍔╂墜 @浜у搧鍔╂墜 ");
	});
});
