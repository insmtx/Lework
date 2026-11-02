import { API_BASE_URL, inspectSparkDevice } from "@leros/store";
import { useEffect, useRef, useState } from "react";
import type { DiscoveredServer } from "../../../shared/discovery";

export function SparkDiscoveryList({
	onSelect,
	disabled,
	autoStart = false,
}: {
	onSelect: (url: string) => void | Promise<void>;
	disabled: boolean;
	autoStart?: boolean;
}) {
	const [servers, setServers] = useState<DiscoveredServer[]>([]);
	const [searching, setSearching] = useState(false);
	const [connecting, setConnecting] = useState(false);
	const [message, setMessage] = useState("");
	const active = useRef(true);
	const disabledRef = useRef(disabled);
	disabledRef.current = disabled;
	const busy = useRef(false);
	const selecting = useRef(false);
	const generation = useRef(0);
	const controller = useRef<AbortController | null>(null);
	const expires = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
	const lastSearch = useRef(0);
	function invalidate() {
		generation.current++;
		controller.current?.abort();
		controller.current = null;
		clearTimeout(expires.current);
		busy.current = false;
		selecting.current = false;
		setSearching(false);
		setConnecting(false);
		setServers([]);
	}
	async function search() {
		if (!window.lerosDiscovery || busy.current || selecting.current) return;
		invalidate();
		busy.current = true;
		setSearching(true);
		setMessage("");
		const revision = generation.current;
		lastSearch.current = Date.now();
		try {
			const result = await window.lerosDiscovery.scan();
			if (!active.current || revision !== generation.current) return;
			setServers(result.servers);
			setMessage(
				result.error ||
					(result.servers.length
						? "选择设备即可连接，也可手动输入地址。"
						: "暂未发现设备。首次网线连接可能需要等待系统自动取址，请稍候重试；也可手动输入地址。"),
			);
			expires.current = setTimeout(() => {
				if (!active.current || revision !== generation.current) return;
				invalidate();
				window.dispatchEvent(new Event("leros:connection-invalidated"));
				setMessage("发现结果已过期，请重新搜索；也可手动输入地址。");
			}, 30000);
		} catch {
			if (active.current && revision === generation.current)
				setMessage("无法搜索，可继续手动输入地址。");
		} finally {
			if (active.current && revision === generation.current) {
				busy.current = false;
				setSearching(false);
			}
		}
	}
	async function select(server: DiscoveredServer) {
		if (disabled || server.conflict || selecting.current || busy.current) return;
		selecting.current = true;
		setConnecting(true);
		setMessage("正在验证连接…");
		const revision = generation.current;
		const abort = new AbortController();
		controller.current = abort;
		const timer = setTimeout(() => abort.abort(), 8000);
		try {
			// 当前路径可用时优先保留；换网后也验证新候选，避免旧主机名失效阻断重连。
			const paths = server.candidates ?? [{ url: server.url, transport: "hostname" as const }];
			const current = paths.find((p) => new URL(p.url).href === new URL(API_BASE_URL).href);
			const choices = current
				? [current, ...paths.filter((path) => path.url !== current.url)].slice(0, 4)
				: paths.slice(0, 4);
			const checked = await Promise.all(
				choices.map(async (path) => {
					try {
						const device = await inspectSparkDevice(path.url, abort.signal);
						// 广播 ID 只能用于候选一致性检查，不构成认证。旧部署仅主机名回退兼容 host_id。
						return path.transport === "hostname" || device?.id === server.id ? path : null;
					} catch {
						return null;
					}
				}),
			);
			if (!active.current || revision !== generation.current) return;
			const rank = { "link-local": 0, "local-network": 1, hostname: 2 };
			if (disabledRef.current) return;
			const verified = checked
				.filter((p) => p !== null)
				.sort((a, b) => {
					if (a.url === current?.url) return -1;
					if (b.url === current?.url) return 1;
					return rank[a.transport] - rank[b.transport];
				});
			if (!verified.length) {
				setMessage("无法验证设备连接，请重试或手动输入已确认的地址。");
				return;
			}
			clearTimeout(timer);
			await onSelect(verified[0].url);
			if (active.current && revision === generation.current) setMessage("");
		} catch {
			if (active.current && revision === generation.current)
				setMessage("连接未完成，请重试或手动输入地址。");
		} finally {
			clearTimeout(timer);
			if (active.current && revision === generation.current) {
				selecting.current = false;
				setConnecting(false);
			}
		}
	}
	useEffect(() => {
		active.current = true;
		const reset = () => {
			invalidate();
			window.dispatchEvent(new Event("leros:connection-invalidated"));
			void search();
		};
		const offline = () => {
			invalidate();
			window.dispatchEvent(new Event("leros:connection-invalidated"));
			setMessage("网络状态已改变，可重新搜索局域网或手动连接。");
		};
		const focus = () => {
			if (Date.now() - lastSearch.current > 30000) reset();
		};
		const cancelled = () => {
			invalidate();
			setMessage("可继续手动连接，或重新搜索设备。");
		};
		window.addEventListener("leros:connection-selection-cancelled", cancelled);
		const unsubscribe = window.lerosDiscovery?.subscribeReset?.(reset);
		window.addEventListener("online", reset);
		window.addEventListener("offline", offline);
		window.addEventListener("focus", focus);
		if (autoStart) void search();
		return () => {
			active.current = false;
			generation.current++;
			controller.current?.abort();
			clearTimeout(expires.current);
			busy.current = false;
			selecting.current = false;
			unsubscribe?.();
			window.removeEventListener("leros:connection-selection-cancelled", cancelled);
			window.removeEventListener("online", reset);
			window.removeEventListener("offline", offline);
			window.removeEventListener("focus", focus);
		};
	}, [autoStart]);
	if (!window.lerosDiscovery) return null;
	return (
		<div className="space-y-3 rounded-lg border border-slate-200 p-3">
			<div className="flex items-center justify-between gap-3">
				<span className="text-sm font-medium">附近的设备</span>
				<button
					type="button"
					className="text-sm text-indigo-600 disabled:opacity-50"
					disabled={searching || connecting || disabled}
					onClick={() => void search()}
				>
					{searching ? "正在搜索…" : "搜索局域网设备"}
				</button>
			</div>
			{searching && (
				<p className="text-xs text-slate-500" role="status">
					正在搜索，约需 8 秒。你仍可手动输入地址。
				</p>
			)}
			<div className="max-h-48 space-y-2 overflow-y-auto">
				{servers.map((s) => (
					<button
						key={`${s.id}|${s.url}`}
						type="button"
						disabled={disabled || connecting || s.conflict}
						onClick={() => void select(s)}
						className="block w-full rounded border border-slate-200 p-2 text-left text-sm hover:bg-slate-50 disabled:opacity-50"
					>
						<span className="block">
							{s.name} · {s.id.slice(-6)}
						</span>
						<span className="block break-all text-xs text-slate-500">{s.url}</span>
						{s.candidates?.some((p) => p.transport === "link-local") && (
							<span className="block text-xs text-slate-500">检测到网线直连候选地址</span>
						)}
						{s.conflict && (
							<span className="text-xs text-red-600">
								设备标识冲突，请联系管理员或手填确认的地址。
							</span>
						)}
					</button>
				))}
			</div>
			{message && (
				<p role="status" className="text-xs text-slate-500">
					{message}
				</p>
			)}
		</div>
	);
}

export const renderSparkDiscovery = (
	onSelect: (url: string) => void | Promise<void>,
	disabled: boolean,
) => <SparkDiscoveryList onSelect={onSelect} disabled={disabled} />;
export const renderInitialSparkDiscovery = (
	onSelect: (url: string) => void | Promise<void>,
	disabled: boolean,
) => <SparkDiscoveryList onSelect={onSelect} disabled={disabled} autoStart />;
