import { zipSync } from "fflate";
import { beforeEach, describe, expect, it, vi } from "vitest";

// expo-file-system 是原生模块，在 node 测试环境不存在；用内存文件系统替身拦截
// 动态 import（ruling R3：extract.ts 只在函数体内 import 原生模块）。
const files = vi.hoisted(() => new Map<string, Uint8Array>());

vi.mock("expo-file-system", () => ({
	File: class {
		private readonly path: string;

		constructor(uri: string) {
			this.path = uri;
		}

		get uri(): string {
			return this.path;
		}

		get exists(): boolean {
			return files.has(this.path);
		}

		async bytes(): Promise<Uint8Array> {
			const content = files.get(this.path);
			if (!content) throw new Error(`文件不存在：${this.path}`);
			return content;
		}

		async write(content: Uint8Array): Promise<void> {
			files.set(this.path, content);
		}

		delete(): void {
			files.delete(this.path);
		}
	},
}));

import { extractApkFromZip, extractedApkPath, pickApkEntry } from "./extract";

const ZIP_URI = "file:///cache/lework-update-0.9.0.zip";
const APK_URI = "file:///cache/lework-update-0.9.0.apk";

function putZip(entries: Record<string, Uint8Array>): void {
	files.set(ZIP_URI, zipSync(entries));
}

beforeEach(() => {
	files.clear();
});

describe("pickApkEntry", () => {
	it("picks the only apk entry and ignores non-apk files", () => {
		expect(pickApkEntry(["lework-android-0.9.0.apk", "readme.txt"])).toBe(
			"lework-android-0.9.0.apk",
		);
	});

	it("picks a nested apk but skips directory entries", () => {
		expect(pickApkEntry(["android/", "android/lework-android-0.9.0.APK"])).toBe(
			"android/lework-android-0.9.0.APK",
		);
	});

	it("returns null when there is no apk or more than one", () => {
		expect(pickApkEntry(["readme.txt"])).toBeNull();
		expect(pickApkEntry(["a.apk", "b.apk"])).toBeNull();
	});
});

describe("extractApkFromZip", () => {
	it("builds the apk path next to the downloaded package", () => {
		expect(extractedApkPath("/cache/", "0.9.0")).toBe("/cache/lework-update-0.9.0.apk");
		expect(extractedApkPath("/cache", "0.9.0")).toBe("/cache/lework-update-0.9.0.apk");
	});

	it("writes the apk bytes to the destination and returns its uri", async () => {
		const apkBytes = new TextEncoder().encode("fake-apk-payload");
		putZip({ "lework-android-0.9.0.apk": apkBytes, "readme.txt": new Uint8Array([1]) });

		await expect(extractApkFromZip({ zipUri: ZIP_URI, apkUri: APK_URI })).resolves.toBe(APK_URI);
		expect(files.get(APK_URI)).toEqual(apkBytes);
	});

	it("removes the downloaded archive after a successful extraction", async () => {
		putZip({ "lework-android-0.9.0.apk": new Uint8Array([7]) });

		await extractApkFromZip({ zipUri: ZIP_URI, apkUri: APK_URI });
		expect(files.has(ZIP_URI)).toBe(false);
	});

	it("overwrites a stale apk left by a previous attempt", async () => {
		files.set(APK_URI, new Uint8Array([9, 9, 9]));
		const apkBytes = new TextEncoder().encode("new-payload");
		putZip({ "lework-android-0.9.0.apk": apkBytes });

		await extractApkFromZip({ zipUri: ZIP_URI, apkUri: APK_URI });
		expect(files.get(APK_URI)).toEqual(apkBytes);
	});

	it("rejects with a readable error when the archive has no apk", async () => {
		putZip({ "readme.txt": new Uint8Array([1]) });

		await expect(extractApkFromZip({ zipUri: ZIP_URI, apkUri: APK_URI })).rejects.toThrow(/APK/);
		expect(files.has(APK_URI)).toBe(false);
	});
});
