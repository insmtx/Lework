import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	sendWorkbenchMessage: vi.fn(),
	pluginListMCPPlatforms: vi.fn(),
	pluginListProject: vi.fn(),
	pluginConnectMCPPlatform: vi.fn(),
	fetchProjects: vi.fn(),
	fetchTasks: vi.fn(),
	uploadLoose: vi.fn(),
	openPreview: vi.fn(),
}));

const projectFileFixture = {
	name: "项目人员表.xlsx",
	publicId: "file-1",
	projectId: "project-1",
	storageUri: "s3://file-1",
	mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	size: 100,
};

vi.mock("./file-preview-store", () => ({
	filePreviewActions: {
		open: (...args: unknown[]) => mocks.openPreview(...args),
	},
}));

vi.mock("@lework/store", () => ({
	pluginApi: {
		listMCPPlatforms: (...args: unknown[]) => mocks.pluginListMCPPlatforms(...args),
		listProject: (...args: unknown[]) => mocks.pluginListProject(...args),
		connectMCPPlatform: (...args: unknown[]) => mocks.pluginConnectMCPPlatform(...args),
	},
	useLayoutStore: (selector: (state: Record<string, unknown>) => unknown) =>
		selector({
			projects: [{ id: "project-1", name: "项目一", tasks: [] }],
			fetchProjects: mocks.fetchProjects,
			fetchTasks: mocks.fetchTasks,
			sendWorkbenchMessage: mocks.sendWorkbenchMessage,
		}),
	projectFileApi: {
		uploadLoose: (...args: unknown[]) => mocks.uploadLoose(...args),
		upload: (...args: unknown[]) => mocks.uploadLoose(...args),
	},
}));

vi.mock("../auth", () => ({
	useAuth: () => ({
		isAuthenticated: true,
		requireAuth: (fn: () => void) => fn(),
	}),
}));

vi.mock("./ProjectTaskPicker", () => ({
	ProjectTaskPickerField: ({
		onSelect,
	}: {
		onSelect: (projectId: string, taskId: string) => void;
	}) => (
		<button type="button" onClick={() => onSelect("p1", "")}>
			项目任务选择
		</button>
	),
}));

vi.mock("../input/BidComparisonConfigDialog", () => ({
	ProjectFilePicker: ({
		open,
		onConfirm,
	}: {
		open: boolean;
		onConfirm: (files: (typeof projectFileFixture)[]) => void;
	}) =>
		open ? (
			<button type="button" onClick={() => onConfirm([projectFileFixture])}>
				确认选择项目文件
			</button>
		) : null,
}));

import { PayrollAccountingPage } from "./PayrollAccountingPage";
import * as payrollService from "./payroll-service-client";

describe("PayrollAccountingPage", () => {
	afterEach(() => {
		cleanup();
		vi.clearAllMocks();
		vi.restoreAllMocks();
	});

	it("仅考勤必填，开始核算走现有任务", async () => {
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
		mocks.pluginListMCPPlatforms.mockResolvedValue({
			data: {
				code: 0,
				data: {
					platforms: [
						{
							code: "payroll",
							name: "Payroll",
							description: "",
							mode: "mcp_only",
							auth_type: "none",
							auto_connect_supported: true,
							connected: true,
							plugin_id: "plugin-payroll",
						},
					],
				},
			},
		} as never);
		mocks.pluginListProject.mockResolvedValue({ data: { code: 0, data: [] } });
		const user = userEvent.setup();
		mocks.pluginListMCPPlatforms.mockResolvedValue({
			data: {
				code: 0,
				data: {
					platforms: [
						{
							code: "payroll",
							name: "Payroll",
							description: "",
							mode: "mcp_only",
							auth_type: "none",
							auto_connect_supported: true,
							connected: true,
							plugin_id: "plugin-payroll",
						},
					],
				},
			},
		});
		mocks.pluginListProject.mockResolvedValue({ data: { code: 0, data: [] } });
		mocks.uploadLoose.mockImplementation(async ({ file }: { file: File }) => ({
			data: { public_id: `id-${file.name}`, storage_uri: `s3://${file.name}` },
		}));
		mocks.sendWorkbenchMessage.mockResolvedValue({
			project_id: "p1",
			task_id: "t1",
			session_id: "s1",
		});
		const goToTaskDetail = vi.fn();

		render(
			<PayrollAccountingPage
				navigation={{
					currentPath: "/workbench/payroll",
					goToRoute: vi.fn(),
					goToPath: vi.fn(),
					goToProject: vi.fn(),
					goToProjectTasks: vi.fn(),
					goToTaskDetail,
					goToAutomationDetail: vi.fn(),
				}}
			/>,
		);

		expect(await screen.findByText("没有人员底表记录")).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "项目任务选择" }));
		expect(screen.getByText("应发工资")).toBeInTheDocument();
		expect(screen.queryByText("部门", { exact: true })).not.toBeInTheDocument();
		expect(screen.getByText("公司/部门", { exact: true })).toBeInTheDocument();
		expect(screen.getAllByText("人员底表", { exact: true })).toHaveLength(1);
		expect(screen.getByText("历史工资表")).toBeInTheDocument();
		const attendanceSection = screen.getByText(/当月考勤/).closest("section");
		const rosterSection = screen.getByText("人员底表", { exact: true }).closest("section");
		const salarySection = screen.getByText("历史工资表", { exact: true }).closest("section");
		if (!attendanceSection || !rosterSection || !salarySection) {
			throw new Error("Payroll sections are missing");
		}
		expect(
			attendanceSection.compareDocumentPosition(rosterSection) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(
			rosterSection.compareDocumentPosition(salarySection) & Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
		expect(screen.getByRole("button", { name: "开始核算" })).toBeDisabled();

		const attendanceInput = screen.getByLabelText("上传当月考勤文件") as HTMLInputElement;
		const attendanceFile = new File(["attendance"], "当月考勤.pdf", { type: "application/pdf" });
		await user.upload(attendanceInput, attendanceFile);
		expect(screen.getByTitle("预览文件")).toBeInTheDocument();
		await user.click(screen.getByTitle("预览文件"));
		expect(mocks.openPreview).toHaveBeenCalledWith(
			expect.objectContaining({ name: "当月考勤.pdf", url: expect.any(String) }),
		);

		await user.click(screen.getByRole("button", { name: "开始核算" }));

		await vi.waitFor(() => {
			expect(mocks.sendWorkbenchMessage).toHaveBeenCalled();
		});
		const args = mocks.sendWorkbenchMessage.mock.calls[0];
		expect(args?.[7]).toBe("salary_accounting");
		expect(args?.[4]).toBeUndefined();
		expect(args?.[3]).toHaveLength(1);
		expect(args?.[3]?.[0]?.attachmentRole).toBe("attendance");
		expect(String(args?.[0])).toContain("attendance-payroll Skill");
		expect(String(args?.[0])).toContain("等待我确认考勤事实后再匹配人员");
		expect(goToTaskDetail).toHaveBeenCalledWith("p1", "t1", "s1");
	});

	it("单独导入底表和历史工资走 Payroll API，不创建核算任务", async () => {
		const importRoster = vi.spyOn(payrollService, "importPayrollRoster").mockResolvedValue({
			task_id: 1,
			status: "success",
			total_count: 1,
			success_count: 1,
			failed_count: 0,
		});
		const importSalary = vi.spyOn(payrollService, "importPayrollSalary").mockResolvedValue({
			task_id: 2,
			status: "success",
			total_count: 1,
			success_count: 1,
			failed_count: 0,
		});
		vi.spyOn(payrollService, "listPayrollPeople").mockResolvedValue({
			total: 1,
			items: [],
			initialized: true,
		});
		vi.spyOn(payrollService, "listPayrollSalary").mockResolvedValue({
			total: 1,
			items: [],
			initialized: true,
		});
		const user = userEvent.setup();
		mocks.uploadLoose.mockImplementation(async ({ file }: { file: File }) => ({
			data: { public_id: `id-${file.name}`, storage_uri: `s3://${file.name}` },
		}));
		mocks.sendWorkbenchMessage.mockResolvedValue({
			project_id: "p1",
			task_id: "t1",
			session_id: "s-import",
		});
		const goToTaskDetail = vi.fn();

		render(
			<PayrollAccountingPage
				navigation={{
					currentPath: "/workbench/payroll",
					goToRoute: vi.fn(),
					goToPath: vi.fn(),
					goToProject: vi.fn(),
					goToProjectTasks: vi.fn(),
					goToTaskDetail,
					goToAutomationDetail: vi.fn(),
				}}
			/>,
		);

		await user.upload(
			screen.getByLabelText("上传人员底表文件") as HTMLInputElement,
			new File(["roster"], "人员底表.xlsx"),
		);
		await user.upload(
			screen.getByLabelText("上传历史工资文件") as HTMLInputElement,
			new File(["history"], "历史工资.xlsx"),
		);

		await vi.waitFor(() => expect(importRoster).toHaveBeenCalledTimes(1));
		expect(importSalary).toHaveBeenCalledTimes(1);
		expect(mocks.sendWorkbenchMessage).not.toHaveBeenCalled();
		expect(goToTaskDetail).not.toHaveBeenCalled();
	});

	it("project file entries import directly into their matching Payroll API", async () => {
		const importProject = vi.spyOn(payrollService, "importPayrollFileByRole").mockResolvedValue({
			task_id: 3,
			status: "success",
			total_count: 1,
			success_count: 1,
			failed_count: 0,
		});
		vi.spyOn(payrollService, "listPayrollPeople").mockResolvedValue({
			total: 1,
			items: [],
			initialized: true,
		});
		vi.spyOn(payrollService, "listPayrollSalary").mockResolvedValue({
			total: 1,
			items: [],
			initialized: true,
		});
		const user = userEvent.setup();
		render(<PayrollAccountingPage />);
		const rosterSection = screen.getByText("人员底表", { exact: true }).closest("section");
		const rosterProjectButton = rosterSection
			? within(rosterSection).getByRole("button", { name: "项目文件" })
			: null;
		if (!rosterProjectButton) throw new Error("roster project file button missing");
		await user.click(rosterProjectButton);
		await user.click(screen.getByRole("button", { name: "确认选择项目文件" }));
		await vi.waitFor(() =>
			expect(importProject).toHaveBeenCalledWith(
				"roster",
				expect.objectContaining({ publicId: "file-1" }),
			),
		);
		const salarySection = screen.getByText("历史工资表", { exact: true }).closest("section");
		const historicalProjectButton = salarySection
			? within(salarySection).getByRole("button", { name: "项目文件" })
			: null;
		if (!historicalProjectButton) throw new Error("historical salary project file button missing");
		await user.click(historicalProjectButton);
		await user.click(screen.getByRole("button", { name: "确认选择项目文件" }));
		await vi.waitFor(() =>
			expect(importProject).toHaveBeenCalledWith(
				"historical_payroll",
				expect.objectContaining({ publicId: "file-1" }),
			),
		);
	});

	it("可按姓名筛选同名人员", async () => {
		vi.spyOn(payrollService, "listPayrollPeople").mockResolvedValue({
			total: 259,
			initialized: true,
			items: [
				{
					id: 1,
					name: "张伟",
					projectName: "瀚阅府",
					companyName: "六部",
					position: "施工员",
					employeeType: "易通外包",
					gender: "男",
				},
				{
					id: 2,
					name: "李娜",
					projectName: "杨职院",
					companyName: "六部",
					position: "资料员",
					employeeType: "在册",
					gender: "女",
				},
			],
		});
		const salaryList = vi.spyOn(payrollService, "listPayrollSalary").mockResolvedValue({
			total: 0,
			items: [],
			initialized: false,
		});
		const user = userEvent.setup();
		render(
			<PayrollAccountingPage
				navigation={{
					currentPath: "/workbench/payroll",
					goToRoute: vi.fn(),
					goToPath: vi.fn(),
					goToProject: vi.fn(),
					goToProjectTasks: vi.fn(),
					goToTaskDetail: vi.fn(),
					goToAutomationDetail: vi.fn(),
				}}
			/>,
		);

		expect(await screen.findByText("张伟")).toBeInTheDocument();
		await screen.findByText("没有历史工资记录");
		const salaryRequestCount = salaryList.mock.calls.length;
		expect(screen.getByText("李娜")).toBeInTheDocument();
		await user.type(screen.getByLabelText("筛选人员名称"), "张伟");
		expect(payrollService.listPayrollPeople).toHaveBeenLastCalledWith(
			expect.objectContaining({ name: "张伟" }),
		);
		await user.type(screen.getByLabelText("筛选项目"), "瀚阅府");
		await user.type(screen.getByLabelText("筛选公司或部门"), "六部");
		await vi.waitFor(() =>
			expect(payrollService.listPayrollPeople).toHaveBeenLastCalledWith(
				expect.objectContaining({
					name: "张伟",
					projectName: "瀚阅府",
					companyName: "六部",
					offset: 0,
				}),
			),
		);
		await user.click(screen.getByRole("button", { name: "2" }));
		await vi.waitFor(() =>
			expect(payrollService.listPayrollPeople).toHaveBeenLastCalledWith(
				expect.objectContaining({ offset: 10, limit: 10 }),
			),
		);
		const rosterPageSize = screen.getAllByLabelText("每页数量")[0];
		if (!rosterPageSize) throw new Error("roster page-size control missing");
		await user.selectOptions(rosterPageSize, "50");
		await vi.waitFor(() =>
			expect(payrollService.listPayrollPeople).toHaveBeenLastCalledWith(
				expect.objectContaining({ offset: 0, limit: 50 }),
			),
		);
		expect(salaryList).toHaveBeenCalledTimes(salaryRequestCount);
	});

	it("编辑人员字段会映射完整 roster DTO", async () => {
		const user = userEvent.setup();
		vi.spyOn(payrollService, "listPayrollPeople").mockResolvedValue({
			total: 1,
			initialized: true,
			items: [
				{
					id: 101,
					name: "张伟",
					projectName: "瀚阅府",
					companyName: "六部",
					position: "施工员",
					employeeType: "易通外包",
					gender: "男",
				},
			],
		});
		vi.spyOn(payrollService, "listPayrollSalary").mockResolvedValue({
			total: 0,
			items: [],
			initialized: false,
		});
		const updateSpy = vi.spyOn(payrollService, "updatePayrollPerson");
		render(
			<PayrollAccountingPage
				navigation={{
					currentPath: "/workbench/payroll",
					goToRoute: vi.fn(),
					goToPath: vi.fn(),
					goToProject: vi.fn(),
					goToProjectTasks: vi.fn(),
					goToTaskDetail: vi.fn(),
					goToAutomationDetail: vi.fn(),
				}}
			/>,
		);

		await screen.findByText("张伟");
		await user.click(screen.getByRole("button", { name: "编辑" }));
		const name = screen.getByLabelText("姓名");
		await user.clear(name);
		await user.type(name, "张小伟");
		await user.click(screen.getByRole("button", { name: "保存" }));

		await vi.waitFor(() => {
			expect(updateSpy).toHaveBeenCalledWith(
				expect.objectContaining({
					id: 101,
					name: "张小伟",
					employeeType: "易通外包",
					gender: "男",
				}),
			);
		});
	});
});
