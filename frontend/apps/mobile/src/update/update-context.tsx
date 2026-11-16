// UpdateProvider 是应用内升级的唯一编排者：调度检查、自动下载、安装入口与
// 暂缓提示都汇聚到状态机（state.ts）的 dispatch 上，UI（update-gate）只读状态。
// 更新源固定为 source.ts 的构建期常量，绝不读取 settings.baseUrl。
import * as Application from "expo-application";
import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useReducer,
	useRef,
} from "react";
import { Alert, AppState } from "react-native";

import { type CheckResult, checkForUpdate } from "./check";
import { downloadApk, updateApkPath } from "./download";
import { installApk } from "./install";
import { shouldCheckOnResume, UPDATE_CHECK_INTERVAL_MS, UPDATE_SNOOZE_MS } from "./schedule";
import { MOBILE_APP_ID } from "./source";
import { reduceUpdateState, type UpdateState } from "./state";

// 启动后延迟首查：避开冷启动首屏，与设计文档「约 1 秒」一致。
const INITIAL_CHECK_DELAY_MS = 1_000;

// 进度回调节流到约 200ms：Expo 的回调频率不固定，100% 例外立即上报。
const PROGRESS_THROTTLE_MS = 200;

export interface UpdateContextValue {
	state: UpdateState;
	/** 本机版本；非正式 APK（Expo Go / 开发宿主）读不到时为 null。 */
	currentVersion: string | null;
	manualCheck: () => void;
	install: () => void;
	dismiss: () => void;
	retry: () => void;
}

const UpdateContext = createContext<UpdateContextValue | null>(null);

/**
 * UpdateProvider 持有更新状态机并驱动检查—下载—安装全流程。
 *
 * 只有在包名与版本号都命中正式版 APK 时才启用；所有异常路径都会离开
 * checking / downloading，绝不让状态机卡死。
 */
export function UpdateProvider({ children }: { children: ReactNode }) {
	const [state, dispatch] = useReducer(reduceUpdateState, { phase: "idle" });

	// 两个字段都是进程内常量；合并成一个可空值让「是否启用」与「本机版本」
	// 只有单一事实来源，类型收窄也不必两处判断。
	const currentVersion: string | null =
		Application.applicationId === MOBILE_APP_ID && Application.nativeApplicationVersion
			? Application.nativeApplicationVersion
			: null;

	const stateRef = useRef(state);
	stateRef.current = state;
	const mountedRef = useRef(true);
	const lastCheckedAtRef = useRef<number | null>(null);
	const checkInFlightRef = useRef(false);
	const downloadRef = useRef<AbortController | null>(null);
	const snoozeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const progressAtRef = useRef(0);

	const cancelActiveDownload = useCallback((): void => {
		const controller = downloadRef.current;
		if (!controller) return;
		downloadRef.current = null;
		// downloadApk 收到 abort 后会 cancelAsync + 清理半成品并 reject；
		// 其 rejection 在下方按「已取消」静默忽略。
		controller.abort();
	}, []);

	// 发起一次下载。调用方保证状态处于 available，或带完整版本信息的 error
	// （原地重试）；并发保护只靠 downloadRef 的单一任务约定。
	const startDownload = useCallback((version: string, apkUrl: string): void => {
		if (downloadRef.current) return;
		const controller = new AbortController();
		downloadRef.current = controller;
		progressAtRef.current = 0;
		dispatch({ type: "download-started" });

		void (async () => {
			try {
				const { cacheDirectory } = await import("expo-file-system/legacy");
				// 缓存目录理论上只在原生宿主缺失时为 null：按失败收场，状态机离开 downloading。
				if (!cacheDirectory) throw new Error("无法访问缓存目录，不能下载更新文件");
				const fileUri = await downloadApk({
					url: apkUrl,
					destination: updateApkPath(cacheDirectory, version),
					signal: controller.signal,
					onProgress: (percent) => {
						const now = Date.now();
						if (percent < 100 && now - progressAtRef.current < PROGRESS_THROTTLE_MS) return;
						progressAtRef.current = now;
						if (mountedRef.current && !controller.signal.aborted) {
							dispatch({ type: "download-progress", progress: percent });
						}
					},
				});
				if (controller.signal.aborted || !mountedRef.current) return;
				dispatch({ type: "download-finished", fileUri });
			} catch (error) {
				// 取消不是失败：dismiss 已把状态带回 idle，这里静默收尾。
				if (controller.signal.aborted || !mountedRef.current) return;
				dispatch({
					type: "failed",
					message: error instanceof Error ? error.message : String(error),
				});
			} finally {
				if (downloadRef.current === controller) downloadRef.current = null;
			}
		})();
	}, []);

	const runCheck = useCallback(
		(manual: boolean): void => {
			if (currentVersion === null) {
				// Expo Go / 开发宿主 / iOS：更新能力不存在，手动入口给出解释而不是假报错。
				if (manual) {
					Alert.alert("检查更新", "当前不是正式版 Android APK，不支持应用内更新。");
				}
				return;
			}
			if (checkInFlightRef.current) return;
			const phase = stateRef.current.phase;
			// 自动检查只在 idle 时进行：error 停留等用户处理（设计文档「不静默丢弃」），
			// available / downloading / installing 中途不能被新检查打断。手动检查额外
			// 允许从 error 重新发起。
			if (phase !== "idle" && !(manual && phase === "error")) return;

			checkInFlightRef.current = true;
			lastCheckedAtRef.current = Date.now();
			dispatch({ type: "check-started" });

			void (async () => {
				let result: CheckResult;
				try {
					result = await checkForUpdate({ currentVersion });
					if (!mountedRef.current) return;
					if (result.kind === "update") {
						dispatch({ type: "update-available", version: result.version, apkUrl: result.apkUrl });
						// 发现新版本立即开始后台下载（与桌面端一致），弹窗同步显示进度。
						startDownload(result.version, result.apkUrl);
						return;
					}
					if (result.kind === "none") {
						dispatch({ type: "no-update" });
						return;
					}
					if (manual) {
						// 手动检查失败要说清楚：Alert 给可读消息，状态留在 error 供重试。
						dispatch({ type: "failed", message: result.message });
						Alert.alert("检查更新失败", result.message);
					} else {
						// 自动检查失败静默：回到 idle，等下一个检查时机。
						dispatch({ type: "no-update" });
					}
					return;
				} finally {
					checkInFlightRef.current = false;
				}
			})();
		},
		[currentVersion, startDownload],
	);

	const manualCheck = useCallback((): void => runCheck(true), [runCheck]);

	const retry = useCallback((): void => {
		const current = stateRef.current;
		// 下载失败仍持有版本信息时原地重新下载，省一次版本检查；
		// 其余失败（如检查、安装失败）退化为一次手动检查。
		if (current.phase === "error" && current.version && current.apkUrl) {
			startDownload(current.version, current.apkUrl);
			return;
		}
		runCheck(true);
	}, [runCheck, startDownload]);

	const dismiss = useCallback((): void => {
		cancelActiveDownload();
		dispatch({ type: "dismissed" });
		if (snoozeTimerRef.current) clearTimeout(snoozeTimerRef.current);
		// 「稍后」= 暂缓 5 分钟后重新检查并再次提示；重启 App 会立即再查。
		snoozeTimerRef.current = setTimeout(() => {
			snoozeTimerRef.current = null;
			runCheck(false);
		}, UPDATE_SNOOZE_MS);
	}, [cancelActiveDownload, runCheck]);

	const install = useCallback((): void => {
		const current = stateRef.current;
		if (current.phase !== "downloaded") return;
		const fileUri = current.fileUri;
		// 安装器接管前台后本进程通常被系统结束，成功路径不再改状态。
		dispatch({ type: "install-started" });
		void (async () => {
			try {
				await installApk(fileUri);
			} catch (error) {
				if (!mountedRef.current) return;
				const reason = error instanceof Error ? error.message : String(error);
				dispatch({
					type: "failed",
					message: `打开系统安装器失败：${reason}。首次升级请在系统设置里允许 Lework 安装未知应用（一次性设置），完成后回到本页重试。`,
				});
			}
		})();
	}, []);

	useEffect(() => {
		// StrictMode 双挂载会先跑一次 cleanup：重新挂载时必须恢复 mounted 标记，
		// 否则所有 dispatch 会被「已卸载」守卫吞掉。
		mountedRef.current = true;
		return () => {
			mountedRef.current = false;
			cancelActiveDownload();
			if (snoozeTimerRef.current) {
				clearTimeout(snoozeTimerRef.current);
				snoozeTimerRef.current = null;
			}
		};
	}, [cancelActiveDownload]);

	// 检查调度：启动 1 秒后首查、前台每 30 分钟、回前台按 5 分钟间隔补查。
	// 更新能力禁用（currentVersion === null）时完全不挂定时器与监听。
	useEffect(() => {
		if (currentVersion === null) return;
		const initialTimer = setTimeout(() => runCheck(false), INITIAL_CHECK_DELAY_MS);
		const interval = setInterval(() => runCheck(false), UPDATE_CHECK_INTERVAL_MS);
		const subscription = AppState.addEventListener("change", (next) => {
			if (next !== "active") return;
			if (shouldCheckOnResume(lastCheckedAtRef.current, Date.now())) runCheck(false);
		});
		return () => {
			clearTimeout(initialTimer);
			clearInterval(interval);
			subscription.remove();
		};
	}, [currentVersion, runCheck]);

	const value = useMemo<UpdateContextValue>(
		() => ({ state, currentVersion, manualCheck, install, dismiss, retry }),
		[state, currentVersion, manualCheck, install, dismiss, retry],
	);

	return <UpdateContext.Provider value={value}>{children}</UpdateContext.Provider>;
}

/** useUpdate 读取更新状态与操作入口；在 UpdateProvider 之外使用属于接线错误。 */
export function useUpdate(): UpdateContextValue {
	const value = useContext(UpdateContext);
	if (!value) throw new Error("useUpdate must be used inside UpdateProvider");
	return value;
}
