import { describe, expect, it } from "vitest";
import {
	automationPath,
	consoleSessionPath,
	consoleRoutes,
	projectPath,
	taskPath,
	workspaceFilesPath,
	workspacePath,
} from "./routes";

describe("consoleRoutes", () => {
	it("keeps every route unique", () => {
		const paths = Object.values(consoleRoutes);
		expect(new Set(paths).size).toBe(paths.length);
	});

	it("encodes dynamic segments and keeps tab paths stable", () => {
		expect(projectPath("a b", "files")).toBe("/projects/a%20b/files");
		expect(taskPath("p", "t", "s 1")).toBe("/projects/p/tasks/t/sessions/s%201");
		expect(workspacePath("x/y")).toBe("/workspaces/x%2Fy");
		expect(workspaceFilesPath("x/y")).toBe("/workspaces/x%2Fy/files");
		expect(automationPath("a&b")).toBe("/automation/a%26b");
		expect(consoleSessionPath("n", "s")).toBe("/workspaces/console?node=n&session=s");
		expect(consoleRoutes.workspacesConsole).toBe("/workspaces/console");
	});
});
