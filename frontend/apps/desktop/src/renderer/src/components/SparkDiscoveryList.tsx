import { useEffect, useRef, useState } from "react";
import type { DiscoveredServer } from "../../../shared/discovery";

export function SparkDiscoveryList({
	onSelect,
	disabled,
	autoStart = false,
}: {
	onSelect: (url: string) => void;
	disabled: boolean;
	autoStart?: boolean;
}) {
	const [servers, setServers] = useState<DiscoveredServer[]>([]);
	const [searching, setSearching] = useState(false);
	const [message, setMessage] = useState("");
	const active = useRef(true);
	const busy = useRef(false);
	async function search() {
		if (busy.current) return;
		busy.current = true;
		setSearching(true);
		setServers([]);
		setMessage("");
		try {
			const result = await window.lerosDiscovery.scan();
			if (!active.current) return;
			setServers(result.servers);
			setMessage(
				result.error ||
					(result.servers.length
						? "请选择设备填入地址，再测试并保存。"
						: "暂未发现设备，请检查是否在同一局域网，或手动输入地址。"),
			);
		} catch {
			if (active.current) setMessage("无法搜索，可继续手动输入地址。");
		} finally {
			busy.current = false;
			if (active.current) setSearching(false);
		}
	}
	useEffect(() => {
		active.current = true;
		if (autoStart) void search();
		return () => {
			active.current = false;
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
					disabled={searching || disabled}
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
						disabled={disabled || s.conflict}
						onClick={() => onSelect(s.url)}
						className="block w-full rounded border border-slate-200 p-2 text-left text-sm hover:bg-slate-50 disabled:opacity-50"
					>
						<span className="block">
							{s.name} · {s.id.slice(-6)}
						</span>
						<span className="block break-all text-xs text-slate-500">{s.url}</span>
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

export const renderSparkDiscovery = (onSelect: (url: string) => void, disabled: boolean) => (
	<SparkDiscoveryList onSelect={onSelect} disabled={disabled} />
);
export const renderInitialSparkDiscovery = (onSelect: (url: string) => void, disabled: boolean) => (
	<SparkDiscoveryList onSelect={onSelect} disabled={disabled} autoStart />
);
