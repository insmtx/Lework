import type { ApiClient } from "@lework/api";
import { describe, expect, it, vi } from "vitest";
import { uploadAttachments } from "./attachments";

class FakeFormData {
	entries: Array<[string, unknown]> = [];
	append(key: string, value: unknown) {
		this.entries.push([key, value]);
	}
}

describe("uploadAttachments", () => {
	it("逐个上传并返回最终路径", async () => {
		vi.stubGlobal("FormData", FakeFormData);
		const uploadFile = vi
			.fn()
			.mockResolvedValueOnce({ path: "a (1).pdf", size: 1 })
			.mockResolvedValueOnce({ path: "b.png", size: 2 });
		const client = { uploadFile } as unknown as ApiClient;

		const paths = await uploadAttachments(client, "n1", "/w", [
			{ uri: "file:///a.pdf", name: "a.pdf", mimeType: "application/pdf" },
			{ uri: "file:///b.png", name: "b.png", mimeType: "image/png" },
		]);

		expect(paths).toEqual(["a (1).pdf", "b.png"]);
		expect(uploadFile).toHaveBeenNthCalledWith(1, "n1", "/w", expect.any(FakeFormData));
		vi.unstubAllGlobals();
	});
});
