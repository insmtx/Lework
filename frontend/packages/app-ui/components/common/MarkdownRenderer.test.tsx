import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { MarkdownRenderer } from "./MarkdownRenderer";

vi.mock("./PlanBlock", () => ({
	PlanBlock: ({ fileId, children }: { fileId: string; children: React.ReactNode }) => (
		<div data-testid="plan-block" data-file-id={fileId}>
			{children}
		</div>
	),
}));

describe("MarkdownRenderer plan directive", () => {
	it("renders a published plan directive as a plan block", () => {
		render(
			<MarkdownRenderer
				content={':::plan{"file_id":"file_plan_1","summary_lines":1,"total_lines":2}\nInspect\n:::'}
			/>,
		);

		expect(screen.getByTestId("plan-block")).toHaveAttribute("data-file-id", "file_plan_1");
		expect(screen.getByTestId("plan-block")).toHaveTextContent("Inspect");
	});
});

describe("MarkdownRenderer external links", () => {
	afterEach(() => {
		delete (window as Window & { lerosDesktop?: unknown }).lerosDesktop;
	});

	it("opens external links in a new browser context instead of navigating in place", () => {
		const openExternal = vi.fn().mockResolvedValue(true);
		(window as Window & { lerosDesktop?: { openExternal: typeof openExternal } }).lerosDesktop = {
			openExternal,
		};

		render(<MarkdownRenderer content="See [docs](https://example.com/docs) for details." />);

		const link = screen.getByRole("link", { name: "docs" });
		expect(link).toHaveAttribute("href", "https://example.com/docs");
		expect(link).toHaveAttribute("target", "_blank");
		expect(link).toHaveAttribute("rel", "noopener noreferrer");

		fireEvent.click(link);
		expect(openExternal).toHaveBeenCalledWith("https://example.com/docs");
	});
});
