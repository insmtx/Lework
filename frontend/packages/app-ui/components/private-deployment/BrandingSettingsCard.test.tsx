// @vitest-environment jsdom

import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storeMocks = vi.hoisted(() => ({
	getNativeFileInputAccept: vi.fn(() => "image/png"),
	projectFileApi: {
		uploadLoose: vi.fn(),
	},
	readBrandLogo: vi.fn(() => null),
	readCustomBrandName: vi.fn(() => null),
	saveBrandLogo: vi.fn((value: string) => value),
	saveBrandName: vi.fn((value: string) => value.trim() || "Lework"),
}));

vi.mock("@leros/store", () => ({
	...storeMocks,
}));

vi.mock("../../assets", () => ({
	APP_LOGO_SRC: "/logo.svg",
}));

vi.mock("../avatar/ProtectedImage", () => ({
	ProtectedImage: ({ alt }: { alt: string }) => <img alt={alt} />,
}));

vi.mock("sonner", () => ({
	toast: {
		success: vi.fn(),
		error: vi.fn(),
	},
}));

import { BrandingSettingsCard } from "./BrandingSettingsCard";

describe("BrandingSettingsCard", () => {
	let container: HTMLDivElement;
	let root: Root;

	beforeEach(() => {
		container = document.createElement("div");
		document.body.appendChild(container);
		root = createRoot(container);
		storeMocks.projectFileApi.uploadLoose.mockReset();
		storeMocks.saveBrandLogo.mockClear();
		storeMocks.saveBrandName.mockClear();
		storeMocks.readBrandLogo.mockReturnValue(null);
		storeMocks.readCustomBrandName.mockReturnValue(null);
	});

	afterEach(() => {
		act(() => root.unmount());
		container.remove();
	});

	it("saves brand name to local storage helpers", async () => {
		await act(async () => root.render(<BrandingSettingsCard />));

		const input = container.querySelector("#branding-settings-name");
		if (!(input instanceof HTMLInputElement)) throw new Error("brand name input missing");
		const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
		setter?.call(input, "Acme Corp");
		input.dispatchEvent(new Event("input", { bubbles: true }));

		const saveButton = [...container.querySelectorAll("button")].find((item) =>
			item.textContent?.includes("保存品牌名"),
		);
		if (!saveButton) throw new Error("save button missing");

		await act(async () => saveButton.click());
		expect(storeMocks.saveBrandName).toHaveBeenCalledWith("Acme Corp");
	});

	it("uploads logo and persists public id", async () => {
		storeMocks.projectFileApi.uploadLoose.mockResolvedValue({
			data: { public_id: "file_brand_logo" },
		});
		await act(async () => root.render(<BrandingSettingsCard />));

		const fileInput = container.querySelector('input[type="file"]');
		if (!(fileInput instanceof HTMLInputElement)) throw new Error("file input missing");

		const file = new File(["logo"], "logo.png", { type: "image/png" });
		await act(async () => {
			Object.defineProperty(fileInput, "files", {
				configurable: true,
				value: [file],
			});
			fileInput.dispatchEvent(new Event("change", { bubbles: true }));
		});

		expect(storeMocks.projectFileApi.uploadLoose).toHaveBeenCalledWith({
			file,
			purpose: "avatar",
		});
		expect(storeMocks.saveBrandLogo).toHaveBeenCalledWith("file_brand_logo");
	});
});
