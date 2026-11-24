import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { DesktopConfig } from "../shared/desktop-config";

const CONFIG_FILE = "desktop-config.json";

export function readDesktopConfig(dir: string): DesktopConfig {
	let raw: string;
	try {
		raw = readFileSync(join(dir, CONFIG_FILE), "utf8");
	} catch {
		return {};
	}

	let parsed: unknown;
	try {
		parsed = JSON.parse(raw);
	} catch {
		return {};
	}
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
		return {};
	}

	const record = parsed as Record<string, unknown>;
	const config: DesktopConfig = {};
	if (typeof record.apiBaseURL === "string") config.apiBaseURL = record.apiBaseURL;
	if (typeof record.controlPlaneBaseURL === "string") {
		config.controlPlaneBaseURL = record.controlPlaneBaseURL;
	}
	if (typeof record.migratedFromFileOrigin === "boolean") {
		config.migratedFromFileOrigin = record.migratedFromFileOrigin;
	}
	return config;
}

export function writeDesktopConfig(dir: string, patch: Partial<DesktopConfig>): DesktopConfig {
	const next = { ...readDesktopConfig(dir), ...patch };
	mkdirSync(dir, { recursive: true });
	writeFileSync(join(dir, CONFIG_FILE), `${JSON.stringify(next, null, 2)}\n`, "utf8");
	return next;
}
