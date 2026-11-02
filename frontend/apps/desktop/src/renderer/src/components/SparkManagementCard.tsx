import {
	type SparkStatus,
	type SparkWorkflow,
	type SparkWorkflowControl,
	sparkApi,
	sparkWorkflowApi,
	useAuthStore,
} from "@leros/store";
import { useCallback, useEffect, useRef, useState } from "react";

import { useSparkConnection } from "./SparkConnectionGate";
import {
	modelErrorCode,
	modelErrorMessage,
	safeComponentState,
	safeSparkChecks,
	safeSparkFailure,
	sparkDiagnosticText,
	sparkWorkflowDiagnosticText,
} from "./sparkDiagnostics";

const buttonStyle =
	"rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-45";

export function SparkManagementCard() {
	const connection = useSparkConnection();
	const user = useAuthStore((s) => s.authUser);
	const org = user?.organizations?.find((o) => o.id === user.currentOrg?.id) ?? user?.currentOrg;
	if (!connection) return null;
	const canView = Boolean(
		user && org?.isAdmin && connection.status === "online" && connection.device.management,
	);
	// 连接、权限或身份变化均卸载旧控制器，清除快照与待确认操作。
	const identity = JSON.stringify([
		connection.baseURL,
		connection.device.id,
		user?.publicId,
		org?.id,
		org?.isAdmin,
		user?.jwtToken,
	]);
	return (
		<section
			className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm"
			aria-label="Spark 设备管理"
		>
			<div className="flex flex-wrap items-center justify-between gap-2">
				<h2 className="text-lg font-semibold text-slate-950">{connection.device.name}</h2>
				<span className="text-sm text-slate-600">
					连接：{connection.status === "online" ? "已连接" : "离线"}
				</span>
			</div>
			{canView ? (
				<DeviceStatus key={identity} />
			) : (
				<p className="mt-4 text-sm text-slate-600">
					{connection.status === "offline"
						? "设备暂时离线，请检查网线或网络连接，连接恢复后将重新读取状态。"
						: !connection.device.management
							? "此设备尚未启用设备管理，请联系管理员完成配置。"
							: "已连接到 Spark。模型和任务状态请联系设备所属组织的管理员查看。"}
				</p>
			)}
		</section>
	);
}

function maintenanceLabel(state: SparkStatus["maintenance"]) {
	switch (state) {
		case "paused":
			return "维护中";
		case "accepting":
			return "正常服务";
		case "unknown":
			return "状态未知";
		default:
			return "尚未接入";
	}
}

const modelLabels = {
	stopped: "已停止",
	starting: "启动中",
	ready: "已就绪",
	draining: "等待请求结束",
	stopping: "正在正常停止",
	error: "操作失败",
	unknown: "状态未知",
};

// 状态灯只跟随模型状态变化，不跟随后台读取请求闪烁。
const modelIndicatorColors = {
	stopped: "bg-slate-400",
	starting: "bg-amber-500",
	ready: "bg-emerald-500",
	draining: "bg-amber-500",
	stopping: "bg-amber-500",
	error: "bg-red-500",
	unknown: "bg-slate-400",
};

const workflowLabels: Record<SparkWorkflow["phase"], string> = {
	accepted: "已接受，等待确认设备状态",
	pause_intent: "暂停请求已记录，等待核实",
	waiting_idle: "等待任务空闲",
	model_intent: "模型请求已记录，等待核实",
	waiting_model: "等待模型结果确认",
	resume_intent: "恢复请求已记录，等待核实",
	waiting_resume: "等待新任务提交恢复确认",
	completed: "模型已就绪，新任务提交已恢复",
	completed_paused: "模型操作已完成，新任务保持暂停",
	attention: "需要管理员核对，流程不会自动重试",
};
function workflowProgressLabel(workflow: SparkWorkflow): string {
	if (workflow.kind !== "manual_maintenance") return workflowLabels[workflow.phase];
	switch (workflow.phase) {
		case "waiting_resume":
			return workflow.action === "pause" ? "等待新任务暂停确认" : "等待新任务提交恢复确认";
		case "completed_paused":
			return "新任务已暂停";
		case "completed":
			return "新任务提交已恢复";
		case "accepted":
		case "pause_intent":
		case "resume_intent":
		case "attention":
			return workflowLabels[workflow.phase];
		default:
			return "等待维护操作确认";
	}
}
function StartupHint({ startedAt }: { startedAt?: number }) {
	const [now, setNow] = useState(() => Date.now());
	useEffect(() => {
		if (!startedAt) return;
		setNow(Date.now());
		const timer = window.setInterval(() => setNow(Date.now()), 1000);
		return () => window.clearInterval(timer);
	}, [startedAt]);
	const seconds = startedAt ? Math.max(0, Math.floor(now / 1000) - startedAt) : 0;
	return (
		<div className="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
			<p>正在加载模型并检查可用性，可能需要数分钟；首次启动可能更久。</p>
			<p className="mt-1">就绪后会自动更新状态，无需反复刷新。可离开此页面，稍后回来查看。</p>
			{startedAt && (
				<p aria-live="off" className="mt-1 tabular-nums">
					本次启动流程已等待 {Math.floor(seconds / 60)} 分 {String(seconds % 60).padStart(2, "0")}{" "}
					秒
				</p>
			)}
		</div>
	);
}

function DeviceStatus() {
	const [workflowControl, setWorkflowControl] = useState<SparkWorkflowControl>();
	const [workflow, setWorkflow] = useState<SparkWorkflow>();
	const [workflowError, setWorkflowError] = useState(false);
	const trackedOperation = useRef<string | undefined>(undefined);
	const [data, setData] = useState<SparkStatus>();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");
	const request = useRef(0);
	const writing = useRef(false);
	const reading = useRef(false);
	const [confirm, setConfirm] = useState<"pause" | "resume" | "start" | "stop">();
	const [notice, setNotice] = useState("");
	const load = useCallback(async () => {
		if (reading.current) return;
		reading.current = true;
		const sequence = ++request.current;
		setBusy(true);
		setError("");
		// 读取时保留诊断展示；busy 与 reading 已撤销所有写入权限。
		setConfirm(undefined);
		try {
			const [status, control] = await Promise.allSettled([
				sparkApi.status(),
				sparkWorkflowApi.current(),
			]);
			if (request.current !== sequence) return;
			if (status.status === "fulfilled") setData(status.value);
			else {
				setData(undefined);
				setError(safeSparkFailure(status.reason));
			}
			if (control.status === "fulfilled") {
				setWorkflowControl(control.value);
				setWorkflowError(false);
				if (control.value.current) {
					trackedOperation.current = control.value.current.operation_id;
					setWorkflow(control.value.current);
				} else if (trackedOperation.current && control.value.enabled) {
					try {
						const op = await sparkWorkflowApi.get(trackedOperation.current);
						if (request.current === sequence) setWorkflow(op);
					} catch {
						if (request.current === sequence) setWorkflowError(true);
					}
				}
			} else {
				setWorkflowControl(undefined);
				setWorkflowError(true);
			}
		} catch (err) {
			if (request.current === sequence) {
				setData(undefined);
				setError(safeSparkFailure(err));
			}
		} finally {
			reading.current = false;
			if (request.current === sequence) setBusy(false);
		}
	}, []);
	const changeMaintenance = async () => {
		const c = data?.maintenance_control;
		if (
			writing.current ||
			reading.current ||
			workflowError ||
			!workflowControl ||
			workflowControl.current ||
			!data ||
			(confirm !== "pause" && confirm !== "resume") ||
			!c ||
			data.model_control?.busy ||
			Date.now() >= data.valid_until * 1000 ||
			(confirm === "pause" ? !c.can_pause : !c.can_resume)
		)
			return;
		writing.current = true;
		const active = confirm === "pause";
		const input = {
			active,
			boot_id: data.snapshot.boot_id,
			expected_revision: c.revision,
			operation_id: active ? crypto.randomUUID() : c.operation_id,
		};
		const sequence = ++request.current;
		setBusy(true);
		setConfirm(undefined);
		setData(undefined);
		setError("");
		setNotice("");
		try {
			await sparkApi.changeMaintenance(input);
			if (request.current !== sequence) return;
			setNotice("维护设置已写入，正在核对服务端和 Worker。该操作不启动或停止模型。");
			await load();
		} catch (err) {
			if (request.current === sequence) setError(safeSparkFailure(err, true));
		} finally {
			writing.current = false;
			if (request.current === sequence) setBusy(false);
		}
	};
	const modelAllowed = (action: "start" | "stop") => {
		if (workflowError || !workflowControl) return false;
		if (workflowControl.enabled)
			return Boolean(
				data &&
					!busy &&
					Date.now() < data.valid_until * 1000 &&
					!workflowControl.current &&
					(action === "start" ? workflowControl.can_start : workflowControl.can_stop),
			);
		return Boolean(
			data &&
				!busy &&
				Date.now() < data.valid_until * 1000 &&
				data.maintenance === "paused" &&
				data.maintenance_control?.active &&
				data.maintenance_control.confirmed &&
				!data.model_control?.busy &&
				data.workers.length > 0 &&
				data.workers.every(
					(w) =>
						w.state === "observed" &&
						w.maintenance === "paused" &&
						w.running_count === 0 &&
						w.waiting_count === 0,
				) &&
				(action === "start"
					? data.can_start_model &&
						data.model_control?.can_start &&
						data.model_control.state === "stopped"
					: data.can_stop_model &&
						data.model_control?.can_stop &&
						data.model_control.state === "ready"),
		);
	};
	const changeModel = async () => {
		if (
			writing.current ||
			reading.current ||
			!data?.model_control ||
			!data.maintenance_control ||
			(confirm !== "start" && confirm !== "stop") ||
			!modelAllowed(confirm)
		)
			return;
		writing.current = true;
		const input = {
			action: confirm,
			boot_id: data.snapshot.boot_id,
			expected_revision: data.model_control.revision,
			maintenance_revision: data.maintenance_control.revision,
			operation_id: crypto.randomUUID(),
		};
		const sequence = ++request.current;
		setBusy(true);
		setConfirm(undefined);
		setData(undefined);
		setError("");
		setNotice("");
		try {
			if (workflowControl?.enabled) {
				trackedOperation.current = input.operation_id;
				const op = await sparkWorkflowApi.begin({
					operation_id: input.operation_id,
					action: input.action,
					boot_id: input.boot_id,
					expected_model_revision: input.expected_revision,
					expected_maintenance_revision: input.maintenance_revision,
				});
				if (request.current !== sequence) return;
				setWorkflow(op);
				setWorkflowControl({
					enabled: true,
					current: op,
					can_start: false,
					can_stop: false,
				});
				setNotice("操作已保存，关闭页面后仍可重新打开查看进度。");
			} else {
				await sparkApi.changeModel(input);
				if (request.current !== sequence) return;
				setNotice("模型操作已提交，正在读取实际状态；维护模式继续保持。");
			}
		} catch (err) {
			if (request.current !== sequence) return;
			setNotice(safeSparkFailure(err, true));
		} finally {
			writing.current = false;
			if (request.current === sequence) await load();
		}
	};
	useEffect(() => {
		if ((!data?.model_control?.busy && !workflowControl?.current) || busy) return;
		const timer = window.setTimeout(() => void load(), 2000);
		return () => window.clearTimeout(timer);
	}, [data, workflowControl, busy, load]);

	useEffect(() => {
		// 保持空闲前台状态新鲜，确认与写操作期间不打断用户。
		const refresh = () => {
			if (busy || confirm || writing.current || document.visibilityState === "hidden") return;
			void load();
		};
		const timer =
			data && !data.model_control?.busy && !busy && !confirm
				? window.setTimeout(refresh, 15000)
				: undefined;
		window.addEventListener("focus", refresh);
		window.addEventListener("online", refresh);
		document.addEventListener("visibilitychange", refresh);
		return () => {
			window.clearTimeout(timer);
			window.removeEventListener("focus", refresh);
			window.removeEventListener("online", refresh);
			document.removeEventListener("visibilitychange", refresh);
		};
	}, [data, busy, confirm, load]);

	useEffect(() => {
		void load();
		return () => {
			++request.current;
			reading.current = false;
		};
	}, [load]);
	useEffect(() => {
		if (!data) return;
		const timer = window.setTimeout(
			() => {
				setData(undefined);
				setConfirm(undefined);
				setError("设备状态已过期，请刷新后重新确认。");
			},
			Math.max(0, data.valid_until * 1000 - Date.now()),
		);
		return () => window.clearTimeout(timer);
	}, [data]);
	const isStarting =
		data?.model_control?.state === "starting" &&
		!workflowError &&
		!workflow?.needs_attention &&
		!data.model_control.error_code;
	const startupOperation = workflowControl?.current;
	const startupStartedAt =
		startupOperation?.action === "start" ? startupOperation.created_at : undefined;
	const tasks = !data
		? "状态未知"
		: data.maintenance_control?.confirmed
			? data.maintenance_control.active
				? "已暂停"
				: "允许提交"
			: "状态待确认";
	const guidance = !data
		? "请刷新状态后再操作。"
		: !data.model_control
			? "未配置可管理的本机模型，请联系管理员完成登记。"
			: data.model_control.busy
				? "模型操作正在进行，页面会自动更新；请勿重复提交。"
				: !data.maintenance_control
					? "此设备尚未启用维护控制，请联系管理员升级或配置管理服务。"
					: !data.maintenance_control.active
						? "当前部署需先暂停新任务：展开「高级维护」进入维护模式，等待任务完成后再启停模型。"
						: !data.maintenance_control.confirmed
							? "正在等待服务端和 Worker 确认，请刷新状态。"
							: data.workers.length === 0 ||
									data.workers.some(
										(w) =>
											w.state !== "observed" ||
											w.maintenance !== "paused" ||
											w.running_count !== 0 ||
											w.waiting_count !== 0,
									)
								? "任务尚未完成或状态无法确认，请稍后刷新；状态未知时请联系管理员。"
								: modelAllowed("start") || modelAllowed("stop")
									? "模型启停不会自动恢复新任务。确认模型可用后，可在「高级维护」恢复服务。"
									: "设备暂未允许模型操作，请刷新状态；仍不可用时请联系管理员查看诊断。";
	return (
		<div className="mt-4 space-y-4">
			<div aria-live="polite" className="space-y-3">
				<p className="flex items-center gap-2 font-medium text-slate-900">
					<span
						aria-hidden="true"
						className={`h-3 w-3 shrink-0 rounded-full ${
							isStarting
								? "border-2 border-amber-200 border-t-amber-600 motion-safe:animate-spin"
								: modelIndicatorColors[data?.model_control?.state ?? "unknown"]
						}`}
					/>
					<span>
						本机模型：
						{data?.model_control
							? `${data.model_control.label} · ${modelLabels[data.model_control.state]}`
							: data
								? "未配置可管理的本机模型"
								: "状态未知"}
					</span>
				</p>
				{isStarting && <StartupHint startedAt={startupStartedAt} />}
				<p className="text-sm text-slate-700">新任务：{tasks}</p>
				<p className="text-sm text-slate-600">
					{workflowControl?.enabled ? "操作前会暂停新任务，并等待当前任务完成。" : guidance}
				</p>
				{workflow && (
					<p className="text-sm text-slate-700">
						操作进度：{workflowProgressLabel(workflow)}
						{workflow.phase === "completed_paused"
							? "。需要接收新任务时，请在高级维护中明确恢复服务。"
							: ""}
					</p>
				)}
				{workflowError && (
					<p role="status" className="text-sm text-amber-900">
						暂时无法读取操作进度，请刷新核对；暂不允许新操作。
					</p>
				)}
				{notice && <p className="text-sm text-slate-600">{notice}</p>}
				{error && (
					<p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900" role="status">
						{error}
					</p>
				)}
				{data?.model_control?.error_code && (
					<p role="status" className="text-sm text-amber-900">
						{modelErrorMessage(data.model_control.error_code)}
					</p>
				)}
			</div>
			<div className="flex flex-wrap gap-2">
				{(["start", "stop"] as const).map((action) => (
					<button
						key={action}
						type="button"
						className={buttonStyle}
						disabled={!modelAllowed(action)}
						onClick={() => setConfirm(action)}
					>
						{action === "start" ? "启动本机模型" : "停止本机模型"}
					</button>
				))}
				<button
					type="button"
					className={buttonStyle.replace(" disabled:opacity-45", "")}
					disabled={busy}
					onClick={() => void load()}
				>
					刷新状态
				</button>
			</div>
			{(confirm === "start" || confirm === "stop") && data?.model_control && (
				<div
					role="alertdialog"
					aria-label="确认本机模型操作"
					className="rounded-lg bg-amber-50 p-3 space-y-2"
				>
					<p>
						确认{confirm === "start" ? "启动" : "正常停止"}本机模型「
						{data.model_control.label}」？
						{confirm === "start"
							? workflowControl?.enabled
								? "确认模型就绪后，只恢复本次操作暂停的新任务。设备原本处于维护模式时，启动模型后仍需手动恢复服务。"
								: "启动后等待健康检查确认就绪，新任务仍会保持暂停。"
							: "停止后将无法使用本机模型，新任务保持暂停。先等待现有请求结束，再正常停止模型；不会强制结束进程。"}
					</p>
					<button
						type="button"
						className={buttonStyle}
						disabled={!modelAllowed(confirm)}
						onClick={() => void changeModel()}
					>
						{confirm === "start" ? "确认启动模型" : "确认停止模型"}
					</button>
					<button type="button" className={buttonStyle} onClick={() => setConfirm(undefined)}>
						取消
					</button>
				</div>
			)}
			{data?.maintenance_control && (
				<details className="border-t border-slate-100 pt-3 text-sm">
					<summary className="cursor-pointer text-slate-700">高级维护</summary>
					<div className="mt-3 space-y-3">
						<p className="text-slate-600">
							进入维护模式后不再接受新任务，正在运行的任务继续处理。恢复服务仅重新允许任务提交，不启动模型；请先确认所需模型可用。
						</p>
						{!data.maintenance_control.confirmed && <p>等待服务端和 Worker 确认，请刷新状态。</p>}
						<div className="flex gap-2">
							<button
								type="button"
								className={buttonStyle}
								disabled={
									busy ||
									workflowError ||
									!workflowControl ||
									Boolean(workflowControl.current) ||
									data.model_control?.busy ||
									!data.maintenance_control.can_pause
								}
								onClick={() => setConfirm("pause")}
							>
								进入维护模式
							</button>
							<button
								type="button"
								className={buttonStyle}
								disabled={
									busy ||
									workflowError ||
									!workflowControl ||
									Boolean(workflowControl.current) ||
									data.model_control?.busy ||
									!data.maintenance_control.can_resume
								}
								onClick={() => setConfirm("resume")}
							>
								恢复服务
							</button>
						</div>
						{(confirm === "pause" || confirm === "resume") && (
							<div
								role="alertdialog"
								aria-label="确认维护操作"
								className="rounded-lg bg-amber-50 p-3 space-y-2"
							>
								<p>
									{confirm === "pause"
										? "确认让当前 Spark 进入维护模式？所有使用该服务的用户将暂时无法提交新任务。"
										: "确认恢复当前 Spark 的任务服务？请先确认所需模型和依赖可用。"}
								</p>
								<button
									type="button"
									className={buttonStyle}
									disabled={busy}
									onClick={() => void changeMaintenance()}
								>
									{confirm === "pause" ? "确认进入维护" : "确认恢复服务"}
								</button>
								<button type="button" className={buttonStyle} onClick={() => setConfirm(undefined)}>
									取消
								</button>
							</div>
						)}
					</div>
				</details>
			)}
			{(data || workflow) && <SparkDiagnostics data={data} workflow={workflow} />}
		</div>
	);
}

function SparkDiagnostics({ data, workflow }: { data?: SparkStatus; workflow?: SparkWorkflow }) {
	const [copyStatus, setCopyStatus] = useState<"idle" | "copying" | "copied" | "failed">("idle");
	const generation = useRef(0);
	const workflowText = workflow ? sparkWorkflowDiagnosticText(workflow) : "";
	const text = [data ? sparkDiagnosticText(data) : "设备观察：暂时不可用", workflowText]
		.filter(Boolean)
		.join("\n");
	useEffect(() => {
		++generation.current;
		// 手动复制回退保留并更新内容，旧快照的复制结果不能标记新快照。
		setCopyStatus((status) => (status === "failed" ? "failed" : "idle"));
		return () => {
			++generation.current;
		};
	}, [text]);
	const copy = async () => {
		const current = generation.current;
		setCopyStatus("copying");
		try {
			await navigator.clipboard.writeText(text);
			if (generation.current === current) setCopyStatus("copied");
		} catch {
			if (generation.current === current) setCopyStatus("failed");
		}
	};
	return (
		<details className="border-t border-slate-100 pt-3 text-sm">
			<summary className="cursor-pointer text-slate-700">诊断详情</summary>
			<div className="mt-3 space-y-3 text-slate-600">
				{data && (
					<>
						<p>
							采集时间：
							{new Date(data.snapshot.observed_at * 1000).toLocaleTimeString()} · 工作负载清单：
							{data.snapshot.inventory_complete ? "采集完成" : "不完整，部分状态未知"}
						</p>
						<p>服务端：{maintenanceLabel(data.maintenance)}</p>
						{data.workers.length === 0 ? (
							<p>本机 Worker 尚未登记，任务状态未知。</p>
						) : (
							data.workers.map((w) => (
								<p key={w.worker_id}>
									Worker {w.worker_id}：
									{w.state === "unknown"
										? "状态未知"
										: `运行 ${w.running_count ?? "未知"}，排队 ${w.waiting_count ?? "未知"}${w.state === "busy" ? "（仍有待处理任务）" : "（当前快照）"}`}{" "}
									· Worker 服务：{maintenanceLabel(w.maintenance)}
								</p>
							))
						)}
						{data.model_control?.error_code && (
							<p>模型诊断代码：{modelErrorCode(data.model_control.error_code)}</p>
						)}
						<ul>
							{safeSparkChecks(data).map((check) => (
								<li key={check.code}>{check.label}</li>
							))}
						</ul>
						<ul className="max-h-60 space-y-1 overflow-y-auto">
							{data.snapshot.components.map((c, index) => (
								<li key={`${c.kind}:${c.id}`} className="break-all">
									工作负载 {index + 1} · {c.kind === "docker" ? "Docker" : "Kubernetes"} ·{" "}
									{safeComponentState(c.state)}
									{c.registered ? "" : " · 未登记启停策略"}
								</li>
							))}
						</ul>
					</>
				)}
				{workflowText && <pre className="whitespace-pre-wrap break-all">{workflowText}</pre>}
				<p className="text-xs">
					复制内容仅含状态、计数、操作引用和已知诊断代码，不含账户、地址、设备标识或原始响应。
				</p>
				<button
					type="button"
					className={buttonStyle}
					disabled={copyStatus === "copying"}
					onClick={() => void copy()}
				>
					复制诊断
				</button>
				<span role="status">
					{copyStatus === "copied"
						? "已复制诊断"
						: copyStatus === "failed"
							? "复制失败，请选取下方文本手动复制。"
							: ""}
				</span>
				{copyStatus === "failed" && (
					<textarea
						aria-label="可手动复制的诊断信息"
						readOnly
						value={text}
						rows={10}
						className="w-full rounded-lg border border-slate-300 p-2 font-mono text-xs"
						onFocus={(event) => event.currentTarget.select()}
					/>
				)}
			</div>
		</details>
	);
}
