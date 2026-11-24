import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveAppVersion } from "./app-version.mjs";

test("prefers APP_VERSION", () => {
	assert.equal(resolveAppVersion({ env: { APP_VERSION: "1.2.3" }, describe: () => "v9.9.9" }), "1.2.3");
});

test("falls back to the tag without v", () => {
	assert.equal(resolveAppVersion({ env: {}, describe: () => "v0.13.9" }), "0.13.9");
});

test("uses a dev suffix when there is no tag", () => {
	assert.equal(
		resolveAppVersion({ env: {}, describe: () => null, sha: "abcdef1234" }),
		"0.0.0-dev.abcdef1",
	);
});

test("degrades to 0.0.0 without tag, sha or env", () => {
	assert.equal(resolveAppVersion({ env: {}, describe: () => null }), "0.0.0");
});
