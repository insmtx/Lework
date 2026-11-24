import { describe, expect, it } from "vitest";
import { createConsoleNavigation } from "./navigation";

describe("createConsoleNavigation", () => {
	it("maps every goToRoute target to the web path", () => {
		const pushed: string[] = [];
		const nav = createConsoleNavigation({ pathname: "/chat", push: (p) => pushed.push(p) });

		nav.goToRoute("workspaces");
		nav.goToRoute("nodes");
		nav.goToRoute("pending");
		nav.goToRoute("settings");
		nav.goToProject("p1");
		nav.goToProjectTasks("p1");
		nav.goToTaskDetail("p1", "t1", "s1");
		nav.goToAutomationDetail("a1");

		expect(pushed).toEqual([
			"/workspaces",
			"/nodes",
			"/pending",
			"/settings",
			"/projects/p1",
			"/projects/p1/tasks",
			"/projects/p1/tasks/t1/sessions/s1",
			"/automation/a1",
		]);
	});

	it("supports OpenCode navigation like the web shell", () => {
		const nav = createConsoleNavigation({ pathname: "/chat", push: () => {} });
		expect(nav.supportsOpenCode).toBe(true);
		expect(nav.currentPath).toBe("/chat");
	});
});
