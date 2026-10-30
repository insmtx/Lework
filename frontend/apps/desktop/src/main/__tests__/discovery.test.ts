import { describe, expect, it } from "vitest";
import { mergeDiscoveryServices, parseDiscoveryService } from "../discovery-protocol";

const service = {
	name: "Spark",
	host: "spark.local.",
	port: 38081,
	txt: { v: "1", id: "unit-1", scheme: "http", path: "/v1" },
};
describe("discovery protocol", () => {
	it("builds the advertised address without hardcoded IP or port", () => {
		expect(parseDiscoveryService(service)?.url).toBe("http://spark.local:38081/v1");
	});
	it("rejects malformed endpoints and unsupported protocols", () => {
		for (const invalid of [
			{ ...service, port: 0 },
			{ ...service, host: "evil.local/path" },
			{ ...service, txt: { ...service.txt, v: "2" } },
			{ ...service, txt: { ...service.txt, path: "//evil.test" } },
		]) {
			expect(parseDiscoveryService(invalid)).toBeNull();
		}
	});
	it("deduplicates interfaces and flags identity conflicts", () => {
		const first = parseDiscoveryService(service);
		if (!first) throw new Error("fixture not parsed");
		expect(mergeDiscoveryServices([first, first])).toHaveLength(1);
		const other = { ...first, url: "http://other.local:38081/v1" };
		expect(mergeDiscoveryServices([first, other]).every((row) => row.conflict)).toBe(true);
		expect(mergeDiscoveryServices([first, { ...first, id: "unit-2" }])).toHaveLength(2);
	});
});
