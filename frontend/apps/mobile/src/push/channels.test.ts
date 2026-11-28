import { describe, expect, it } from "vitest";

import { androidChannels, LEGACY_CHANNEL_IDS } from "./channels";
import { APPROVAL_SOUND, COMPLETION_SOUND } from "./sounds";

describe("androidChannels", () => {
	it("审批与执行结果各用一个渠道", () => {
		const channels = androidChannels();
		expect(channels.map((channel) => channel.id)).toEqual(["permissions-v3", "completions-v4"]);
	});

	// Android 会「复活」同 ID 的已删除渠道并保留旧设置（声音、震动都属于旧设置），
	// 所以改渠道配置只能换新 ID；旧 ID 必须在启动时删掉。
	it("不复用会保留旧设置的渠道 ID", () => {
		const ids = androidChannels().map((channel) => channel.id);
		for (const id of ids) {
			expect(LEGACY_CHANNEL_IDS).not.toContain(id);
		}
	});

	it("两类提醒使用不同提示音", () => {
		const [approval, completion] = androidChannels();
		expect(approval.sound).toBe(APPROVAL_SOUND);
		expect(completion.sound).toBe(COMPLETION_SOUND);
		expect(approval.sound).not.toBe(completion.sound);
	});

	it("两类提醒都震动，但节奏不同", () => {
		const [approval, completion] = androidChannels();
		expect(approval.vibrationPattern).toEqual([0, 250, 150, 250]);
		expect(completion.vibrationPattern).toEqual([0, 150]);
	});

	it("两类提醒都要能打断屏幕", () => {
		const [approval, completion] = androidChannels();
		expect(approval.importance).toBe("high");
		expect(approval.enableVibrate).toBe(true);
		// DEFAULT 只进通知栏；前台盯着屏幕时看不到，等于没有完成提醒。
		expect(completion.importance).toBe("high");
		expect(completion.enableVibrate).toBe(true);
	});
});
