import { beforeEach, describe, expect, it, vi } from "vitest";

// downloadApk 在函数体内动态 import("expo-file-system/legacy")（ruling R3），
// vi.mock 的模块注册同样拦截动态 import：这里给取消路径一个可编程的假任务。
vi.mock("expo-file-system/legacy", () => ({
	createDownloadResumable: vi.fn(),
	deleteAsync: vi.fn(),
}));

import { createDownloadResumable, deleteAsync } from "expo-file-system/legacy";

import { clampProgress, downloadApk, updateApkPath } from "./download";

const APK_URL = "https://example.test/0.9.0/android/lework-android-0.9.0.apk";
const DESTINATION = "/cache/lework-update-0.9.0.apk";

describe("download helpers", () => {
	it("builds a versioned cache path", () => {
		expect(updateApkPath("/cache/", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
		expect(updateApkPath("/cache", "0.8.0")).toBe("/cache/lework-update-0.8.0.apk");
	});

	it("clamps progress", () => {
		expect(clampProgress(-5)).toBe(0);
		expect(clampProgress(42.6)).toBe(43);
		expect(clampProgress(150)).toBe(100);
	});
});

describe("downloadApk cancellation", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		// clearAllMocks 会连默认 resolved 值一起清掉：每个用例都要重新可 await。
		vi.mocked(deleteAsync).mockResolvedValue(undefined);
	});

	it("aborting mid-download cancels the task, cleans the partial file and rejects", async () => {
		let startTask!: () => void;
		let settleDownload!: (value: undefined) => void;
		const taskStarted = new Promise<void>((resolve) => {
			startTask = resolve;
		});
		const downloadAsync = vi.fn(
			() =>
				new Promise<undefined>((resolve) => {
					settleDownload = resolve;
				}),
		);
		// 真实 DownloadResumable 在 cancelAsync 后让 downloadAsync 尽快结束；假任务照做。
		const cancelAsync = vi.fn(async () => {
			settleDownload(undefined);
		});
		vi.mocked(createDownloadResumable).mockImplementation(() => {
			startTask();
			return { downloadAsync, cancelAsync } as never;
		});

		const controller = new AbortController();
		const promise = downloadApk({
			url: APK_URL,
			destination: DESTINATION,
			signal: controller.signal,
		});
		await taskStarted;
		// 只看取消路径的清理：此前还有一次「下载前删除旧文件」的调用。
		vi.mocked(deleteAsync).mockClear();

		controller.abort();

		await expect(promise).rejects.toThrow(/下载已取消/);
		expect(cancelAsync).toHaveBeenCalledTimes(1);
		expect(deleteAsync).toHaveBeenCalledWith(DESTINATION, { idempotent: true });
	});

	it("rejects as cancelled without creating a task when the signal is already aborted", async () => {
		vi.mocked(createDownloadResumable).mockImplementation(() => {
			throw new Error("signal 已取消时不应创建下载任务");
		});

		const controller = new AbortController();
		controller.abort();

		await expect(
			downloadApk({ url: APK_URL, destination: DESTINATION, signal: controller.signal }),
		).rejects.toThrow(/下载已取消/);
		expect(createDownloadResumable).not.toHaveBeenCalled();
	});

	it("treats an abort racing a completed download as cancellation, not success", async () => {
		const controller = new AbortController();
		const cancelAsync = vi.fn(async () => {
			// 竞态取消无需假任务做任何事：thenable 已经把结果交付在途。
		});
		// thenable 在 await 移交点先同步 abort、再交付成功结果：把「取消与下载完成同帧竞态」
		// 的时序做成确定的，而非依赖真实的微任务调度。
		const downloadAsync = vi.fn(() => ({
			// biome-ignore lint/suspicious/noThenProperty: 故意实现 thenable 以固定移交时序
			then: (onFulfilled: (value: { uri: string; status: number }) => void) => {
				controller.abort();
				Promise.resolve({ uri: "file:///cache/lework-update-0.9.0.apk", status: 200 }).then(
					onFulfilled,
				);
			},
		}));
		vi.mocked(createDownloadResumable).mockReturnValue({ downloadAsync, cancelAsync } as never);

		await expect(
			downloadApk({ url: APK_URL, destination: DESTINATION, signal: controller.signal }),
		).rejects.toThrow(/下载已取消/);
		// 竞态取消同样清理文件（此处删的是刚下完的 APK：上层已回 idle，残留下次也会重下）。
		expect(cancelAsync).toHaveBeenCalled();
		expect(deleteAsync).toHaveBeenLastCalledWith(DESTINATION, { idempotent: true });
	});
});
