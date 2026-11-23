import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { APPROVAL_SOUND, COMPLETION_SOUND, PUSH_SOUND_FILES } from "./sounds";

describe("推送提示音资源", () => {
	it("每个提示音都随包打到 assets/sounds 下", () => {
		for (const name of PUSH_SOUND_FILES) {
			const path = fileURLToPath(new URL(`../../assets/sounds/${name}`, import.meta.url));
			expect(existsSync(path), `${name} 缺失`).toBe(true);
		}
	});

	// 渠道里的声音名必须是 expo-notifications 插件登记过的文件：漏登记时
	// Android 会静默回落到系统默认音，用户就听不出两类提醒的区别。
	it("每个提示音都登记在 expo-notifications 插件里", () => {
		const config = JSON.parse(
			readFileSync(fileURLToPath(new URL("../../app.json", import.meta.url)), "utf8"),
		) as { expo: { plugins: unknown[] } };
		const plugin = config.expo.plugins.find(
			(entry): entry is [string, { sounds?: string[] }] =>
				Array.isArray(entry) && entry[0] === "expo-notifications",
		);
		expect(plugin, "app.json 缺少 expo-notifications 插件配置").toBeDefined();

		const registered = plugin?.[1].sounds ?? [];
		for (const name of [APPROVAL_SOUND, COMPLETION_SOUND]) {
			expect(
				registered.some((entry) => entry.endsWith(`/${name}`)),
				`${name} 未登记在 sounds 数组`,
			).toBe(true);
		}
	});
});
