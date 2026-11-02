import type { SparkStatus, SparkWorkflow } from "@leros/store";

const modelErrors: Record<string, string> = {
	gate_unavailable: "暂时无法确认模型状态，请刷新或联系管理员。",
	model_outcome_unknown: "模型操作结果尚未确认，请刷新核对，不要重复提交。",
	model_start_timeout: "模型启动超时，请刷新状态或联系管理员检查。",
	model_drain_timeout: "模型仍有请求未结束，请稍后刷新。",
	model_stop_unconfirmed: "尚未确认模型停止，请刷新核对，不要重复提交。",
};
const checkLabels: Record<string, string> = {
	inventory_incomplete: "工作负载清单不完整",
	unregistered_workloads: "存在未登记管理策略的工作负载",
	worker_mapping_missing: "尚未登记任务执行服务",
	worker_unknown: "部分任务状态无法确认",
	worker_busy: "仍有未完成任务",
	model_unknown: "本机模型状态无法确认",
};
const safeMessages = new Set([
	"当前后端版本尚不支持 Spark 设备管理。",
	"登录已失效，请重新登录。",
	"仅设备所属组织的管理员可查看和操作。",
	"后端尚未配置 Spark 管理服务和主机绑定。",
	"暂时无法确认设备状态或管理员权限，请稍后刷新。",
	"设备状态响应无法验证，请刷新或联系管理员检查部署。",
	"模型或维护状态已变化，请刷新后重新确认。",
	"维护状态已变化，请刷新后重新确认。",
]);

export function safeSparkFailure(error: unknown, mutation = false) {
	if (error instanceof Error && safeMessages.has(error.message)) return error.message;
	return mutation
		? "操作结果尚未确认，请刷新状态核对；不要重复提交。"
		: "设备状态未知，请检查连接后刷新；仍无法读取时请联系管理员。";
}

export function modelErrorCode(code: string) {
	return Object.hasOwn(modelErrors, code) ? code : code ? "unknown_error" : "";
}
export function modelErrorMessage(code: string) {
	return Object.hasOwn(modelErrors, code)
		? modelErrors[code]
		: code
			? "模型状态异常，请刷新或联系管理员检查。"
			: "";
}
export function safeSparkChecks(data: SparkStatus) {
	return [...new Set(data.checks.map((check) => check.code))]
		.filter((code) => Object.hasOwn(checkLabels, code))
		.map((code) => ({ code, label: checkLabels[code] }));
}

const componentStates: Record<string, string> = {
	running: "运行中",
	exited: "已停止",
	created: "已创建",
	restarting: "重启中",
	paused: "已暂停",
	dead: "已停止",
	Running: "运行中",
	Pending: "等待启动",
	Succeeded: "已完成",
	Failed: "运行失败",
};
export function safeComponentState(state: string) {
	return Object.hasOwn(componentStates, state) ? componentStates[state] : "状态未知";
}

// 诊断仅导出固定枚举、布尔值和计数；绝不序列化原始响应或身份字段。
export function sparkDiagnosticText(data: SparkStatus) {
	return JSON.stringify(
		{
			version: 1,
			connection: "online",
			observed_at: data.snapshot.observed_at,
			valid_until: data.valid_until,
			inventory_complete: data.snapshot.inventory_complete,
			task_admission: data.maintenance ?? "unsupported",
			maintenance_confirmed: data.maintenance_control?.confirmed ?? false,
			model: data.model_control
				? {
						state: data.model_control.state,
						action: data.model_control.action,
						busy: data.model_control.busy,
						error_code: modelErrorCode(data.model_control.error_code),
					}
				: null,
			workers: data.workers.map((worker) => ({
				state: worker.state,
				maintenance: worker.maintenance ?? "unsupported",
				running_count: worker.running_count,
				waiting_count: worker.waiting_count,
			})),
			components: data.snapshot.components.map((component) => ({
				kind: component.kind,
				registered: component.registered,
			})),
			checks: safeSparkChecks(data).map((check) => check.code),
		},
		null,
		2,
	);
}

// 操作引用可用于定位记录；绝不复制整条 workflow 或原始错误。
export function sparkWorkflowDiagnosticText(workflow: SparkWorkflow): string {
	const phases = [
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
	];
	const errors = [
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
	];
	const number = (value: number) => (Number.isSafeInteger(value) && value > 0 ? value : "unknown");
	return [
		"操作进度诊断：",
		`operation_id: ${/^[A-Za-z0-9_-]{1,128}$/.test(workflow.operation_id) ? workflow.operation_id : "invalid"}`,
		`action: ${["start", "stop", "pause", "resume"].includes(workflow.action) ? workflow.action : "unknown"}`,
		`phase: ${phases.includes(workflow.phase) ? workflow.phase : "unknown"}`,
		`version: ${number(workflow.version)}`,
		`updated_at: ${number(workflow.updated_at)}`,
		`error_code: ${errors.includes(workflow.error_code) ? workflow.error_code || "none" : "unknown"}`,
	].join("\n");
}
