import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ authenticatedFetch: vi.fn() }));

vi.mock("@lework/store", () => ({
	API_BASE_URL: "http://lework.test/v1",
	authenticatedFetch: mocks.authenticatedFetch,
	projectFileApi: {
		downloadVersion: (projectId: string, id: string) => `/files/${projectId}/${id}`,
	},
}));

import {
	deletePayrollPerson,
	importPayrollFileByRole,
	importPayrollRoster,
	listPayrollPeople,
	listPayrollSalary,
	updatePayrollPerson,
} from "./payroll-service-client";

const api = (data: unknown) => new Response(JSON.stringify(data));
const requestBody = (index: number) => mocks.authenticatedFetch.mock.calls[index]?.[1]?.body;

describe("payroll-service-client", () => {
	beforeEach(() => {
		mocks.authenticatedFetch.mockReset();
	});
	afterEach(() => {
		vi.clearAllMocks();
	});

	it("maps roster identity and enums in both directions", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({
				code: 0,
				response: {
					total: 1,
					items: [
						{
							roster_id: 42,
							name: "张三",
							employee_type: 2,
							gender: 1,
							company_name: "部门",
							project_name: "项目",
							position: "工程师",
						},
					],
				},
			}),
		);
		const page = await listPayrollPeople({ offset: 10, limit: 10 });
		expect(page.items[0]).toEqual({
			id: 42,
			name: "张三",
			employeeType: "易通外包",
			gender: "男",
			companyName: "部门",
			projectName: "项目",
			position: "工程师",
		});
		expect(JSON.parse(requestBody(0) as string)).toEqual({
			request: {
				offset: 10,
				limit: 10,
				name: "",
				project_name: "",
				company_name: "",
			},
		});

		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({
				code: 0,
				response: {
					roster: {
						roster_id: 42,
						name: "张三",
						employee_type: 2,
						gender: 1,
						company_name: "部门",
						project_name: "项目",
						position: "工程师",
					},
				},
			}),
		);
		const person = page.items[0];
		if (!person) throw new Error("roster item missing from fixture");
		await updatePayrollPerson(person);
		expect(JSON.parse(requestBody(1) as string)).toEqual({
			request: {
				id: 42,
				name: "张三",
				project_name: "项目",
				company_name: "部门",
				employee_type: 2,
				gender: 1,
				position: "工程师",
			},
		});
	});

	it("uses actual historical salary list fields and derives initialized from total", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({ code: 0, response: { total: 0, items: [] } }),
		);
		expect(await listPayrollSalary({ limit: 1, payrollMonth: "2026-06", name: "张三" })).toEqual({
			total: 0,
			items: [],
			initialized: false,
		});
		expect(mocks.authenticatedFetch.mock.calls[0]?.[0]).toBe(
			"http://lework.test/v1/PayrollProxy/PageHisSalary",
		);
		expect(JSON.parse(requestBody(0) as string)).toEqual({
			request: { offset: 0, limit: 1, payroll_month: "2026-06", name: "张三" },
		});
	});

	it("normalizes legacy null page items to empty arrays", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({ code: 0, response: { total: 0, items: null } }),
		);
		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({ code: 0, response: { total: 0, items: null } }),
		);

		expect((await listPayrollPeople()).items).toEqual([]);
		expect((await listPayrollSalary()).items).toEqual([]);
	});

	it("sends authenticated multipart import and hides raw backend errors", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(
			new Response(
				JSON.stringify({
					task_id: 7,
					status: "success",
					total_count: 1,
					success_count: 1,
					failed_count: 0,
				}),
			),
		);
		const file = new File(["xlsx"], "roster.xlsx");
		await importPayrollRoster(file);
		const options = mocks.authenticatedFetch.mock.calls[0]?.[1] as RequestInit;
		expect(mocks.authenticatedFetch.mock.calls[0]?.[0]).toBe(
			"http://lework.test/v1/PayrollProxy/ImportRoster",
		);
		expect(options.method).toBe("POST");
		expect((options.body as FormData).get("file")).toBe(file);

		mocks.authenticatedFetch.mockResolvedValueOnce(
			api({ code: 9, message: "Go Payroll internal failure", response: {} }),
		);
		await expect(listPayrollPeople()).rejects.toThrow("工资资料暂时无法处理，请稍后重试。");

		mocks.authenticatedFetch.mockResolvedValueOnce(
			new Response(JSON.stringify({ message: "Go Payroll internal failure" }), { status: 500 }),
		);
		await expect(importPayrollRoster(file)).rejects.toThrow("文件导入失败，请检查表格内容后重试。");
	});

	it("downloads selected project files through Lework before Payroll import", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(new Response("xlsx-bytes"));
		mocks.authenticatedFetch.mockResolvedValueOnce(
			new Response(
				JSON.stringify({
					task_id: 9,
					status: "success",
					total_count: 1,
					success_count: 1,
					failed_count: 0,
				}),
			),
		);
		await importPayrollFileByRole("historical_payroll", {
			id: "f1",
			name: "history.xlsx",
			publicId: "f1",
			projectId: "p1",
			storageUri: "s3://f1",
			mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			size: 9,
			role: "historical_payroll",
		});
		expect(mocks.authenticatedFetch).toHaveBeenNthCalledWith(1, "/files/p1/f1");
		const init = mocks.authenticatedFetch.mock.calls[1]?.[1] as RequestInit;
		expect(mocks.authenticatedFetch.mock.calls[1]?.[0]).toBe(
			"http://lework.test/v1/PayrollProxy/ImportHisSalary",
		);
		expect((init.body as FormData).get("file")).toMatchObject({ name: "history.xlsx" });
	});

	it("uses roster id as request.id for soft-delete API", async () => {
		mocks.authenticatedFetch.mockResolvedValueOnce(api({ code: 0, response: { id: 42 } }));
		await deletePayrollPerson(42);
		expect(JSON.parse(requestBody(0) as string)).toEqual({ request: { id: 42 } });
	});
});
