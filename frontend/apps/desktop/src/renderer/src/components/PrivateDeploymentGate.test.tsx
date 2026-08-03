// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	hasPrivateServerConfiguration: vi.fn(),
	savePrivateServerBaseURL: vi.fn(),
	testServerConnection: vi.fn(),
}));

const reloadMocks = vi.hoisted(() => ({
	reloadDesktopRenderer: vi.fn(),
}));

vi.mock("@leros/store", () => ({
	API_BASE_URL: "https://default.example.com/v1",
	isPrivateDeployment: true,
	...storeMocks,
}));

vi.mock("../utils/reload", () => reloadMocks);

import { PrivateDeploymentGate } from "./PrivateDeploymentGate";

describe("PrivateDeploymentGate", () => {
	let container: HTMLDivElement;
	let root: Root;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
		storeMocks.hasPrivateServerConfiguration.mockReset();
		storeMocks.savePrivateServerBaseURL.mockReset();
		storeMocks.testServerConnection.mockReset();
		reloadMocks.reloadDesktopRenderer.mockReset();
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	it("renders the application only after a server has been configured", () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(true);

		act(() => {
			root.render(
				<PrivateDeploymentGate>
					<main data-testid="application">application</main>
				</PrivateDeploymentGate>,
			);
		});

		expect(container.querySelector('[data-testid="application"]')).not.toBeNull();
		expect(document.body.textContent).not.toContain("连接私有化服务");
	});

	it("blocks the application with a dialog that has no close button", () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);

		act(() => {
			root.render(
				<PrivateDeploymentGate>
					<main data-testid="application">application</main>
				</PrivateDeploymentGate>,
			);
		});

		expect(container.querySelector('[data-testid="application"]')).toBeNull();
		expect(document.body.textContent).toContain("连接私有化服务");
		expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
	});

	it("does not prefill the server address input", () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);

		act(() => {
			root.render(
				<PrivateDeploymentGate>
					<main>application</main>
				</PrivateDeploymentGate>,
			);
		});

		const input = document.querySelector<HTMLInputElement>("#private-server-url");
		expect(input?.value).toBe("");
	});

	it("does not save when the connection test fails", async () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);
		storeMocks.testServerConnection.mockRejectedValue(new Error("连接失败"));

		await renderAndSubmit(root, "https://private.example.com");

		expect(storeMocks.savePrivateServerBaseURL).not.toHaveBeenCalled();
		expect(reloadMocks.reloadDesktopRenderer).not.toHaveBeenCalled();
		expect(document.body.textContent).toContain("连接失败");
	});

	it("saves and reloads after the connection test succeeds", async () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);
		storeMocks.testServerConnection.mockResolvedValue("https://private.example.com/v1");

		await renderAndSubmit(root, "https://private.example.com");

		expect(storeMocks.savePrivateServerBaseURL).toHaveBeenCalledWith(
			"https://private.example.com/v1",
		);
		expect(reloadMocks.reloadDesktopRenderer).toHaveBeenCalledOnce();
	});
});

async function renderAndSubmit(root: Root, serverURL: string) {
	await act(async () => {
		root.render(
			<PrivateDeploymentGate>
				<main>application</main>
			</PrivateDeploymentGate>,
		);
	});

	const input = document.querySelector<HTMLInputElement>("#private-server-url");
	if (!input) throw new Error("setup input was not rendered");
	setInputValue(input, serverURL);

	const form = document.querySelector("form");
	if (!form) throw new Error("setup form was not rendered");

	await act(async () => {
		form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
	});
}

function setInputValue(input: HTMLInputElement, value: string) {
	const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
	setter?.call(input, value);
	input.dispatchEvent(new Event("input", { bubbles: true }));
}
