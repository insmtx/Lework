import { apiClient } from "./client";

export const sparkWorkflowPhases = [
	"accepted",
	"pause_intent",
	"waiting_idle",
	"model_intent",
	"waiting_model",
	"resume_intent",
	"waiting_resume",
	"completed",
	"completed_paused",
	"attention",
] as const;
export const sparkWorkflowErrors = [
	"",
	"interrupted",
	"deadline_exceeded",
	"outcome_unknown",
	"binding_changed",
	"identity_changed",
	"revision_changed",
	"idle_timeout",
	"model_unconfirmed",
	"resume_unconfirmed",
] as const;
export type SparkWorkflow = {
	operation_id: string;
	action: "start" | "stop" | "pause" | "resume";
	kind: "workflow" | "manual_model" | "manual_maintenance";
	phase: (typeof sparkWorkflowPhases)[number];
	version: number;
	created_at: number;
	updated_at: number;
	deadline: number;
	error_code: (typeof sparkWorkflowErrors)[number];
	needs_attention: boolean;
	maintenance_retained: boolean;
};
export type SparkWorkflowControl = {
	enabled: boolean;
	current: SparkWorkflow | null;
	can_start: boolean;
	can_stop: boolean;
};
export type SparkWorkflowRequest = {
	operation_id: string;
	action: "start" | "stop";
	boot_id: string;
	expected_model_revision: number;
	expected_maintenance_revision: number;
};
const options = {
	timeout: 25000,
	retryCount: 0,
	redirect: "error" as const,
	credentials: "omit" as const,
};
const record = (x: unknown): x is Record<string, unknown> =>
	x !== null && typeof x === "object" && !Array.isArray(x);
const id = (x: unknown): x is string => typeof x === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(x);
const integer = (x: unknown): x is number =>
	typeof x === "number" && Number.isSafeInteger(x) && x > 0;
const invalid = () => new Error("流程状态无法确认，请刷新核对；不要重复提交。");
function unwrap(x: unknown): unknown {
	if (!record(x) || x.code !== 0 || !("data" in x)) throw invalid();
	return x.data;
}
function operation(x: unknown): SparkWorkflow {
	if (
		!record(x) ||
		!id(x.operation_id) ||
		!["start", "stop", "pause", "resume"].includes(x.action as string) ||
		!["workflow", "manual_model", "manual_maintenance"].includes(x.kind as string) ||
		!sparkWorkflowPhases.includes(x.phase as SparkWorkflow["phase"]) ||
		!sparkWorkflowErrors.includes(x.error_code as SparkWorkflow["error_code"]) ||
		!integer(x.version) ||
		!integer(x.created_at) ||
		!integer(x.updated_at) ||
		!integer(x.deadline) ||
		x.updated_at < x.created_at ||
		x.deadline <= x.created_at ||
		typeof x.needs_attention !== "boolean" ||
		typeof x.maintenance_retained !== "boolean" ||
		x.needs_attention !== (x.phase === "attention") ||
		(x.kind !== "manual_maintenance" && x.action !== "start" && x.action !== "stop")
	)
		throw invalid();
	return {
		operation_id: x.operation_id,
		action: x.action as SparkWorkflow["action"],
		kind: x.kind as SparkWorkflow["kind"],
		phase: x.phase as SparkWorkflow["phase"],
		version: x.version,
		created_at: x.created_at,
		updated_at: x.updated_at,
		deadline: x.deadline,
		error_code: x.error_code as SparkWorkflow["error_code"],
		needs_attention: x.needs_attention,
		maintenance_retained: x.maintenance_retained,
	};
}
export const sparkWorkflowApi = {
	async current(): Promise<SparkWorkflowControl> {
		try {
			const x = unwrap(
				(await apiClient.get<unknown>("/ops/spark/workflows/current", options)).data,
			);
			if (
				!record(x) ||
				typeof x.enabled !== "boolean" ||
				typeof x.can_start !== "boolean" ||
				typeof x.can_stop !== "boolean" ||
				!("current" in x)
			)
				throw invalid();
			const current = x.current === null ? null : operation(x.current);
			if (
				(!x.enabled && (current || x.can_start || x.can_stop)) ||
				(current && (x.can_start || x.can_stop))
			)
				throw invalid();
			return {
				enabled: x.enabled,
				current,
				can_start: x.can_start,
				can_stop: x.can_stop,
			};
		} catch (error) {
			// 仅明确不存在的旧路由可回退；离线、权限或数据库故障不能伪装成不支持。
			if (record(error) && error.status === 404)
				return {
					enabled: false,
					current: null,
					can_start: false,
					can_stop: false,
				};
			throw invalid();
		}
	},
	async get(operationID: string): Promise<SparkWorkflow> {
		if (!id(operationID)) throw invalid();
		try {
			const x = operation(
				unwrap((await apiClient.get<unknown>(`/ops/spark/workflows/${operationID}`, options)).data),
			);
			if (x.operation_id !== operationID) throw invalid();
			return x;
		} catch {
			throw invalid();
		}
	},
	async begin(request: SparkWorkflowRequest): Promise<SparkWorkflow> {
		if (
			!id(request.operation_id) ||
			!["start", "stop"].includes(request.action) ||
			typeof request.boot_id !== "string" ||
			!request.boot_id.trim() ||
			request.boot_id.length > 128 ||
			!integer(request.expected_model_revision) ||
			!integer(request.expected_maintenance_revision)
		)
			throw invalid();
		try {
			const x = operation(
				unwrap((await apiClient.post<unknown>("/ops/spark/workflows", request, options)).data),
			);
			if (
				x.operation_id !== request.operation_id ||
				x.action !== request.action ||
				x.kind !== "workflow"
			)
				throw invalid();
			return x;
		} catch {
			throw invalid();
		}
	},
};
