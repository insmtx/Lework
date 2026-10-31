import "@testing-library/jest-dom/vitest";

import type { BackendTask, BackendTaskDeliverables, ProjectTask } from "@lework/store";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TaskAcceptancePanel } from "./TaskAcceptancePanel";

const mockAccept = vi.fn();
const mockReject = vi.fn();

vi.mock("@lework/store/api/taskApi", () => ({
	taskApi: {
		accept: (...args: unknown[]) => mockAccept(...args),
		reject: (...args: unknown[]) => mockReject(...args),
	},
}));

const pendingTask: ProjectTask = {
	id: "task_1",
	title: "修复空输入崩溃",
	meta: "",
	status: "in_progress",
	backendStatus: "pending_acceptance",
	expectedOutput: "修复补丁 + 单元测试",
	acceptanceCriteria: "go test ./... 通过",
};

const backendTask = {
	public_id: "task_1",
	title: "修复空输入崩溃",
	status: "completed",
} as BackendTask;

const deliverables: BackendTaskDeliverables = {
	task_id: "task_1",
	artifacts: [
		{
			artifact_id: "art_1",
			title: "修复报告",
			filename: "report.md",
			artifact_type: "document",
		},
	],
	files: [
		{
			file_public_id: "file_1",
			relative_path: "src/fix.go",
			resource_type: "artifact",
			version_no: 1,
			updated_at: "2026-10-30T00:00:00Z",
		},
	],
	git_commit: "",
	result_summary: "已修复并补充测试",
};

describe("TaskAcceptancePanel", () => {
	beforeEach(() => {
		mockAccept.mockReset();
		mockReject.mockReset();
	});

	afterEach(() => {
		cleanup();
	});

	it("renders expected output, acceptance criteria and deliverables", () => {
		render(<TaskAcceptancePanel task={pendingTask} deliverables={deliverables} />);

		expect(screen.getByText("待验收")).toBeInTheDocument();
		expect(screen.getByText("修复补丁 + 单元测试")).toBeInTheDocument();
		expect(screen.getByText("go test ./... 通过")).toBeInTheDocument();
		expect(screen.getByText("修复报告")).toBeInTheDocument();
		expect(screen.getByText("src/fix.go")).toBeInTheDocument();
	});

	it("shows an empty delivery hint when there are no artifacts or files", () => {
		render(
			<TaskAcceptancePanel
				task={pendingTask}
				deliverables={{ ...deliverables, artifacts: [], files: [] }}
			/>,
		);

		expect(screen.getByText("队友还没有交付，去会话里催一下")).toBeInTheDocument();
	});

	it("hides acceptance actions when task is not pending acceptance", () => {
		render(
			<TaskAcceptancePanel
				task={{ ...pendingTask, backendStatus: "in_progress" }}
				deliverables={deliverables}
			/>,
		);

		expect(screen.queryByRole("button", { name: "验收通过" })).not.toBeInTheDocument();
		expect(screen.queryByRole("button", { name: "驳回" })).not.toBeInTheDocument();
	});

	it("accepts the task and reports the updated task", async () => {
		mockAccept.mockResolvedValue({ data: { data: backendTask } });
		const onAccepted = vi.fn();
		render(
			<TaskAcceptancePanel
				task={pendingTask}
				deliverables={deliverables}
				onAccepted={onAccepted}
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "验收通过" }));

		await waitFor(() => expect(mockAccept).toHaveBeenCalledWith({ task_id: "task_1", note: "" }));
		expect(onAccepted).toHaveBeenCalledWith(backendTask);
	});

	it("requires a rejection note before calling the API", async () => {
		mockReject.mockResolvedValue({ data: { data: backendTask } });
		const onRejected = vi.fn();
		render(
			<TaskAcceptancePanel
				task={pendingTask}
				deliverables={deliverables}
				onRejected={onRejected}
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "驳回" }));
		const submit = screen.getByRole("button", { name: "确认驳回" });
		expect(submit).toBeDisabled();

		fireEvent.change(screen.getByPlaceholderText("请填写驳回意见"), {
			target: { value: "必须补充测试" },
		});
		expect(submit).not.toBeDisabled();
		fireEvent.click(submit);

		await waitFor(() =>
			expect(mockReject).toHaveBeenCalledWith({ task_id: "task_1", note: "必须补充测试" }),
		);
		expect(onRejected).toHaveBeenCalledWith(backendTask);
	});
});
