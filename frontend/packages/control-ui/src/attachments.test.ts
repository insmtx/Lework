// @vitest-environment jsdom
import type { ApiClient } from "@lework/api";
import { describe, expect, it, vi } from "vitest";
import { sendWithAttachments } from "./attachments";

const file = new File(["abc"], "a.txt", { type: "text/plain" });

describe("sendWithAttachments", () => {
	it("先上传再用附件提示发送", async () => {
		const calls: string[] = [];
		const client = {
			uploadFile: vi.fn(async () => {
				calls.push("upload");
				return { path: "a.txt", size: 3 };
			}),
			sendPrompt: vi.fn(async (_node: string, _session: string, text: string) => {
				calls.push("prompt");
				expect(text).toBe("总结一下\n\n已上传到项目根目录：a.txt");
			}),
		} as unknown as ApiClient;

		await sendWithAttachments(client, "n1", "s1", "/w", "总结一下", "k1", [file]);

		expect(calls).toEqual(["upload", "prompt"]);
	});

	it("上传失败时不发 prompt", async () => {
		const client = {
			uploadFile: vi.fn(async () => {
				throw new Error("offline");
			}),
			sendPrompt: vi.fn(),
		} as unknown as ApiClient;

		await expect(sendWithAttachments(client, "n1", "s1", "/w", "hi", "k1", [file])).rejects.toThrow(
			"offline",
		);
		expect(client.sendPrompt).not.toHaveBeenCalled();
	});

	it("没有附件时只发正文", async () => {
		const client = {
			uploadFile: vi.fn(),
			sendPrompt: vi.fn(async () => "ok"),
		} as unknown as ApiClient;
		await sendWithAttachments(client, "n1", "s1", "/w", "hi", "k1", []);
		expect(client.uploadFile).not.toHaveBeenCalled();
		expect(client.sendPrompt).toHaveBeenCalledWith("n1", "s1", "hi", "k1");
	});
});
