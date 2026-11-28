// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PendingView } from "./pending-view";
import { WorkspacesView } from "./workspaces-view";

const { mockClient } = vi.hoisted(() => ({
	mockClient: {
		listProjects: vi.fn(),
		listNodes: vi.fn(),
		getOverview: vi.fn(),
		listNodeForms: vi.fn(),
		setProjectPrimaryNode: vi.fn(),
	},
}));

vi.mock("../../context", () => ({
	useControlApi: () => ({ client: mockClient }),
}));

beforeEach(() => {
	mockClient.listProjects.mockResolvedValue([]);
	mockClient.listNodes.mockResolvedValue([]);
	mockClient.getOverview.mockResolvedValue([]);
	mockClient.listNodeForms.mockResolvedValue([]);
});

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

describe("WorkspacesView", () => {
	it("shows the empty state and opens the nodes page", async () => {
		const onOpenWorkspace = vi.fn();
		const onOpenNodes = vi.fn();

		render(<WorkspacesView onOpenWorkspace={onOpenWorkspace} onOpenNodes={onOpenNodes} />);

		expect(await screen.findByText("还没有工作区")).toBeTruthy();
		fireEvent.click(screen.getByText("管理节点"));
		expect(onOpenNodes).toHaveBeenCalledOnce();
	});

	it("opens a workspace by key", async () => {
		mockClient.listProjects.mockResolvedValue([
			{
				key: "proj-1",
				id: "p1",
				canonical: "/work/demo",
				nodes: [{ node_id: "node-1", online: true, last_seen: "2026-11-24T00:00:00Z" }],
				last_seen: "2026-11-24T00:00:00Z",
			},
		]);
		mockClient.listNodes.mockResolvedValue([
			{ id: "node-1", name: "本机", connected: true },
		]);
		const onOpenWorkspace = vi.fn();

		render(<WorkspacesView onOpenWorkspace={onOpenWorkspace} onOpenNodes={vi.fn()} />);

		await screen.findByText("demo");
		fireEvent.click(screen.getByText("打开"));

		expect(onOpenWorkspace).toHaveBeenCalledWith("proj-1");
	});

	it("opens the project file page from the secondary action", async () => {
		mockClient.listProjects.mockResolvedValue([
			{
				key: "proj-1",
				id: "p1",
				canonical: "/work/demo",
				nodes: [{ node_id: "node-1", online: true, last_seen: "2026-11-24T00:00:00Z" }],
				last_seen: "2026-11-24T00:00:00Z",
			},
		]);
		mockClient.listNodes.mockResolvedValue([{ id: "node-1", name: "本机", connected: true }]);
		const onBrowseWorkspace = vi.fn();

		render(
			<WorkspacesView
				onOpenWorkspace={vi.fn()}
				onOpenNodes={vi.fn()}
				onBrowseWorkspace={onBrowseWorkspace}
			/>,
		);

		await screen.findByText("demo");
		fireEvent.click(screen.getByText("文件"));

		expect(onBrowseWorkspace).toHaveBeenCalledWith("proj-1");
	});

	it("hides the file action when the host does not wire it", async () => {
		mockClient.listProjects.mockResolvedValue([
			{
				key: "proj-1",
				id: "p1",
				canonical: "/work/demo",
				nodes: [{ node_id: "node-1", online: true, last_seen: "2026-11-24T00:00:00Z" }],
				last_seen: "2026-11-24T00:00:00Z",
			},
		]);

		render(<WorkspacesView onOpenWorkspace={vi.fn()} onOpenNodes={vi.fn()} />);

		await screen.findByText("demo");
		expect(screen.queryByText("文件")).toBeNull();
	});
});

describe("PendingView", () => {
	it("shows the empty state", async () => {
		render(<PendingView onOpenSession={vi.fn()} />);

		expect(await screen.findByText("没有待处理")).toBeTruthy();
	});

	it("opens the session that owns a pending item", async () => {
		mockClient.getOverview.mockResolvedValue([
			{
				node_id: "node-1",
				name: "本机",
				connected: true,
				sessions: [
					{
						session_id: "sess-1",
						title: "修复登录",
						state: "waiting_permission",
						pending_permissions: 1,
						last_activity: "2026-11-24T00:00:00Z",
					},
				],
			},
		]);
		const onOpenSession = vi.fn();

		render(<PendingView onOpenSession={onOpenSession} />);

		await waitFor(() => expect(screen.getByText("修复登录")).toBeTruthy());
		fireEvent.click(screen.getByText("处理"));

		expect(onOpenSession).toHaveBeenCalledWith("node-1", "sess-1");
	});
});
