import "@testing-library/jest-dom/vitest";
import type { AutomationItem } from "@leros/store";
import { skillChipMarkup } from "@leros/store";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AutomationFormDialog } from "./AutomationFormDialog";

// Base UI 弹窗会内联切换 pointer-events，jsdom 下无真实命中测试导致偶发 flaky；
// 这里关闭 user-event 的 pointer-events 检查，仅验证选择逻辑而非 CSS 指针行为。
const setupUser = () => userEvent.setup({ pointerEventsCheck: 0 });

// cmdk（Command 弹层）依赖 ResizeObserver / scrollIntoView，jsdom 未实现，测试中打桩。
if (typeof window !== "undefined" && !("ResizeObserver" in window)) {
	class ResizeObserverStub {
		observe() {
			// noop：jsdom 未实现 ResizeObserver
		}
		unobserve() {
			// noop
		}
		disconnect() {
			// noop
		}
	}
	// @ts-expect-error 只读属性注入
	window.ResizeObserver = ResizeObserverStub;
}
if (typeof Element !== "undefined" && !Element.prototype.scrollIntoView) {
	// jsdom 缺少 scrollIntoView，打桩
	Element.prototype.scrollIntoView = () => {
		// noop
	};
}

// 项目列表由测试按需覆写
let mockProjects: { id: string; name: string }[] = [];

const { skillPickerMock, storeMock } = vi.hoisted(() => ({
	skillPickerMock: {
		skillOptions: [
			{
				code: "daily-report",
				label: "日报 Skill",
				description: "生成日报",
				keywords: ["日报", "daily-report"],
				source: "organization" as const,
			},
		],
		skillsLoading: false,
	},
	storeMock: {
		createAutomation: vi.fn(async () => ({ ok: true, status: undefined })),
		updateAutomation: vi.fn(async () => ({ ok: true, status: undefined })),
		fetchProjects: vi.fn(async () => true),
	},
}));

vi.mock("@leros/store", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@leros/store")>();
	return {
		...actual,
		useAutomationStore: (selector: (state: unknown) => unknown) =>
			selector({
				createAutomation: storeMock.createAutomation,
				updateAutomation: storeMock.updateAutomation,
			}),
		useLayoutStore: (selector: (state: unknown) => unknown) =>
			selector({
				projects: mockProjects,
				fetchProjects: storeMock.fetchProjects,
			}),
	};
});

vi.mock("../input/useComposerSkillOptions", () => ({
	useComposerSkillOptions: () => skillPickerMock,
}));

function renderDialog(props: Partial<Parameters<typeof AutomationFormDialog>[0]> = {}) {
	return render(<AutomationFormDialog open onOpenChange={vi.fn()} {...props} />);
}

/** 点击预设周期下拉中的某个选项 */
async function pickPreset(user: ReturnType<typeof userEvent.setup>, optionText: string) {
	await user.click(screen.getByRole("combobox"));
	await user.click(await screen.findByText(optionText));
}

/** 在预设周期下拉中选择“每周执行” */
async function switchToWeekly(user: ReturnType<typeof userEvent.setup>) {
	await pickPreset(user, "每周执行");
	// 菜单关闭后应出现“选择星期”按钮触发器
	await screen.findByRole("button", { name: "选择星期" });
}

/** 在预设周期下拉中选择“每月执行” */
async function switchToMonthly(user: ReturnType<typeof userEvent.setup>) {
	await pickPreset(user, "每月执行");
	await screen.findByRole("button", { name: "选择日期" });
}

beforeEach(() => {
	mockProjects = [];
	storeMock.fetchProjects.mockReset();
	storeMock.fetchProjects.mockResolvedValue(true);
});

afterEach(() => {
	cleanup();
	storeMock.createAutomation.mockClear();
	storeMock.updateAutomation.mockClear();
});

describe("AutomationFormDialog 周/月多选", () => {
	it("每周可连续勾选两项菜单不关闭，触发框展示名称摘要", async () => {
		const user = setupUser();
		renderDialog();
		await switchToWeekly(user);

		const weekdayTrigger = screen.getByRole("button", { name: "选择星期" });
		await user.click(weekdayTrigger);

		const menu = await screen.findByRole("menu");
		// 勾选 周二（默认周一已选，共 2 项）
		await user.click(within(menu).getByText("周二"));
		// 菜单保持打开（多选不关闭）
		expect(screen.getByRole("menu")).toBeInTheDocument();

		// 触发框显示 “周一、周二”
		expect(weekdayTrigger).toHaveTextContent("周一、周二");
	});

	it("三项及以上触发框显示选中数量", async () => {
		const user = setupUser();
		renderDialog();
		await switchToWeekly(user);

		const weekdayTrigger = screen.getByRole("button", { name: "选择星期" });
		await user.click(weekdayTrigger);
		const menu = await screen.findByRole("menu");

		// 勾选周二、周三、周四（配合默认周一共 4 项）
		await user.click(within(menu).getByText("周二"));
		await user.click(within(menu).getByText("周三"));
		await user.click(within(menu).getByText("周四"));

		expect(weekdayTrigger).toHaveTextContent("已选 4 项");
	});

	it("唯一选中项不可取消，多选后可移除其中一项", async () => {
		const user = setupUser();
		renderDialog();
		await switchToWeekly(user);

		const weekdayTrigger = screen.getByRole("button", { name: "选择星期" });
		await user.click(weekdayTrigger);
		const menu = await screen.findByRole("menu");

		// 默认只有周一（唯一项），点击不应取消
		const mondayItem = within(menu).getByText("周一");
		await user.click(mondayItem);
		expect(weekdayTrigger).toHaveTextContent("周一");

		// 勾选周二后，可取消周一，剩周二
		await user.click(within(menu).getByText("周二"));
		expect(weekdayTrigger).toHaveTextContent("周一、周二");
		await user.click(within(menu).getByText("周一"));
		await waitFor(() => expect(weekdayTrigger).toHaveTextContent("周二"));
	});

	it("周期摘要完整列出所选选项", async () => {
		const user = setupUser();
		renderDialog();
		await switchToWeekly(user);

		const weekdayTrigger = screen.getByRole("button", { name: "选择星期" });
		await user.click(weekdayTrigger);
		const menu = await screen.findByRole("menu");
		await user.click(within(menu).getByText("周二"));

		expect(screen.getByText(/每周.*周一、周二/)).toBeInTheDocument();
	});

	it("每月可勾选多个日期且≥3项显示数量，周期摘要完整列出", async () => {
		const user = setupUser();
		renderDialog();
		await switchToMonthly(user);

		const dateTrigger = screen.getByRole("button", { name: "选择日期" });
		await user.click(dateTrigger);
		const menu = await screen.findByRole("menu");

		// 勾选 15、20、28（配合默认 1 日共 4 项）
		await user.click(within(menu).getByText("15日"));
		await user.click(within(menu).getByText("20日"));
		await user.click(within(menu).getByText("28日"));
		expect(dateTrigger).toHaveTextContent("已选 4 项");

		// 周期摘要完整列出
		expect(screen.getByText(/每月1日、15日、20日、28日/)).toBeInTheDocument();
	});

	it("编辑回显所选数组并在保存时提交全部", async () => {
		const user = setupUser();
		const editTarget: AutomationItem = {
			publicId: "auto-1",
			name: "热点日报",
			instruction: "生成日报",
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: false,
			createdAt: 0,
			updatedAt: 0,
			formConfig: {
				mode: "calendar",
				timezone: "Asia/Shanghai",
				calendar: { preset: "weekly", hour: 8, minute: 30, days_of_week: [1, 3] },
			},
		};
		renderDialog({ editTarget });

		// 回显触发框应含 周一、周三
		const weekdayTrigger = await screen.findByRole("button", { name: "选择星期" });
		expect(weekdayTrigger).toHaveTextContent("周一、周三");

		// 保存
		await user.click(screen.getByRole("button", { name: "保存" }));

		expect(storeMock.updateAutomation).toHaveBeenCalledWith(
			"auto-1",
			expect.objectContaining({
				schedule_mode: "calendar",
				schedule: expect.objectContaining({
					calendar: expect.objectContaining({ days_of_week: [1, 3] }),
				}),
			}),
		);
	});
});

describe("AutomationFormDialog Skill 指令", () => {
	it("输入 / 选择 Skill 后按 Enter 创建自动化", async () => {
		const user = setupUser();
		renderDialog();

		await user.type(screen.getByPlaceholderText("例如：AI 热点日报"), "日报自动化");
		const textbox = screen.getByRole("textbox", { name: /输入 \/ 选择技能/ });
		await user.click(textbox);
		await user.keyboard("/");
		const picker = await screen.findByRole("dialog", { name: "选择技能" });
		expect(picker).toHaveClass("w-[min(300px,calc(100vw-2rem))]", "rounded-xl");
		expect(picker.closest("[data-slot='dialog-content']")).toBeNull();
		expect(picker.parentElement).toHaveAttribute("data-skill-picker-positioner");
		await user.click(await screen.findByText("日报 Skill"));

		await waitFor(() => {
			expect(textbox.querySelector('[data-mention-kind="skill"]')).toBeInTheDocument();
			expect(textbox).toHaveTextContent("日报 Skill");
		});
		await waitFor(() => expect(document.activeElement).toBe(textbox));
		await user.keyboard("{Enter}");

		await waitFor(() =>
			expect(storeMock.createAutomation).toHaveBeenCalledWith(
				expect.objectContaining({
					name: "日报自动化",
					instruction: skillChipMarkup("daily-report", "日报 Skill"),
				}),
			),
		);
	});

	it("编辑已有指令时恢复已知 Skill token", async () => {
		const editTarget: AutomationItem = {
			publicId: "auto-skill",
			name: "日报自动化",
			instruction: `${skillChipMarkup("daily-report", "日报 Skill")} 生成日报`,
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: false,
			createdAt: 0,
			updatedAt: 0,
		};
		renderDialog({ editTarget });

		const textbox = await screen.findByRole("textbox", { name: /输入 \/ 选择技能/ });
		await waitFor(() => {
			expect(textbox.querySelector('[data-mention-kind="skill"]')).toBeInTheDocument();
		});
	});
});

describe("AutomationFormDialog 关联项目选择", () => {
	it("创建默认显示“新项目”，提交不携带 project_public_id", async () => {
		mockProjects = [
			{ id: "prj-1", name: "项目甲" },
			{ id: "prj-2", name: "项目乙" },
		];
		const user = setupUser();
		renderDialog();

		// 默认触发框
		const trigger = screen.getByRole("button", { name: /^新项目$/ });
		expect(trigger).toBeInTheDocument();

		// 填写必填项后保存
		await user.type(screen.getByPlaceholderText(/例如：AI 热点日报/), "自动化一");
		await user.type(screen.getByRole("textbox", { name: /输入 \/ 选择技能/ }), "生成日报");
		await user.click(screen.getByRole("button", { name: "创建" }));

		expect(storeMock.createAutomation).toHaveBeenCalledWith(
			expect.not.objectContaining({ project_public_id: expect.anything() }),
		);
	});

	it("创建时选择已有项目则提交携带 project_public_id", async () => {
		mockProjects = [
			{ id: "prj-1", name: "项目甲" },
			{ id: "prj-2", name: "项目乙" },
		];
		const user = setupUser();
		renderDialog();

		await user.type(screen.getByPlaceholderText(/例如：AI 热点日报/), "自动化一");
		await user.type(screen.getByRole("textbox", { name: /输入 \/ 选择技能/ }), "生成日报");

		// 打开项目选择器并选中“项目乙”
		const trigger = screen.getByRole("button", { name: /^新项目$/ });
		await user.click(trigger);
		await user.click(await screen.findByText("项目乙"));

		await user.click(screen.getByRole("button", { name: "创建" }));
		expect(storeMock.createAutomation).toHaveBeenCalledWith(
			expect.objectContaining({ project_public_id: "prj-2" }),
		);
	});

	it("编辑回显当前关联项目名", async () => {
		mockProjects = [{ id: "prj-1", name: "项目甲" }];
		const editTarget: AutomationItem = {
			publicId: "auto-1",
			name: "热点日报",
			instruction: "生成日报",
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: false,
			projectPublicId: "prj-1",
			projectName: "项目甲",
			createdAt: 0,
			updatedAt: 0,
		};
		renderDialog({ editTarget });

		expect(screen.getByRole("button", { name: /项目甲/ })).toBeInTheDocument();
	});

	it("完整展示列表接口返回的已有项目，并与新项目分组", async () => {
		mockProjects = [
			{ id: "prj-1", name: "项目甲" },
			{ id: "prj-2", name: "项目乙" },
		];
		const user = setupUser();
		renderDialog();

		const trigger = screen.getByRole("button", { name: /^新项目$/ });
		await user.click(trigger);

		expect(await screen.findByText("已有项目")).toBeInTheDocument();
		expect(screen.getByText("项目甲")).toBeInTheDocument();
		expect(screen.getByText("项目乙")).toBeInTheDocument();
	});

	it("编辑时当前关联项目已失效则提示改选", async () => {
		mockProjects = [];
		// 当前关联 prj-old 不在项目列表；即使列表为空也应提示不可访问。
		const editTarget: AutomationItem = {
			publicId: "auto-1",
			name: "热点日报",
			instruction: "生成日报",
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: false,
			projectPublicId: "prj-old",
			projectName: "旧项目",
			createdAt: 0,
			updatedAt: 0,
		};
		renderDialog({ editTarget });

		expect(await screen.findByText(/当前关联项目已不可访问/)).toBeInTheDocument();
	});

	it("首次打开时已有项目仍在加载会展示加载提示", async () => {
		let resolveFetch: ((value: boolean) => void) | undefined;
		storeMock.fetchProjects.mockImplementationOnce(
			() =>
				new Promise<boolean>((resolve) => {
					resolveFetch = resolve;
				}),
		);
		const user = setupUser();
		renderDialog();

		await user.click(screen.getByRole("button", { name: /^新项目$/ }));
		expect(await screen.findByText("正在加载已有项目...")).toBeInTheDocument();

		resolveFetch?.(true);
	});

	it("编辑切回默认新项目时提交 project_public_id 空串", async () => {
		mockProjects = [
			{ id: "prj-1", name: "项目甲" },
			{ id: "prj-2", name: "项目乙" },
		];
		const user = setupUser();
		const editTarget: AutomationItem = {
			publicId: "auto-1",
			name: "热点日报",
			instruction: "生成日报",
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: false,
			projectPublicId: "prj-1",
			projectName: "项目甲",
			createdAt: 0,
			updatedAt: 0,
		};
		renderDialog({ editTarget });

		// 已关联项目甲；打开选择器切回"新项目"
		const trigger = screen.getByRole("button", { name: /项目甲/ });
		await user.click(trigger);
		await user.click(await screen.findByRole("option", { name: "新项目" }));

		await user.click(screen.getByRole("button", { name: "保存" }));
		expect(storeMock.updateAutomation).toHaveBeenCalledWith(
			"auto-1",
			expect.objectContaining({ project_public_id: "" }),
		);
	});

	it("存在活动执行时禁用项目选择器", async () => {
		mockProjects = [{ id: "prj-1", name: "项目甲" }];
		const editTarget: AutomationItem = {
			publicId: "auto-1",
			name: "热点日报",
			instruction: "生成日报",
			enabled: true,
			scheduleMode: "schedule",
			timezone: "Asia/Shanghai",
			assistantId: 1,
			summary: "",
			hasActiveExecution: true,
			createdAt: 0,
			updatedAt: 0,
		};
		renderDialog({ editTarget });

		const trigger = screen.getByRole("button", { name: /新项目|项目/ });
		expect(trigger).toBeDisabled();
	});
});
