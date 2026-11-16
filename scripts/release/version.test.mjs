// scripts/release/version.test.mjs
import assert from "node:assert/strict";
import test from "node:test";
import { compareVersions, isStableVersion } from "./version.mjs";

test("compares stable versions", () => {
  assert.equal(compareVersions("0.8.0", "0.7.9"), 1);
  assert.equal(compareVersions("0.8.0", "0.8.0"), 0);
  assert.equal(compareVersions("0.7.9", "0.8.0"), -1);
});

test("prerelease sorts below its stable release", () => {
  assert.equal(compareVersions("0.8.0", "0.8.0-rc.1"), 1);
  assert.equal(compareVersions("0.8.0-rc.2", "0.8.0-rc.1"), 1);
});

test("rejects invalid versions", () => {
  assert.throws(() => compareVersions("latest", "0.8.0"));
});

test("detects stable versions", () => {
  assert.equal(isStableVersion("0.8.0"), true);
  assert.equal(isStableVersion("0.8.0-rc.1"), false);
  assert.equal(isStableVersion("v0.8.0"), false);
});
