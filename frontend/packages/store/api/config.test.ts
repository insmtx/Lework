// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chooseAPIBaseURL } from "./config";

const STORAGE_KEY = "lework-server-base-url";

beforeEach(() => {
	window.localStorage.clear();
});

afterEach(() => {
	vi.restoreAllMocks();
	delete (window as { leworkDesktop?: unknown }).leworkDesktop;
});

describe("chooseAPIBaseURL", () => {
	it("prefers a relative build-time base URL over a stored absolute address", () => {
		expect(
			chooseAPIBaseURL({
				isServer: false,
				buildURL: "/v1",
				storedURL: "https://private.example.com/v1",
			}),
		).toBe("/v1");
	});

	it("keeps the stored absolute address when the build URL is absolute", () => {
		expect(
			chooseAPIBaseURL({
				isServer: false,
				buildURL: "https://build.example.com/v1",
				storedURL: "https://private.example.com/v1",
			}),
		).toBe("https://private.example.com/v1");
	});

	it("falls back to the build URL when nothing is stored", () => {
		expect(
			chooseAPIBaseURL({
				isServer: false,
				buildURL: "https://build.example.com/v1",
				storedURL: null,
			}),
		).toBe("https://build.example.com/v1");
	});
});

describe("desktop bridge", () => {
	it("hands the normalized address to the desktop bridge on save", async () => {
		const setApiBaseURL = vi.fn().mockResolvedValue(undefined);
		Object.defineProperty(window, "leworkDesktop", {
			value: { setApiBaseURL, testServerConnection: vi.fn() },
			configurable: true,
		});
		const { saveServerBaseURL } = await import("./config");

		const saved = saveServerBaseURL("https://private.example.com");

		expect(saved).toBe("https://private.example.com/v1");
		expect(window.localStorage.getItem(STORAGE_KEY)).toBe("https://private.example.com/v1");
		expect(setApiBaseURL).toHaveBeenCalledWith("https://private.example.com/v1");
	});
});
