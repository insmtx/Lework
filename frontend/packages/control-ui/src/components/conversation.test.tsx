// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Message } from "../types";
import { Conversation } from "./conversation";

const noop = async () => undefined;

function conversationNode(
	sessionID: string,
	messages: Message[] = [],
	onSend: (text: string, files: File[]) => Promise<void> = noop,
) {
	return (
		<Conversation
			sessionID={sessionID}
			messages={messages}
			permissions={[]}
			forms={[]}
			onSend={onSend}
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
		/>
	);
}

function renderConversation(messages: Message[]) {
	render(conversationNode("s1", messages));
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

describe("Conversation attachments", () => {
	it("只有附件也能发送，并随 onSend 传出文件", async () => {
		const onSend = vi.fn(async () => undefined);
		render(conversationNode("s1", [], onSend));
		const file = new File(["abc"], "a.txt");
		fireEvent.change(screen.getByTestId("attachment-input"), { target: { files: [file] } });
		fireEvent.click(screen.getByLabelText("Send prompt"));
		await Promise.resolve();
		expect(onSend).toHaveBeenCalledWith("", [file]);
	});

	it("切换会话清空已选附件", () => {
		const view = render(conversationNode("s1"));
		const file = new File(["abc"], "a.txt");
		fireEvent.change(screen.getByTestId("attachment-input"), { target: { files: [file] } });
		expect(screen.getByText("a.txt")).toBeTruthy();
		view.rerender(conversationNode("s2"));
		expect(screen.queryByText("a.txt")).toBeNull();
	});
});
