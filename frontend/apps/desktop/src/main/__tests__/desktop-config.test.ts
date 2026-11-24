import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { readDesktopConfig, writeDesktopConfig } from "../desktop-config";

describe("desktop config", () => {
	it("writes, merges and reads config from the userData directory", () => {
		const dir = mkdtempSync(join(tmpdir(), "lework-desktop-"));

		expect(readDesktopConfig(dir)).toEqual({});
		writeDesktopConfig(dir, { controlPlaneBaseURL: "http://127.0.0.1:8787" });
		writeDesktopConfig(dir, { apiBaseURL: "https://api.example.com/v1" });

		expect(readDesktopConfig(dir)).toEqual({
			controlPlaneBaseURL: "http://127.0.0.1:8787",
			apiBaseURL: "https://api.example.com/v1",
		});
	});

	it("ignores malformed json instead of crashing", () => {
		const dir = mkdtempSync(join(tmpdir(), "lework-desktop-"));
		writeFileSync(join(dir, "desktop-config.json"), "{broken");

		expect(readDesktopConfig(dir)).toEqual({});
	});
});
