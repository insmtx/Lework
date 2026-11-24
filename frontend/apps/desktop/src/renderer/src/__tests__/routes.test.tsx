// @vitest-environment jsdom

import { act, type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// 只验证路由表：共享页面替换成标记，避免在桌面测试里拉登录态和网络。
vi.mock("@lework/control-ui", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@lework/control-ui")>();
	const marker = (name: string) => () => <div>{name}</div>;

	return {
		...actual,
		ConsoleShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
		NewTaskRoute: marker("new-task"),
		WorkbenchRoute: marker("workbench"),
		PayrollRoute: marker("payroll"),
		ProjectsHubRoute: marker("projects"),
		SkillsRoute: marker("skills"),
		AutomationRoute: marker("automation"),
		NodesView: marker("nodes"),
		PendingView: marker("pending"),
		EmptyRoute: marker("empty"),
		ProjectRoute: ({ projectId, tab }: { projectId: string; tab?: string }) => (
			<div>{`project:${projectId}:${tab ?? "chat"}`}</div>
		),
		TaskDetailRoute: ({
			projectId,
			taskId,
			sessionId,
		}: {
			projectId: string;
			taskId: string;
			sessionId: string;
		}) => <div>{`task:${projectId}:${taskId}:${sessionId}`}</div>,
		OrgAdminRoute: ({ section }: { section: string }) => <div>{`org:${section}`}</div>,
		SkillDetailRoute: ({ skillId }: { skillId: string }) => <div>{`skill:${skillId}`}</div>,
		AutomationDetailRoute: ({ publicId }: { publicId: string }) => (
			<div>{`automation:${publicId}`}</div>
		),
		WorkspacesView: () => <div>workspaces</div>,
		WorkspaceConsole: ({
			projectKey,
			initialSessionID,
		}: {
			projectKey: string;
			initialSessionID?: string | null;
		}) => <div>{`workspace:${projectKey}:${initialSessionID ?? "none"}`}</div>,
		ControlConsole: ({
			initialNodeID,
			initialSessionID,
		}: {
			initialNodeID?: string | null;
			initialSessionID?: string | null;
		}) => <div>{`console:${initialNodeID ?? "none"}:${initialSessionID ?? "none"}`}</div>,
	};
});

vi.mock("../components/ControlPlaneProvider", () => ({
	ControlPlaneProvider: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

vi.mock("../components/DesktopSettingsPage", () => ({
	DesktopSettingsPage: () => <div>settings</div>,
}));

import { AppRoutes } from "../routes";

describe("desktop routes", () => {
	let container: HTMLDivElement;
	let root: Root;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	async function renderAt(path: string): Promise<string> {
		await act(async () => {
			root.render(
				<MemoryRouter key={path} initialEntries={[path]}>
					<AppRoutes />
				</MemoryRouter>,
			);
		});
		return container.textContent ?? "";
	}

	it("renders every shared console route", async () => {
		expect(await renderAt("/chat")).toContain("new-task");
		expect(await renderAt("/workbench")).toContain("workbench");
		expect(await renderAt("/workbench/payroll")).toContain("payroll");
		expect(await renderAt("/projects")).toContain("projects");
		expect(await renderAt("/projects/p1")).toContain("project:p1:chat");
		expect(await renderAt("/projects/p1/tasks")).toContain("project:p1:tasks");
		expect(await renderAt("/projects/p1/files")).toContain("project:p1:files");
		expect(await renderAt("/projects/p1/activity")).toContain("project:p1:activity");
		expect(await renderAt("/projects/p1/tasks/t1/sessions/s1")).toContain("task:p1:t1:s1");
		expect(await renderAt("/org/profile")).toContain("org:profile");
		expect(await renderAt("/org/models")).toContain("org:models");
		expect(await renderAt("/tasks")).toContain("empty");
		expect(await renderAt("/skills")).toContain("skills");
		expect(await renderAt("/skills/s1")).toContain("skill:s1");
		expect(await renderAt("/automation")).toContain("automation");
		expect(await renderAt("/automation/a1")).toContain("automation:a1");
		expect(await renderAt("/workspaces")).toContain("workspaces");
		expect(await renderAt("/nodes")).toContain("nodes");
		expect(await renderAt("/pending")).toContain("pending");
		expect(await renderAt("/settings")).toContain("settings");
	});

	it("redirects aliases to their canonical page", async () => {
		expect(await renderAt("/")).toContain("new-task");
		expect(await renderAt("/knowledge")).toContain("workbench");
		expect(await renderAt("/org")).toContain("org:profile");
		expect(await renderAt("/unknown")).toContain("new-task");
	});

	it("passes workspace and console query parameters through", async () => {
		expect(await renderAt("/workspaces/p1?session=s1")).toContain("workspace:p1:s1");
		expect(await renderAt("/workspaces/p1")).toContain("workspace:p1:none");
		expect(await renderAt("/workspaces/console?node=n1&session=s1")).toContain("console:n1:s1");
		expect(await renderAt("/workspaces/console")).toContain("console:none:none");
	});
});
