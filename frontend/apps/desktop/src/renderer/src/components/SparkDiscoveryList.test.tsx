// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { SparkDiscoveryList } from "./SparkDiscoveryList";

it("only selects a discovered address on an explicit click", async () => {
	const scan = vi.fn().mockResolvedValue({
		servers: [{ id: "a", name: "Spark", url: "http://spark.local:38081/v1" }],
	});
	window.lerosDiscovery = { scan };
	const container = document.createElement("div");
	document.body.append(container);
	const root = createRoot(container);
	const select = vi.fn();
	try {
		await act(async () =>
			root.render(<SparkDiscoveryList onSelect={select} disabled={false} autoStart />),
		);
		expect(scan).toHaveBeenCalledOnce();
		expect(select).not.toHaveBeenCalled();
		const candidate = [...container.querySelectorAll("button")].find((b) =>
			b.textContent?.includes("spark.local"),
		);
		expect(candidate).toBeDefined();
		await act(async () => candidate?.click());
		expect(select).toHaveBeenCalledWith("http://spark.local:38081/v1");
	} finally {
		act(() => root.unmount());
		container.remove();
	}
});
