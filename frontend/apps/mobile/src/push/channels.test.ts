import { describe, expect, it } from "vitest";

import { androidChannels, LEGACY_CHANNEL_IDS } from "./channels";
import { APPROVAL_SOUND, COMPLETION_SOUND } from "./sounds";

describe("androidChannels", () => {
	it("审批与执行结果各用一个渠道", () => {
		const channels = androidChannels();
		expect(channels.map((channel) => channel.id)).toEqual(["permissions-v2", "completions-v2"]);
	});

	// Android 会「复活」同 ID 的已删除渠道并保留旧设置，因此改声音只能换
	// 新的渠道 ID；旧 ID 必须在启动时删掉，否则系统设置里会留下重复条目。
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

	it("审批打断用户，完成只给一条横幅", () => {
		const [approval, completion] = androidChannels();
		expect(approval.importance).toBe("high");
		expect(approval.enableVibrate).toBe(true);
		expect(completion.importance).toBe("default");
		expect(completion.enableVibrate).toBe(false);
	});
});
