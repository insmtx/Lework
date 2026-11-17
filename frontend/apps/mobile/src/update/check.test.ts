import { describe, expect, it } from "vitest";
import { checkForUpdate } from "./check";

function response(status: number, body = ""): Response {
	return new Response(body, { status });
}

describe("checkForUpdate", () => {
	it("returns update when remote is newer and package exists", async () => {
		const calls: string[] = [];
		const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
			calls.push(`${init?.method ?? "GET"} ${String(input)}`);
			if (init?.method === "HEAD") return response(200);
			return response(200, "0.8.0\n");
		}) as typeof fetch;

		await expect(
			checkForUpdate({ currentVersion: "0.7.0", fetchImpl, now: () => 123 }),
		).resolves.toEqual({
			kind: "update",
			version: "0.8.0",
			downloadUrl:
				"https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/0.8.0/android/lework-android-0.8.0.zip",
		});
		expect(calls).toEqual([
			"GET https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/LATEST.txt?t=123",
			"HEAD https://leros-1395325824.cos.ap-beijing.myqcloud.com/application/0.8.0/android/lework-android-0.8.0.zip",
		]);
	});

	it("returns none for same version, missing package, or invalid body", async () => {
		const same = (async () => response(200, "0.8.0\n")) as typeof fetch;
		await expect(checkForUpdate({ currentVersion: "0.8.0", fetchImpl: same })).resolves.toEqual({
			kind: "none",
		});

		const missing = (async (_input: RequestInfo | URL, init?: RequestInit) =>
			init?.method === "HEAD" ? response(404) : response(200, "0.8.0\n")) as typeof fetch;
		await expect(checkForUpdate({ currentVersion: "0.7.0", fetchImpl: missing })).resolves.toEqual({
			kind: "none",
		});

		const html = (async () => response(200, "<html>oops</html>")) as typeof fetch;
		await expect(checkForUpdate({ currentVersion: "0.7.0", fetchImpl: html })).resolves.toEqual({
			kind: "error",
			message: expect.any(String),
		});
	});

	it("returns error on http failure or network rejection", async () => {
		const http500 = (async () => response(500)) as typeof fetch;
		await expect(checkForUpdate({ currentVersion: "0.7.0", fetchImpl: http500 })).resolves.toEqual({
			kind: "error",
			message: expect.stringContaining("500"),
		});

		const offline = (async () => {
			throw new Error("offline");
		}) as typeof fetch;
		await expect(checkForUpdate({ currentVersion: "0.7.0", fetchImpl: offline })).resolves.toEqual({
			kind: "error",
			message: expect.stringContaining("offline"),
		});
	});
});
