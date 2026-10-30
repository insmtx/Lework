import { apiClient } from "./client";

export type SparkCheck = { code: string; message: string };
export type SparkMaintenance = "unsupported" | "unknown" | "paused" | "accepting";
export type SparkStatus = {
	maintenance?: SparkMaintenance;
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
	can_start_model: false;
	can_stop_model: false;
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
	v === undefined || ["unsupported", "unknown", "paused", "accepting"].includes(String(v));

function validStatus(v: unknown): v is SparkStatus {
	if (!record(v) || !record(v.snapshot)) return false;
	const s = v.snapshot;
	return (
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
		v.can_start_model === false &&
		v.can_stop_model === false &&
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
