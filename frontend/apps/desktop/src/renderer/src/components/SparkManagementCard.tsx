import { API_BASE_URL, type SparkStatus, sparkApi, useAuthStore } from "@leros/store";
import { useCallback, useEffect, useRef, useState } from "react";

const buttonStyle =
	"rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-45";

export function SparkManagementCard() {
	const user = useAuthStore((s) => s.authUser);
	const org = user?.organizations?.find((o) => o.id === user.currentOrg?.id) ?? user?.currentOrg;
	const canView = Boolean(user && org?.isAdmin);
	// 切换身份时卸载旧卡片，旧请求不能把上一组织的状态带入新页面。
	const identity = `${user?.publicId ?? ""}:${org?.id ?? ""}:${canView}:${user?.jwtToken ?? ""}`;
	return (
		<section
			className="rounded-2xl border border-slate-200 bg-white p-6 shadow-sm"
			aria-label="Spark 设备管理"
		>
			<h2 className="text-lg font-semibold text-slate-950">Spark 设备管理</h2>
			<p className="mt-1 text-sm text-slate-600">
				查看当前连接设备的服务和任务状态。预检不会停止服务。
			</p>
			{canView ? (
				<DeviceStatus key={identity} />
			) : (
				<p className="mt-4 text-sm text-slate-600">请使用设备所属组织的管理员账号查看。</p>
			)}
		</section>
	);
}

function maintenanceLabel(state: SparkStatus["maintenance"]) {
	switch (state) {
		case "paused":
			return "已暂停";
		case "accepting":
			return "正常接单";
		case "unknown":
			return "状态未知";
		default:
			return "尚未接入";
	}
}

function DeviceStatus() {
	const [data, setData] = useState<SparkStatus>();
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");
	const [preflight, setPreflight] = useState(false);
	const request = useRef(0);
	const load = useCallback(async (check = false) => {
		const sequence = ++request.current;
		setBusy(true);
		setError("");
		setData(undefined);
		setPreflight(false);
		try {
			const result = check ? (await sparkApi.preflight()).status : await sparkApi.status();
			if (request.current !== sequence) return;
			setData(result);
			setPreflight(check);
		} catch (err) {
			if (request.current === sequence)
				setError(err instanceof Error ? err.message : "设备状态未知，请刷新。");
		} finally {
			if (request.current === sequence) setBusy(false);
		}
	}, []);
	useEffect(() => {
		void load();
		return () => {
			++request.current;
		};
	}, [load]);
	useEffect(() => {
		if (!data) return;
		const timer = window.setTimeout(
			() => {
				setData(undefined);
				setPreflight(false);
				setError("设备状态已过期，请刷新后重新预检。");
			},
			Math.max(0, data.valid_until * 1000 - Date.now()),
		);
		return () => window.clearTimeout(timer);
	}, [data]);
	return (
		<div className="mt-4 space-y-4">
			<p className="break-all text-xs text-slate-500">当前后端：{API_BASE_URL}</p>
			<div className="flex flex-wrap gap-2">
				<button type="button" className={buttonStyle} disabled={busy} onClick={() => void load()}>
					刷新状态
				</button>
				<button
					type="button"
					className={buttonStyle}
					disabled={busy || !data}
					onClick={() => void load(true)}
				>
					关机预检
				</button>
			</div>
			<div aria-live="polite">
				{busy && <p className="text-sm text-slate-600">正在读取设备状态…</p>}
				{error && (
					<p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-900" role="status">
						{error}
					</p>
				)}
				{data && (
					<div className="space-y-4">
						<dl className="grid gap-2 text-sm sm:grid-cols-2">
							<div>
								<dt className="text-slate-500">设备</dt>
								<dd className="font-medium text-slate-900">{data.snapshot.hostname}</dd>
							</div>
							<div>
								<dt className="text-slate-500">采集时间</dt>
								<dd>{new Date(data.snapshot.observed_at * 1000).toLocaleTimeString()}</dd>
							</div>
							<div>
								<dt className="text-slate-500">本机模型</dt>
								<dd>未配置可管理的本机模型</dd>
							</div>
							<div>
								<dt className="text-slate-500">工作负载清单</dt>
								<dd>{data.snapshot.inventory_complete ? "采集完成" : "不完整，部分状态未知"}</dd>
							</div>
						</dl>
						<div className="text-sm text-slate-700">
							<p>服务端接单：{maintenanceLabel(data.maintenance)}</p>
							{data.workers.length === 0 ? (
								<p>本机 Worker 尚未登记，任务状态未知。</p>
							) : (
								data.workers.map((w) => (
									<p key={w.worker_id}>
										Worker {w.worker_id}：
										{w.state === "unknown"
											? "状态未知"
											: `运行 ${w.running_count ?? "未知"}，排队 ${w.waiting_count ?? "未知"}${w.state === "busy" ? "（仍有待处理任务）" : "（当前快照）"}`}
										<span> · Worker 接单：{maintenanceLabel(w.maintenance)}</span>
									</p>
								))
							)}
						</div>
						<div className="rounded-lg bg-amber-50 p-3 text-sm text-amber-950">
							<p className="font-medium">
								{preflight ? "预检完成：暂不满足关机条件" : "当前仅支持只读预检"}
							</p>
							<ul className="mt-2 list-disc space-y-1 pl-5">
								{data.checks.map((c) => (
									<li key={c.code}>{c.message}</li>
								))}
							</ul>
						</div>
						{data.snapshot.components.length > 0 && (
							<details className="text-sm">
								<summary className="cursor-pointer text-slate-700">
									查看工作负载（{data.snapshot.components.length}）
								</summary>
								<ul className="mt-2 max-h-60 space-y-1 overflow-y-auto text-slate-600">
									{data.snapshot.components.map((c) => (
										<li key={`${c.kind}:${c.id}`} className="break-all">
											{c.name} · {c.kind} · {c.state}
											{c.registered ? "" : " · 未登记启停策略"}
										</li>
									))}
								</ul>
							</details>
						)}
					</div>
				)}
			</div>
			<div className="flex flex-wrap gap-2 border-t border-slate-100 pt-4">
				{["启动本机模型", "停止本机模型", "安全关机"].map((label) => (
					<button
						key={label}
						type="button"
						className={buttonStyle}
						disabled
						title="完成任务维护、服务停止和恢复能力验证后启用"
					>
						{label}
					</button>
				))}
			</div>
			<p className="text-xs leading-5 text-slate-500">
				启停和关机能力尚未启用。设备断连不能证明关机已完成。
			</p>
		</div>
	);
}
