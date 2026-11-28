// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectFilesView } from "./project-files-view";

const { mockClient } = vi.hoisted(() => ({
	mockClient: { listProjects: vi.fn(), listNodes: vi.fn(), listFiles: vi.fn(), readFile: vi.fn() },
}));

vi.mock("../../context", () => ({ useControlApi: () => ({ client: mockClient }) }));

// jsdom 没有 matchMedia，useIsMobile 需要它。
Object.defineProperty(window, "matchMedia", {
	writable: true,
	value: (query: string) => ({
		matches: false,
		media: query,
		addEventListener: () => undefined,
		removeEventListener: () => undefined,
	}),
});

const project = {
	key: "proj-1",
	id: "p1",
	canonical: "/work/demo",
	primary_node_id: "node-1",
	nodes: [
		{ node_id: "node-1", online: true, last_seen: "", canonical: "/work/demo" },
		{ node_id: "node-2", online: true, last_seen: "", canonical: "/srv/demo" },
	],
	last_seen: "",
};

beforeEach(() => {
	mockClient.listProjects.mockResolvedValue([project]);
	mockClient.listNodes.mockResolvedValue([
		{ id: "node-1", name: "本机", connected: true },
		{ id: "node-2", name: "远端", connected: true },
	]);
	mockClient.listFiles.mockImplementation((_node: string, _dir: string, path: string) =>
		Promise.resolve(
			path === ""
				? [{ path: "src/", type: "directory" }]
				: path === "src"
					? [{ path: "app.tsx", type: "file" }]
					: [],
		),
	);
	mockClient.readFile.mockResolvedValue({
		path: "src/app.tsx",
		encoding: "utf-8",
		content: "console.log(1)",
		size: 14,
		truncated: false,
	});
});

afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

describe("ProjectFilesView", () => {
	it("loads the root and expands a directory only once", async () => {
		render(<ProjectFilesView projectKey="proj-1" onBack={vi.fn()} />);

		expect(await screen.findByText("src")).toBeTruthy();
		fireEvent.click(screen.getByText("src"));
		expect(await screen.findByText("app.tsx")).toBeTruthy();
		expect(mockClient.listFiles).toHaveBeenCalledWith("node-1", "/work/demo", "src");

		fireEvent.click(screen.getByText("src"));
		fireEvent.click(screen.getByText("src"));
		await waitFor(() => expect(mockClient.listFiles).toHaveBeenCalledTimes(2));
	});

	it("shows an empty state for an empty directory", async () => {
		mockClient.listFiles.mockImplementation((_node: string, _dir: string, path: string) =>
			Promise.resolve(path === "" ? [{ path: "empty/", type: "directory" }] : []),
		);

		render(<ProjectFilesView projectKey="proj-1" onBack={vi.fn()} />);

		fireEvent.click(await screen.findByText("empty"));
		expect(await screen.findByText("此目录为空")).toBeTruthy();
	});

	it("previews a file when clicked", async () => {
		render(<ProjectFilesView projectKey="proj-1" onBack={vi.fn()} />);

		fireEvent.click(await screen.findByText("src"));
		fireEvent.click(await screen.findByText("app.tsx"));

		expect(await screen.findByText("console.log(1)")).toBeTruthy();
		expect(mockClient.readFile).toHaveBeenCalledWith("node-1", "/work/demo", "src/app.tsx");
	});

	it("reloads with the selected node canonical", async () => {
		render(<ProjectFilesView projectKey="proj-1" onBack={vi.fn()} />);

		await screen.findByText("src");
		fireEvent.change(screen.getByLabelText("选择节点"), { target: { value: "node-2" } });

		await waitFor(() =>
			expect(mockClient.listFiles).toHaveBeenCalledWith("node-2", "/srv/demo", ""),
		);
	});

	it("shows an error when no project matches", async () => {
		mockClient.listProjects.mockResolvedValue([]);

		render(<ProjectFilesView projectKey="missing" onBack={vi.fn()} />);

		expect(await screen.findByText("没有找到这个工作区，它可能已从节点上移除。")).toBeTruthy();
		expect(screen.getByText("返回工作区")).toBeTruthy();
	});

	it("shows an error when every node is offline", async () => {
		mockClient.listProjects.mockResolvedValue([
			{ ...project, nodes: [{ node_id: "node-1", online: false, last_seen: "" }] },
		]);

		render(<ProjectFilesView projectKey="proj-1" onBack={vi.fn()} />);

		expect(await screen.findByText("没有在线节点，无法浏览文件。")).toBeTruthy();
		expect(mockClient.listFiles).not.toHaveBeenCalled();
	});
});
