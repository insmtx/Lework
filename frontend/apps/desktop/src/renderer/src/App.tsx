import {
	CLIENT_UPGRADE_REQUIRED_EVENT,
	type ClientUpdatePolicy,
	type ClientUpgradeRequiredEvent,
	clientUpdateApi,
} from "@leros/store";
import { ThemeProvider } from "@leros/ui/components/common/theme-provider";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Toaster } from "@leros/ui/components/ui/sonner";
import { Download, Loader2, RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { HashRouter } from "react-router-dom";
import { toast } from "sonner";
import type { DesktopUpdateState } from "../../shared/auto-update";
import { AppRoutes } from "./routes";

const initialUpdateState: DesktopUpdateState = {
	currentVersion: "0.0.0",
	phase: "idle",
	message: "正在读取更新状态",
	canCheck: false,
	canRestart: false,
};

const versionUpdateImageSrc = new URL(
	"../../../resources/octopus_version_update.png",
	import.meta.url,
).href;
const forcedUpdateRefreshIntervalMs = 2 * 60 * 1000;

export default function App() {
	return (
		<HashRouter>
			<ThemeProvider defaultTheme="system">
				<MacTitleBarDragRegion />
				<AppRoutes />
				<ClientUpdateGate />
				<Toaster />
			</ThemeProvider>
		</HashRouter>
	);
}

// mac 沉浸式标题栏：仅在 mac 下给 body 挂上标记类，
// 具体的拖拽区/红绿灯让位样式全部由 globals.css 中的 body.leros-mac-titlebar 规则控制，
// 拖拽能力通过侧栏品牌行与右侧顶栏的 -webkit-app-region 精细划分，不再使用整块覆盖层。
function MacTitleBarDragRegion() {
	const isMac = window.electron?.process?.platform === "darwin";

	useEffect(() => {
		if (!isMac) return;
		document.body.classList.add("leros-mac-titlebar");
		return () => {
			document.body.classList.remove("leros-mac-titlebar");
		};
	}, [isMac]);

	return null;
}

function ClientUpdateGate() {
	const [policy, setPolicy] = useState<ClientUpdatePolicy | null>(null);
	const [updateState, setUpdateState] = useState<DesktopUpdateState>(initialUpdateState);
	const [checking, setChecking] = useState(false);
	const [restarting, setRestarting] = useState(false);

	useEffect(() => {
		const handleUpgradeRequired = (event: Event) => {
			setPolicy((event as ClientUpgradeRequiredEvent).detail);
		};

		window.addEventListener(CLIENT_UPGRADE_REQUIRED_EVENT, handleUpgradeRequired);

		void clientUpdateApi.reportVersion().catch(() => {
			// Version reporting must not block app startup when the server is temporarily unavailable.
		});

		void window.lerosDesktop
			.getState()
			.then(setUpdateState)
			.catch(() => undefined);
		const unsubscribe = window.lerosDesktop.subscribe(setUpdateState);

		return () => {
			window.removeEventListener(CLIENT_UPGRADE_REQUIRED_EVENT, handleUpgradeRequired);
			unsubscribe();
		};
	}, []);

	useEffect(() => {
		if (!policy?.force_update) {
			return;
		}

		let cancelled = false;

		const refreshUpdateData = async () => {
			await clientUpdateApi.reportVersion().catch(() => {
				// Keep the existing dialog state when the policy endpoint is temporarily unavailable.
			});

			const state = await window.lerosDesktop.getState().catch(() => null);
			if (!state || cancelled) {
				return;
			}

			setUpdateState(state);
			if (!state.canCheck || state.phase === "checking" || state.phase === "downloading") {
				return;
			}

			const nextState = await window.lerosDesktop.checkForUpdates().catch(() => null);
			if (nextState && !cancelled) {
				setUpdateState(nextState);
			}
		};

		void refreshUpdateData();
		const intervalId = window.setInterval(refreshUpdateData, forcedUpdateRefreshIntervalMs);

		return () => {
			cancelled = true;
			window.clearInterval(intervalId);
		};
	}, [policy?.force_update]);

	if (!policy?.force_update) {
		return null;
	}

	const handleCheckForUpdates = async () => {
		setChecking(true);
		try {
			const nextState = await window.lerosDesktop.checkForUpdates();
			setUpdateState(nextState);
			if (nextState.phase === "unsupported") {
				toast.message(nextState.message);
			}
			if (nextState.phase === "up-to-date") {
				toast.message("当前版本已低于服务端最低要求，请等待新版本发布");
			}
		} finally {
			setChecking(false);
		}
	};

	const handleRestart = async () => {
		setRestarting(true);
		try {
			const accepted = await window.lerosDesktop.quitAndInstall();
			if (!accepted) {
				toast.message("更新尚未下载完成，请先检查更新");
			}
		} finally {
			setRestarting(false);
		}
	};

	const message = policy.message || "当前客户端版本过低，请更新后继续使用";
	const currentVersion = policy.current_version || updateState.currentVersion;
	const targetVersion =
		updateState.availableVersion ||
		updateState.downloadedVersion ||
		policy.latest_version ||
		policy.min_supported_version ||
		"最新版本";
	const statusMessage =
		updateState.phase === "up-to-date"
			? "暂未发现可下载的新版本，请稍后重试"
			: updateState.message || "检查更新后即可下载并安装";

	return (
		<Dialog open onOpenChange={() => undefined}>
			<DialogContent
				className="!max-w-[min(92vw,520px)] overflow-hidden rounded-2xl border-[#4F46E5]/20 bg-[#F7F6FF] p-0 shadow-[0_22px_60px_rgba(79,70,229,0.24)]"
				showCloseButton={false}
			>
				<div className="relative overflow-hidden border-b border-white/70 bg-[#EEEDFF] px-6 pb-6 pt-6">
					<div className="absolute -right-10 -top-10 h-44 w-44 rounded-full bg-white/55 blur-2xl" />
					<div className="absolute -left-10 bottom-0 h-36 w-36 rounded-full bg-[#DCD8FF]/70 blur-2xl" />
					<div className="relative flex flex-col items-center text-center">
						<img
							src={versionUpdateImageSrc}
							alt=""
							className="h-24 w-24 object-contain"
							aria-hidden="true"
						/>
						<DialogHeader className="mt-4 items-center gap-2 text-center">
							<DialogTitle className="text-[28px] font-semibold leading-tight tracking-normal text-slate-950">
								需要更新客户端
							</DialogTitle>
							<DialogDescription className="max-w-[360px] text-sm leading-5 text-slate-700">
								{message}
							</DialogDescription>
						</DialogHeader>
					</div>
				</div>

				<div className="space-y-4 px-5 py-5">
					<div className="rounded-xl border border-white/80 bg-white/75 p-3.5 shadow-sm">
						<div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
							<div className="min-w-0">
								<div className="flex flex-wrap items-center gap-2 text-sm">
									<span className="font-semibold text-slate-950">v{currentVersion}</span>
									<span className="text-slate-300">→</span>
									<span className="font-semibold text-[#4F46E5]">v{targetVersion}</span>
								</div>
								<div className="mt-1 flex items-center gap-1.5 text-xs text-slate-500">
									<Download className="size-3.5 text-[#4F46E5]" />
									<span className="truncate">{statusMessage}</span>
								</div>
							</div>

							{updateState.canRestart ? (
								<Button
									className="h-8 shrink-0 rounded-full bg-slate-950 px-3.5 text-sm font-semibold text-white shadow-[0_8px_18px_rgba(15,23,42,0.2)] hover:bg-slate-800"
									onClick={handleRestart}
									disabled={restarting}
								>
									{restarting ? (
										<Loader2 className="size-3.5 animate-spin" />
									) : (
										<RotateCcw className="size-3.5" />
									)}
									重启安装
								</Button>
							) : (
								<Button
									className="h-8 shrink-0 rounded-full bg-slate-950 px-3.5 text-sm font-semibold text-white shadow-[0_8px_18px_rgba(15,23,42,0.2)] hover:bg-slate-800"
									onClick={handleCheckForUpdates}
									disabled={!updateState.canCheck || checking}
								>
									{checking || updateState.phase === "checking" ? (
										<Loader2 className="size-3.5 animate-spin" />
									) : (
										<RotateCcw className="size-3.5" />
									)}
									强制更新
								</Button>
							)}
						</div>
						{typeof updateState.progressPercent === "number" ? (
							<div className="mt-3 h-1.5 overflow-hidden rounded-full bg-[#E4E1FF]">
								<div
									className="h-full rounded-full bg-[#4F46E5] transition-all"
									style={{
										width: `${Math.max(0, Math.min(updateState.progressPercent, 100))}%`,
									}}
								/>
							</div>
						) : null}
					</div>
				</div>
			</DialogContent>
		</Dialog>
	);
}
