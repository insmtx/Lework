import { API_BASE_URL, authenticatedFetch, projectFileApi } from "@lework/store";
import type {
	PayrollEmployeeType,
	PayrollSelectedFile,
	PayrollStoredPerson,
} from "./payroll-accounting-mock";

const PAYROLL_SESSION_STORAGE_KEY = "lework-payroll-task-sessions";
const employeeTypeToCode: Record<PayrollEmployeeType, number> = {
	在册: 1,
	易通外包: 2,
	益通外包: 3,
	博途外包: 4,
	劳务派遣: 5,
};
const employeeTypes = Object.keys(employeeTypeToCode) as PayrollEmployeeType[];
const genders = ["未知", "男", "女"] as const;
const genderToCode = { 未知: 0, 男: 1, 女: 2 } as const;

export type PayrollSalaryRecord = {
	id: number;
	payroll_month: string;
	roster_id: number;
	name: string;
	position: string;
	work_days: string;
	gross_amount: string;
};

export type PayrollChangeField = {
	field: string;
	field_name: string;
	before?: string;
	after?: string;
	value_type?: string;
};
export type PayrollChangeEvent = {
	id: number;
	operation: "CREATE" | "UPDATE" | "DELETE";
	source_type: "IMPORT" | "CRUD";
	version: number;
	changed_at: string;
	summary: string;
	changes: PayrollChangeField[];
	snapshot?: Record<string, unknown>;
};
export type PayrollChangeHistory = { record_id: number; events: PayrollChangeEvent[] };

export type PayrollImportResult = {
	task_id: number;
	status: "pending" | "processing" | "success" | "partial_success" | "failed";
	total_count: number;
	success_count: number;
	failed_count: number;
	message?: string;
};

type PayrollEnvelope<T> = { code: number; message?: string; response: T };
type PayrollRosterDTO = {
	roster_id: number;
	name: string;
	employee_type: number;
	gender: number;
	company_name: string;
	project_name: string;
	position: string;
};

function payrollRequest(path: string): string {
	return `${API_BASE_URL.replace(/\/+$/, "")}/PayrollProxy/${path.replace(/^\/payroll\./, "")}`;
}

async function payrollJSON<T>(path: string, body: unknown): Promise<T> {
	let response: Response;
	try {
		response = await authenticatedFetch(payrollRequest(path), {
			method: "POST",
			headers: {
				Accept: "application/json",
				"Content-Type": "application/json",
			},
			body: JSON.stringify(body),
		});
	} catch {
		throw new Error("工资资料暂时无法连接，请稍后重试。");
	}
	let data: PayrollEnvelope<T>;
	try {
		data = (await response.json()) as PayrollEnvelope<T>;
	} catch {
		throw new Error("工资资料暂时无法读取，请稍后重试。");
	}
	if (!response.ok || !Number.isInteger(data.code) || data.code !== 0) {
		throw new Error("工资资料暂时无法处理，请稍后重试。");
	}
	return data.response;
}
async function importPayrollFile(path: string, file: File): Promise<PayrollImportResult> {
	if (!file.name.toLowerCase().endsWith(".xlsx")) throw new Error("仅支持 .xlsx 文件");
	const form = new FormData();
	form.append("file", file);
	let response: Response;
	try {
		response = await authenticatedFetch(payrollRequest(path), {
			method: "POST",
			headers: { Accept: "application/json" },
			body: form,
		});
	} catch {
		throw new Error("文件导入暂时无法完成，请稍后重试。");
	}
	let result: PayrollImportResult & { code?: number };
	try {
		result = (await response.json()) as PayrollImportResult & { code?: number };
	} catch {
		throw new Error("文件导入未完成，请检查文件后重试。");
	}
	if (!response.ok || (result.code !== undefined && result.code !== 0))
		throw new Error("文件导入失败，请检查表格内容后重试。");
	if (typeof result.task_id !== "number" || typeof result.status !== "string") {
		throw new Error("文件导入未完成，请检查文件后重试。");
	}
	if (result.status === "pending" || result.status === "processing") {
		throw new Error("文件正在处理中，请稍后查看导入结果。");
	}
	return result;
}

function mapRoster(row: PayrollRosterDTO): PayrollStoredPerson {
	const employeeType = employeeTypes[row.employee_type - 1];
	const gender = genders[row.gender];
	if (!employeeType || !gender) throw new Error("人员资料包含无法识别的类别信息，请核对后重试。");
	return {
		id: row.roster_id, // Frontend view-model mapping: roster_id -> id.
		name: row.name,
		projectName: row.project_name,
		companyName: row.company_name,
		position: row.position,
		employeeType,
		gender,
	};
}

export async function listPayrollPeople(
	options: {
		offset?: number;
		limit?: number;
		name?: string;
		projectName?: string;
		companyName?: string;
	} = {},
): Promise<{ total: number; items: PayrollStoredPerson[]; initialized: boolean }> {
	const result = await payrollJSON<{ total: number; items: PayrollRosterDTO[] | null }>(
		"/payroll.PageRoster",
		{
			request: {
				offset: options.offset ?? 0,
				limit: options.limit ?? 10,
				name: options.name ?? "",
				project_name: options.projectName ?? "",
				company_name: options.companyName ?? "",
			},
		},
	);
	return { ...result, items: (result.items ?? []).map(mapRoster), initialized: result.total > 0 };
}

export async function listPayrollSalary(
	options: { offset?: number; limit?: number; payrollMonth?: string; name?: string } = {},
): Promise<{ total: number; items: PayrollSalaryRecord[]; initialized: boolean }> {
	const result = await payrollJSON<{ total: number; items: PayrollSalaryRecord[] | null }>(
		"/payroll.PageHisSalary",
		{
			request: {
				offset: options.offset ?? 0,
				limit: options.limit ?? 10,
				payroll_month: options.payrollMonth ?? "",
				name: options.name ?? "",
			},
		},
	);
	return { ...result, items: result.items ?? [], initialized: result.total > 0 };
}

export async function updatePayrollPerson(
	person: PayrollStoredPerson,
): Promise<PayrollStoredPerson> {
	const result = await payrollJSON<{ roster: PayrollRosterDTO }>("/payroll.UpdateRoster", {
		request: {
			id: person.id,
			name: person.name,
			project_name: person.projectName,
			company_name: person.companyName,
			employee_type: employeeTypeToCode[person.employeeType],
			gender: genderToCode[person.gender],
			position: person.position,
		},
	});
	return mapRoster(result.roster);
}

export async function deletePayrollPerson(id: number): Promise<void> {
	await payrollJSON<{ id: number }>("/payroll.DeleteRoster", { request: { id } });
}

export function listPayrollRosterChangeHistory(rosterId: number) {
	return payrollJSON<PayrollChangeHistory>("/payroll.ListRosterChangeHistory", {
		request: { roster_id: rosterId },
	});
}

export function listPayrollSalaryChangeHistory(hisSalaryRecordId: number) {
	return payrollJSON<PayrollChangeHistory>("/payroll.ListHisSalaryChangeHistory", {
		request: { his_salary_record_id: hisSalaryRecordId },
	});
}

export const importPayrollRoster = (file: File) => importPayrollFile("/payroll.ImportRoster", file);
export const importPayrollSalary = (file: File) =>
	importPayrollFile("/payroll.ImportHisSalary", file);

export async function updatePayrollSalary(
	record: PayrollSalaryRecord,
): Promise<PayrollSalaryRecord> {
	const result = await payrollJSON<{ record: PayrollSalaryRecord }>("/payroll.UpdateHisSalary", {
		request: record,
	});
	return result.record;
}

export async function deletePayrollSalary(id: number): Promise<void> {
	await payrollJSON<{ id: number }>("/payroll.DeleteHisSalary", { request: { id } });
}

export async function importPayrollFileByRole(
	role: "roster" | "historical_payroll",
	selected: PayrollSelectedFile,
): Promise<PayrollImportResult> {
	let file = selected.file;
	if (!file && selected.projectId && selected.publicId) {
		const response = await authenticatedFetch(
			projectFileApi.downloadVersion(selected.projectId, selected.publicId),
		);
		if (!response.ok) throw new Error(`${selected.name} 下载失败（HTTP ${response.status}）`);
		file = new File([await response.blob()], selected.name, { type: selected.mimeType });
	}
	if (!file) throw new Error(`文件「${selected.name}」无法读取`);
	return role === "roster" ? importPayrollRoster(file) : importPayrollSalary(file);
}

export function markPayrollTaskSession(sessionId: string) {
	const sessions = readPayrollSessions();
	sessions.add(sessionId);
	writePayrollSessions(sessions);
}

export function isPayrollTaskSession(sessionId: string): boolean {
	return readPayrollSessions().has(sessionId);
}

function readPayrollSessions(): Set<string> {
	if (typeof sessionStorage === "undefined") return new Set();
	try {
		const raw = sessionStorage.getItem(PAYROLL_SESSION_STORAGE_KEY);
		const parsed = raw ? (JSON.parse(raw) as unknown) : [];
		return new Set(Array.isArray(parsed) ? parsed.filter((id) => typeof id === "string") : []);
	} catch {
		return new Set();
	}
}

function writePayrollSessions(sessions: Set<string>) {
	if (typeof sessionStorage !== "undefined") {
		sessionStorage.setItem(PAYROLL_SESSION_STORAGE_KEY, JSON.stringify([...sessions]));
	}
}
