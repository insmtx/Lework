// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	hasPrivateServerConfiguration: vi.fn(),
	savePrivateServerBaseURL: vi.fn(),
	testServerConnection: vi.fn(),
}));

vi.mock("@leros/store", () => ({
	API_BASE_URL: "https://default.example.com/v1",
	isPrivateDeployment: true,
	...storeMocks,
}));

import { PrivateDeploymentGate } from "./PrivateDeploymentGate";

describe("PrivateDeploymentGate", () => {
	let container: HTMLDivElement;
	let root: Root;
	let onReload: ReturnType<typeof vi.fn<() => void>>;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
		onReload = vi.fn<() => void>();
		storeMocks.hasPrivateServerConfiguration.mockReset();
		storeMocks.savePrivateServerBaseURL.mockReset();
		storeMocks.testServerConnection.mockReset();
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	it("renders the application only after a server has been configured", () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(true);

		act(() => {
			root.render(
				<PrivateDeploymentGate onReload={onReload}>
					<main data-testid="application">application</main>
				</PrivateDeploymentGate>,
			);
		});

		expect(container.querySelector('[data-testid="application"]')).not.toBeNull();
		expect(document.body.textContent).not.toContain("连接服务");
	});

	it("blocks the application with a dialog that has no close button", () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);

		act(() => {
			root.render(
				<PrivateDeploymentGate onReload={onReload}>
					<main data-testid="application">application</main>
				</PrivateDeploymentGate>,
			);
		});

		expect(container.querySelector('[data-testid="application"]')).toBeNull();
		expect(document.body.textContent).toContain("连接服务");
		expect(document.body.textContent).not.toContain("Lework");
		expect(document.body.textContent).not.toContain("私有化");
		expect(document.querySelector('[data-slot="dialog-close"]')).toBeNull();
	});

	it("saves and reloads after the connection test succeeds", async () => {
		storeMocks.hasPrivateServerConfiguration.mockReturnValue(false);
		storeMocks.testServerConnection.mockResolvedValue("https://private.example.com/v1");

		await act(async () => {
			root.render(
				<PrivateDeploymentGate onReload={onReload}>
					<main>application</main>
				</PrivateDeploymentGate>,
			);
		});

		const input = document.querySelector<HTMLInputElement>("#private-server-url");
		if (!input) throw new Error("setup input was not rendered");
		const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
		setter?.call(input, "https://private.example.com");
		input.dispatchEvent(new Event("input", { bubbles: true }));

		const form = document.querySelector("form");
		if (!form) throw new Error("setup form was not rendered");

		await act(async () => {
			form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
		});

		expect(storeMocks.savePrivateServerBaseURL).toHaveBeenCalledWith(
			"https://private.example.com/v1",
		);
		expect(onReload).toHaveBeenCalledOnce();
	});
});
