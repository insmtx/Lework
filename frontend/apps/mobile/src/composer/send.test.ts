import type { ApiClient } from "@lework/api";
import { describe, expect, it, vi } from "vitest";
import { uploadAttachments } from "./attachments";
import { sendToProject } from "./send";

vi.mock("./attachments", () => ({
	uploadAttachments: vi.fn(),
}));

describe("sendToProject", () => {
	it("先上传附件，再建会话并发送", async () => {
		const calls: string[] = [];
		vi.mocked(uploadAttachments).mockImplementation(async () => {
			calls.push("upload");
			return ["a.txt"];
		});
		const client = {
			createSession: vi.fn(async () => {
				calls.push("create");
				return { id: "s1" };
			}),
			sendPrompt: vi.fn(async (_n: string, _s: string, text: string) => {
				calls.push("prompt");
				expect(text).toBe("hi\n\n已上传到项目根目录：a.txt");
			}),
		} as unknown as ApiClient;

		await sendToProject(client, "n1", "/w", "hi", "k1", [{ uri: "file:///a.txt", name: "a.txt" }]);

		expect(calls).toEqual(["upload", "create", "prompt"]);
	});

	it("正文为空时只发附件提示", async () => {
		vi.mocked(uploadAttachments).mockResolvedValue(["a.txt"]);
		const client = {
			createSession: vi.fn(async () => ({ id: "s1" })),
			sendPrompt: vi.fn(async (_n: string, _s: string, text: string) => {
				expect(text).toBe("\n\n已上传到项目根目录：a.txt");
			}),
		} as unknown as ApiClient;

		await sendToProject(client, "n1", "/w", "", "k1", [{ uri: "file:///a.txt", name: "a.txt" }]);

		expect(client.sendPrompt).toHaveBeenCalledTimes(1);
	});
});
