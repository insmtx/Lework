import { expect, it } from "vitest";
import type { ComponentProps } from "react";
import { ConsoleShell } from "./console-shell";
import {
	AutomationDetailRoute,
	AutomationRoute,
	EmptyRoute,
	NewTaskRoute,
	OrgAdminRoute,
	PayrollRoute,
	ProjectRoute,
	ProjectsHubRoute,
	SkillDetailRoute,
	SkillsRoute,
	TaskDetailRoute,
	WorkbenchRoute,
} from "./pages";
import { createConsoleNavigation } from "./navigation";

const navigation = createConsoleNavigation({ pathname: "/chat", push: () => {} });

// 页面包装只做参数映射，真正的渲染依赖登录态与接口；这里校验宿主必须提供的
// props 形状，既能在模块缺失时失败，也不引入网络桩或 DOM 环境。
it("exposes the shared shell and page props", () => {
	const shell: ComponentProps<typeof ConsoleShell> = { navigation };
	const cases: unknown[] = [
		{ navigation } satisfies ComponentProps<typeof NewTaskRoute>,
		{ navigation } satisfies ComponentProps<typeof WorkbenchRoute>,
		{ navigation } satisfies ComponentProps<typeof PayrollRoute>,
		{ navigation } satisfies ComponentProps<typeof ProjectsHubRoute>,
		{
			projectId: "p",
			tab: "tasks",
			navigation,
			onTabChange: () => {},
		} satisfies ComponentProps<typeof ProjectRoute>,
		{
			projectId: "p",
			taskId: "t",
			sessionId: "s",
			navigation,
		} satisfies ComponentProps<typeof TaskDetailRoute>,
		{ section: "profile", navigation } satisfies ComponentProps<typeof OrgAdminRoute>,
		{ navigation } satisfies ComponentProps<typeof SkillsRoute>,
		{ skillId: "s", navigation } satisfies ComponentProps<typeof SkillDetailRoute>,
		{ navigation } satisfies ComponentProps<typeof AutomationRoute>,
		{ publicId: "a", navigation } satisfies ComponentProps<typeof AutomationDetailRoute>,
	];

	expect(shell.navigation).toBe(navigation);
	expect(cases).toHaveLength(11);
	expect(EmptyRoute).toBeTypeOf("function");
});
