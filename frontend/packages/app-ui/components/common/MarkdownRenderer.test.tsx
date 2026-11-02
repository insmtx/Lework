import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CODE_BLOCK_THEME_STORAGE_KEY } from "./codeBlockTheme";
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
		delete (window as Window & { leworkDesktop?: unknown }).leworkDesktop;
	});

	it("opens external links in a new browser context instead of navigating in place", () => {
		const openExternal = vi.fn().mockResolvedValue(true);
		(window as Window & { leworkDesktop?: { openExternal: typeof openExternal } }).leworkDesktop = {
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

describe("MarkdownRenderer code blocks", () => {
	afterEach(() => {
		cleanup();
		window.localStorage.removeItem(CODE_BLOCK_THEME_STORAGE_KEY);
	});
	it("renders fenced code with a theme switch control", () => {
		render(
			<MarkdownRenderer
				content={`\`\`\`ts
const answer = 42;
\`\`\``}
			/>,
		);

		expect(document.querySelector('[data-slot="code-block"]')).toHaveAttribute(
			"data-theme",
			"light",
		);
		expect(screen.getByRole("switch", { name: "切换为暗色代码块" })).toBeInTheDocument();
		expect(screen.getByText("const answer = 42;")).toBeInTheDocument();
	});

	it("shares code theme across separate replies", () => {
		render(
			<>
				<MarkdownRenderer
					content={`\`\`\`ts
const first = 1;
\`\`\``}
				/>
				<MarkdownRenderer
					content={`\`\`\`ts
const second = 2;
\`\`\``}
				/>
			</>,
		);

		const toggles = screen.getAllByRole("switch", { name: "切换为暗色代码块" });
		fireEvent.click(toggles[0]!);

		const blocks = document.querySelectorAll('[data-slot="code-block"]');
		expect(blocks[0]).toHaveAttribute("data-theme", "dark");
		expect(blocks[1]).toHaveAttribute("data-theme", "dark");
	});
});
