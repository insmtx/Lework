import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	sendTaskRoomMessage: vi.fn(),
	uploadLoose: vi.fn(),
}));

vi.mock("@lework/store", () => ({
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			projects: [{ id: "p1", name: "Payroll", tasks: [] }],
		}),
	useChatStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			sendTaskRoomMessage: mocks.sendTaskRoomMessage,
		}),
	projectFileApi: {
		uploadLoose: (...args: unknown[]) => mocks.uploadLoose(...args),
		upload: (...args: unknown[]) => mocks.uploadLoose(...args),
	},
}));

vi.mock("../input/BidComparisonConfigDialog", () => ({
	ProjectFilePicker: ({
		open,
		titleOverride,
		onConfirm,
	}: {
		open: boolean;
		titleOverride: string;
		onConfirm: (files: unknown[]) => void;
	}) =>
		open ? (
			<div role="dialog" aria-label={titleOverride}>
				<button
					type="button"
					onClick={() =>
						onConfirm([
							{
								name: "roster.xlsx",
								publicId: "file-1",
								projectId: "p1",
								storageUri: "s3://roster.xlsx",
								mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
								size: 10,
							},
						])
					}
				>
					确认项目文件
				</button>
			</div>
		) : null,
}));

vi.mock("./file-preview-store", () => ({
	filePreviewActions: { open: vi.fn() },
}));

import { PayrollTaskPanel } from "./PayrollTaskPanel";
import * as payrollService from "./payroll-service-client";

describe("PayrollTaskPanel", () => {
	afterEach(() => {
		cleanup();
		vi.clearAllMocks();
		vi.restoreAllMocks();
	});

	it("窗口内可编辑基准并直接上传考勤核算", async () => {
		vi.spyOn(payrollService, "listPayrollPeople").mockResolvedValue({
			total: 0,
			items: [],
			initialized: false,
		});
		vi.spyOn(payrollService, "listPayrollSalary").mockResolvedValue({
			total: 0,
			items: [],
			initialized: false,
		});

		const user = userEvent.setup();
		mocks.uploadLoose.mockImplementation(async ({ file }: { file: File }) => ({
			data: { public_id: `id-${file.name}`, storage_uri: `s3://${file.name}` },
		}));
		mocks.sendTaskRoomMessage.mockResolvedValue({
			project_id: "p1",
			task_id: "t1",
			session_id: "s1",
		});

		render(
			<PayrollTaskPanel open onOpenChange={vi.fn()} projectId="p1" taskId="t1" sessionId="s1" />,
		);

		expect(await screen.findByText("人员工资基准")).toBeInTheDocument();
		expect(await screen.findByText("历史工资表")).toBeInTheDocument();
		expect(screen.getByRole("heading", { name: "人员底表" })).toBeInTheDocument();
		const attendanceSection = screen
			.getAllByText(/当月考勤/)
			.map((element) => element.closest("section"))
			.find((section) => section !== null);
		const rosterSection = screen.getByRole("heading", { name: "人员底表" }).closest("section");
		const salarySection = screen.getByText("历史工资表", { exact: true }).closest("section");
		const startButton = screen.getByRole("button", { name: "开始核算" });
		const footer = startButton.closest('[data-slot="dialog-footer"]');
		if (!attendanceSection || !rosterSection || !salarySection || !footer) {
			throw new Error("Payroll preparation sections or footer are missing");
		}
		expect(
			attendanceSection.compareDocumentPosition(rosterSection) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(
			rosterSection.compareDocumentPosition(salarySection) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(
			salarySection.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(footer).toContainElement(startButton);
		expect(screen.getByLabelText("上传人员底表文件")).toBeInTheDocument();
		expect(screen.getByLabelText("上传历史工资文件")).toBeInTheDocument();
		expect(screen.getAllByText("可上传多份，或从项目文件中选择")).toHaveLength(1);
		expect(screen.queryByText("确认人员基准无误")).not.toBeInTheDocument();
		expect(startButton).toBeDisabled();
		expect(screen.getAllByRole("button", { name: "项目文件" })).toHaveLength(3);

		const rosterProjectButton = within(rosterSection).getByRole("button", { name: "项目文件" });
		if (!rosterProjectButton) throw new Error("人员底表项目文件入口不存在");
		expect(rosterProjectButton).toBeInTheDocument();

		const attendanceInput = screen.getByLabelText("上传当月考勤文件") as HTMLInputElement;
		await user.upload(
			attendanceInput,
			new File(["att"], "当月考勤.pdf", { type: "application/pdf" }),
		);
		expect(startButton).toBeEnabled();
		expect(footer).toContainElement(startButton);
		await user.click(startButton);

		await vi.waitFor(() => {
			expect(mocks.sendTaskRoomMessage).toHaveBeenCalled();
		});
		expect(mocks.sendTaskRoomMessage.mock.calls[0]?.[1]).toEqual(
			expect.objectContaining({ scene: "salary_accounting", sessionId: "s1" }),
		);
		expect(mocks.sendTaskRoomMessage.mock.calls[0]?.[2]).toEqual([
			expect.objectContaining({ attachmentRole: "attendance" }),
		]);
	});
});
