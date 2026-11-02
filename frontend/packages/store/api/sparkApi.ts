import { apiClient } from "./client";

export type SparkCheck = { code: string; message: string };
export type SparkMaintenance = "unsupported" | "unknown" | "paused" | "accepting";
export type SparkModelControl = {
	protocol_version: 1;
	host_id: string;
	boot_id: string;
	observed_at: number;
	model_id: string;
	label: string;
	state: "stopped" | "starting" | "ready" | "draining" | "stopping" | "error" | "unknown";
	revision: number;
	operation_id: string;
	action: "none" | "start" | "stop";
	error_code: string;
	busy: boolean;
	can_start: boolean;
	can_stop: boolean;
};
export type SparkModelRequest = {
	boot_id: string;
	expected_revision: number;
	maintenance_revision: number;
	operation_id: string;
	action: "start" | "stop";
};
export type SparkStatus = {
	model_control?: SparkModelControl;
	maintenance?: SparkMaintenance;
	maintenance_control?: {
		revision: number;
		operation_id: string;
		active: boolean;
		confirmed: boolean;
		can_pause: boolean;
		can_resume: boolean;
	};
	valid_until: number;
	snapshot: {
		protocol_version: 1;
		host_id: string;
		boot_id: string;
		hostname: string;
		observed_at: number;
		mode: "read_only";
		model_state: "unconfigured";
		inventory_complete: boolean;
		components: {
			kind: "docker" | "kubernetes";
			id: string;
			name: string;
			state: string;
			registered: boolean;
		}[];
		checks: SparkCheck[];
	};
	workers: {
		worker_id: number;
		maintenance?: SparkMaintenance;
		observed_at: number;
		state: "unknown" | "busy" | "observed";
		running_count: number | null;
		waiting_count: number | null;
	}[];
	checks: SparkCheck[];
	can_start_model: boolean;
	can_stop_model: boolean;
	can_shutdown: false;
};
export type SparkPreflight = { ready: false; checked_at: number; status: SparkStatus };

const options = {
	timeout: 25000,
	retryCount: 0,
	redirect: "error" as const,
	credentials: "omit" as const,
};
const invalid = () => new Error("设备状态响应无法验证，请刷新或联系管理员检查部署。");
const record = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const text = (v: unknown): v is string =>
	typeof v === "string" && v.trim().length > 0 && v.length <= 2048;
const count = (v: unknown): v is number =>
	typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
const fresh = (v: unknown) =>
	count(v) && Math.floor(Date.now() / 1000) - v >= -5 && Math.floor(Date.now() / 1000) - v <= 30;
const checks = (v: unknown): v is SparkCheck[] =>
	Array.isArray(v) &&
	v.length <= 320 &&
	v.every((c) => record(c) && text(c.code) && text(c.message));

const maintenance = (v: unknown) =>
	v === undefined ||
	(typeof v === "string" && ["unsupported", "unknown", "paused", "accepting"].includes(v));

function validControl(v: Record<string, unknown>) {
	const c = v.maintenance_control;
	if (c === undefined) return true;
	if (
		!record(c) ||
		!count(c.revision) ||
		c.revision === 0 ||
		typeof c.operation_id !== "string" ||
		!/^[A-Za-z0-9_-]{1,128}$/.test(c.operation_id) ||
		typeof c.active !== "boolean" ||
		typeof c.confirmed !== "boolean" ||
		typeof c.can_pause !== "boolean" ||
		typeof c.can_resume !== "boolean"
	)
		return false;
	const writable = c.revision < Number.MAX_SAFE_INTEGER;
	if (
		(c.can_pause && !(c.confirmed && !c.active && writable)) ||
		(c.can_resume &&
			!(
				c.confirmed &&
				c.active &&
				writable &&
				!(record(v.model_control) && (v.model_control.busy || v.model_control.state === "unknown"))
			))
	)
		return false;
	if (c.confirmed) {
		const phase = c.active ? "paused" : "accepting";
		if (
			v.maintenance !== phase ||
			!Array.isArray(v.workers) ||
			v.workers.length === 0 ||
			!v.workers.every((w) => record(w) && w.maintenance === phase && w.state !== "unknown")
		)
			return false;
	}
	return true;
}

function validModel(v: unknown): v is SparkModelControl {
	if (!record(v)) return false;
	return (
		v.protocol_version === 1 &&
		text(v.host_id) &&
		text(v.boot_id) &&
		fresh(v.observed_at) &&
		text(v.model_id) &&
		text(v.label) &&
		typeof v.state === "string" &&
		["stopped", "starting", "ready", "draining", "stopping", "error", "unknown"].includes(
			String(v.state),
		) &&
		count(v.revision) &&
		v.revision > 0 &&
		typeof v.operation_id === "string" &&
		/^[A-Za-z0-9_-]{1,128}$/.test(v.operation_id) &&
		typeof v.action === "string" &&
		["none", "start", "stop"].includes(v.action) &&
		typeof v.error_code === "string" &&
		v.error_code.length <= 2048 &&
		typeof v.busy === "boolean" &&
		typeof v.can_start === "boolean" &&
		typeof v.can_stop === "boolean" &&
		(!["starting", "draining", "stopping"].includes(String(v.state)) || v.busy) &&
		(!v.can_start || (!v.busy && v.state === "stopped" && v.revision < Number.MAX_SAFE_INTEGER)) &&
		(!v.can_stop || (!v.busy && v.state === "ready" && v.revision < Number.MAX_SAFE_INTEGER))
	);
}

function validModelAuthority(v: Record<string, unknown>) {
	const m = v.model_control;
	if (m === undefined) return v.can_start_model === false && v.can_stop_model === false;
	if (
		!validModel(m) ||
		!record(v.snapshot) ||
		m.host_id !== v.snapshot.host_id ||
		m.boot_id !== v.snapshot.boot_id ||
		!count(v.valid_until) ||
		v.valid_until > m.observed_at + 30 ||
		v.can_start_model !== m.can_start ||
		v.can_stop_model !== m.can_stop
	)
		return false;
	if (!m.can_start && !m.can_stop) return true;
	const c = v.maintenance_control;
	return (
		record(c) &&
		c.active === true &&
		c.confirmed === true &&
		v.maintenance === "paused" &&
		Array.isArray(v.workers) &&
		v.workers.length > 0 &&
		v.workers.every(
			(w) =>
				record(w) &&
				w.state === "observed" &&
				w.maintenance === "paused" &&
				w.running_count === 0 &&
				w.waiting_count === 0,
		)
	);
}

function validStatus(v: unknown): v is SparkStatus {
	if (!record(v) || !record(v.snapshot)) return false;
	const s = v.snapshot;
	return (
		validControl(v) &&
		validModelAuthority(v) &&
		maintenance(v.maintenance) &&
		s.protocol_version === 1 &&
		text(s.host_id) &&
		text(s.boot_id) &&
		text(s.hostname) &&
		fresh(s.observed_at) &&
		s.mode === "read_only" &&
		s.model_state === "unconfigured" &&
		typeof s.inventory_complete === "boolean" &&
		checks(s.checks) &&
		Array.isArray(s.components) &&
		s.components.length <= 256 &&
		s.components.every(
			(c) =>
				record(c) &&
				(c.kind === "docker" || c.kind === "kubernetes") &&
				text(c.id) &&
				text(c.name) &&
				text(c.state) &&
				typeof c.registered === "boolean",
		) &&
		Array.isArray(v.workers) &&
		v.workers.length <= 32 &&
		v.workers.every(
			(w) =>
				record(w) &&
				maintenance(w.maintenance) &&
				count(w.worker_id) &&
				w.worker_id > 0 &&
				["unknown", "busy", "observed"].includes(String(w.state)) &&
				count(w.observed_at) &&
				(w.state === "unknown"
					? w.observed_at === 0 && w.running_count === null && w.waiting_count === null
					: fresh(w.observed_at) &&
						count(w.running_count) &&
						count(w.waiting_count) &&
						count(v.valid_until) &&
						v.valid_until <= w.observed_at + 30) &&
				(w.running_count === null || count(w.running_count)) &&
				(w.waiting_count === null || count(w.waiting_count)),
		) &&
		count(v.valid_until) &&
		v.valid_until >= Math.floor(Date.now() / 1000) &&
		count(s.observed_at) &&
		v.valid_until <= s.observed_at + 30 &&
		checks(v.checks) &&
		v.can_shutdown === false
	);
}
function unwrap(v: unknown): unknown {
	if (!record(v) || v.code !== 0) throw invalid();
	return v.data;
}
function failure(error: unknown): Error {
	if (record(error)) {
		if (error.status === 404 || error.status === 405)
			return new Error("当前后端版本尚不支持 Spark 设备管理。");
		if (error.status === 401) return new Error("登录已失效，请重新登录。");
		if (error.status === 403) return new Error("仅设备所属组织的管理员可查看和操作。");
		if (error.message === "spark_not_configured")
			return new Error("后端尚未配置 Spark 管理服务和主机绑定。");
		if (error.status === 502) return invalid();
		if (error.status === 503) return new Error("暂时无法确认设备状态或管理员权限，请稍后刷新。");
	}
	return error instanceof Error && error.message === invalid().message
		? error
		: new Error("设备状态未知，请检查连接后刷新。");
}

export const sparkApi = {
	async changeModel(request: SparkModelRequest): Promise<SparkModelControl> {
		try {
			// 固定目标由服务端绑定，仅发送协议字段，避免携带调用方扩展参数。
			const body = {
				boot_id: request.boot_id,
				expected_revision: request.expected_revision,
				maintenance_revision: request.maintenance_revision,
				operation_id: request.operation_id,
				action: request.action,
			};
			const data = unwrap((await apiClient.post<unknown>("/ops/spark/model", body, options)).data);
			if (
				!validModel(data) ||
				data.boot_id !== request.boot_id ||
				data.revision !== request.expected_revision + 1 ||
				data.operation_id !== request.operation_id ||
				data.action !== request.action
			)
				throw invalid();
			return data;
		} catch (error) {
			if (record(error) && error.status === 409)
				throw new Error("模型或维护状态已变化，请刷新后重新确认。");
			if (record(error) && (error.status === 401 || error.status === 403)) throw failure(error);
			throw new Error("模型操作结果尚未确认，请刷新状态核对；不要重复提交。");
		}
	},
	async changeMaintenance(request: {
		boot_id: string;
		expected_revision: number;
		operation_id: string;
		active: boolean;
	}): Promise<{ revision: number; operation_id: string; active: boolean }> {
		try {
			const data = unwrap(
				(await apiClient.post<unknown>("/ops/spark/maintenance", request, options)).data,
			);
			if (
				!record(data) ||
				data.revision !== request.expected_revision + 1 ||
				data.operation_id !== request.operation_id ||
				data.active !== request.active
			)
				throw invalid();
			return {
				revision: data.revision as number,
				operation_id: data.operation_id as string,
				active: data.active as boolean,
			};
		} catch (error) {
			if (record(error) && error.status === 409)
				throw new Error("维护状态已变化，请刷新后重新确认。");
			if (record(error) && (error.status === 401 || error.status === 403)) throw failure(error);
			throw new Error("操作结果尚未确认，请刷新状态核对；不要重复提交。");
		}
	},
	async status(): Promise<SparkStatus> {
		try {
			const data = unwrap((await apiClient.get<unknown>("/ops/spark/status", options)).data);
			if (!validStatus(data)) throw invalid();
			return data;
		} catch (error) {
			throw failure(error);
		}
	},
	async preflight(): Promise<SparkPreflight> {
		try {
			const data = unwrap(
				(await apiClient.post<unknown>("/ops/spark/preflight", {}, options)).data,
			);
			if (
				!record(data) ||
				data.ready !== false ||
				!fresh(data.checked_at) ||
				!validStatus(data.status)
			)
				throw invalid();
			return { ready: false, checked_at: data.checked_at as number, status: data.status };
		} catch (error) {
			throw failure(error);
		}
	},
};
