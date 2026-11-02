// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SERVER_CONFIG_STORAGE_KEY } from "../api/config";
import * as auth from "./authStorage";

const old = "http://old.local:38081/v1";
const next = "http://new.local:38081/v1";
beforeEach(() => {
	localStorage.clear();
	localStorage.setItem(SERVER_CONFIG_STORAGE_KEY, old);
	localStorage.setItem(auth.AUTH_STORAGE_KEY, "old-token");
});
afterEach(() => vi.restoreAllMocks());
it("preserves auth at the same normalized endpoint", () => {
	auth.saveServerConnection("http://old.local:38081/v1/", old);
	expect(localStorage.getItem(auth.AUTH_STORAGE_KEY)).toBe("old-token");
});
it("clears old credentials before persisting a changed endpoint", () => {
	auth.saveServerConnection(next, old);
	expect(localStorage.getItem(auth.AUTH_STORAGE_KEY)).toBeNull();
	expect(localStorage.getItem(SERVER_CONFIG_STORAGE_KEY)).toBe(next);
});
it("refuses a new endpoint when clearing old credentials fails", () => {
	vi.spyOn(Storage.prototype, "removeItem").mockImplementation(() => {
		throw new Error("storage blocked");
	});
	expect(() => auth.saveServerConnection(next, old)).toThrow();
	expect(localStorage.getItem(SERVER_CONFIG_STORAGE_KEY)).toBe(old);
});
it("failed saving cannot leave new endpoint paired with old auth", () => {
	vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
		throw new Error("quota");
	});
	expect(() => auth.saveServerConnection(next, old)).toThrow();
	expect(localStorage.getItem(SERVER_CONFIG_STORAGE_KEY)).toBe(old);
	expect(localStorage.getItem(auth.AUTH_STORAGE_KEY)).toBeNull();
});
