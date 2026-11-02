import { afterEach, expect, it, vi } from "vitest";
import { inspectSparkDevice } from "./sparkDevice";

const device = {
	type: "spark",
	protocol_version: 1,
	id: "spark-001",
	name: "工作站",
	management: true,
};
function respond(data: unknown) {
	const fetcher = vi
		.fn()
		.mockResolvedValue(new Response(JSON.stringify({ code: 0, data }), { status: 200 }));
	vi.stubGlobal("fetch", fetcher);
	return fetcher;
}
afterEach(() => {
	vi.unstubAllGlobals();
	vi.useRealTimers();
});
it("probes anonymously without redirects and returns only the device whitelist", async () => {
	const fetcher = respond({ edition: "oss", device: { ...device, token: "secret" } });
	expect(await inspectSparkDevice("http://spark.local:8080")).toEqual(device);
	expect(fetcher).toHaveBeenCalledWith(
		"http://spark.local:8080/v1/GlobalConfig",
		expect.objectContaining({
			credentials: "omit",
			redirect: "error",
			headers: { Accept: "application/json" },
		}),
	);
});
it("distinguishes a normal backend from invalid or unsupported declarations", async () => {
	respond({ edition: "oss" });
	expect(await inspectSparkDevice("http://spark.local")).toBeNull();
	for (const malformed of [
		{ ...device, id: "" },
		{ ...device, id: "bad/id" },
		{ ...device, protocol_version: 2 },
		{ ...device, name: "bad\nname" },
		{ ...device, management: "true" },
		null,
	]) {
		respond({ edition: "oss", device: malformed });
		await expect(inspectSparkDevice("http://spark.local")).rejects.toThrow();
	}
	respond({ unrelated: true });
	await expect(inspectSparkDevice("http://spark.local")).rejects.toThrow();
});
it("rejects credential-bearing addresses without issuing a request", async () => {
	const fetcher = respond({ edition: "oss", device });
	await expect(inspectSparkDevice("http://user:password@spark.local")).rejects.toThrow();
	expect(fetcher).not.toHaveBeenCalled();
});
it("bounds a stalled probe and respects caller cancellation", async () => {
	vi.useFakeTimers();
	vi.stubGlobal(
		"fetch",
		vi.fn(
			(_url, options: RequestInit) =>
				new Promise((_resolve, reject) => {
					options.signal?.addEventListener("abort", () =>
						reject(new DOMException("Aborted", "AbortError")),
					);
				}),
		),
	);
	const request = inspectSparkDevice("http://spark.local");
	const rejection = expect(request).rejects.toThrow();
	await vi.advanceTimersByTimeAsync(8001);
	await rejection;
	const controller = new AbortController();
	const cancelled = inspectSparkDevice("http://spark.local", controller.signal);
	const cancelledRejection = expect(cancelled).rejects.toThrow();
	controller.abort();
	await cancelledRejection;
});
it("does not expose arbitrary remote error text", async () => {
	vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("token=private-value")));
	await expect(inspectSparkDevice("http://spark.local")).rejects.not.toThrow("private-value");
});

it("rejects oversized streamed responses even without Content-Length", async () => {
	respond({ edition: "oss", device, extra: "x".repeat(65536) });
	await expect(inspectSparkDevice("http://spark.local")).rejects.toThrow();
});
it("cancels stalled response bodies at the probe deadline", async () => {
	vi.useFakeTimers();
	const cancel = vi.fn();
	vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(new ReadableStream({ cancel }))));
	const probe = inspectSparkDevice("http://spark.local");
	const rejection = expect(probe).rejects.toThrow();
	await vi.advanceTimersByTimeAsync(8001);
	await rejection;
	expect(cancel).toHaveBeenCalledOnce();
}, 1000);
