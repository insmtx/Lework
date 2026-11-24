// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Message } from "../types";
import { Conversation } from "./conversation";

const noop = async () => undefined;

function renderConversation(messages: Message[]) {
	render(
		<Conversation
			messages={messages}
			permissions={[]}
			forms={[]}
			onSend={noop}
			onInterrupt={noop}
			onSubmitForm={noop}
			onCancelForm={noop}
			onResolvePermission={noop}
			sending={false}
			interrupting={false}
			loading={false}
			streamConnected
			nodeConnected
			hasOlder={false}
			loadingOlder={false}
			onLoadOlder={noop}
		/>,
	);
}

afterEach(() => {
	cleanup();
	delete (window as Window & { leworkDesktop?: unknown }).leworkDesktop;
});

describe("Conversation links", () => {
	it("点击助手回复中的链接时用外部浏览器打开", () => {
		const openExternal = vi.fn().mockResolvedValue(true);
		(window as Window & { leworkDesktop?: { openExternal: typeof openExternal } }).leworkDesktop = {
			openExternal,
		};

		renderConversation([
			{ id: "m1", role: "assistant", text: "See [docs](https://example.com/docs)." },
		]);

		fireEvent.click(screen.getByRole("link", { name: "docs" }));

		expect(openExternal).toHaveBeenCalledWith("https://example.com/docs");
	});
});
