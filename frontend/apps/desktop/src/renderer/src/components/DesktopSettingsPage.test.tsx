// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@leros/store", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@leros/store")>();
	return {
		...actual,
		isPrivateDeployment: false,
	};
});

import { DesktopSettingsPage } from "./DesktopSettingsPage";

describe("DesktopSettingsPage", () => {
	let container: HTMLDivElement;
	let root: Root;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
		vi.resetModules();
	});

	it("redirects non-private desktop builds away from settings", async () => {
		await act(async () => {
			root.render(
				<MemoryRouter initialEntries={["/settings"]}>
					<DesktopSettingsPage />
				</MemoryRouter>,
			);
		});

		expect(document.body.textContent).not.toContain("管理私有化后端服务连接");
	});
});
