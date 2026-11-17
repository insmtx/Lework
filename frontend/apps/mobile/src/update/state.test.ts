import { describe, expect, it } from "vitest";
import { isUpdateVisible, reduceUpdateState, type UpdateState } from "./state";

const idle: UpdateState = { phase: "idle" };

describe("reduceUpdateState", () => {
	it("walks check -> available -> downloading -> downloaded", () => {
		let state = reduceUpdateState(idle, { type: "check-started" });
		expect(state.phase).toBe("checking");

		state = reduceUpdateState(state, {
			type: "update-available",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
		});
		expect(state).toMatchObject({ phase: "available", version: "0.8.0" });

		state = reduceUpdateState(state, { type: "download-started" });
		expect(state).toMatchObject({ phase: "downloading", progress: 0 });

		state = reduceUpdateState(state, { type: "download-progress", progress: 42.6 });
		expect(state).toMatchObject({ phase: "downloading", progress: 43 });

		state = reduceUpdateState(state, { type: "download-finished", fileUri: "file:///a.apk" });
		expect(state).toEqual({
			phase: "downloaded",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
			fileUri: "file:///a.apk",
		});
	});

	it("clamps progress and keeps version on failure", () => {
		let state = reduceUpdateState(idle, {
			type: "update-available",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
		});
		state = reduceUpdateState(state, { type: "download-started" });
		state = reduceUpdateState(state, { type: "download-progress", progress: 150 });
		expect(state).toMatchObject({ phase: "downloading", progress: 100 });

		state = reduceUpdateState(state, { type: "failed", message: "网络中断" });
		expect(state).toMatchObject({
			phase: "error",
			message: "网络中断",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
		});
	});

	it("returns to idle on no-update and dismissed", () => {
		expect(reduceUpdateState({ phase: "checking" }, { type: "no-update" })).toEqual(idle);
		expect(
			reduceUpdateState(
				{ phase: "downloaded", version: "0.8.0", downloadUrl: "u", fileUri: "f" },
				{ type: "dismissed" },
			),
		).toEqual(idle);
	});

	it("cancels an active download via dismissed", () => {
		const downloading: UpdateState = {
			phase: "downloading",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
			progress: 50,
		};
		expect(reduceUpdateState(downloading, { type: "dismissed" })).toEqual(idle);
	});

	it("retries download from error that kept version and downloadUrl", () => {
		const errored: UpdateState = {
			phase: "error",
			message: "网络中断",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
		};
		expect(reduceUpdateState(errored, { type: "download-started" })).toEqual({
			phase: "downloading",
			version: "0.8.0",
			downloadUrl: "https://example.com/app.apk",
			progress: 0,
		});
	});

	it("ignores download-started from error without downloadUrl", () => {
		const errored: UpdateState = { phase: "error", message: "检查失败", version: "0.8.0" };
		expect(reduceUpdateState(errored, { type: "download-started" })).toBe(errored);
	});
});

describe("isUpdateVisible", () => {
	it("shows update states only", () => {
		expect(isUpdateVisible({ phase: "available", version: "0.8.0", downloadUrl: "u" })).toBe(true);
		expect(
			isUpdateVisible({ phase: "downloading", version: "0.8.0", downloadUrl: "u", progress: 1 }),
		).toBe(true);
		expect(isUpdateVisible({ phase: "idle" })).toBe(false);
		expect(isUpdateVisible({ phase: "checking" })).toBe(false);
		expect(isUpdateVisible({ phase: "installing", version: "0.8.0", fileUri: "f" })).toBe(false);
	});
});
