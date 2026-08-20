import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { CodeBlock } from "./CodeBlock";
import { CODE_BLOCK_THEME_STORAGE_KEY } from "./codeBlockTheme";

describe("CodeBlock theme switch", () => {
	afterEach(() => {
		cleanup();
		window.localStorage.removeItem(CODE_BLOCK_THEME_STORAGE_KEY);
	});

	it("defaults to light and switches to dark via the sun/moon control", async () => {
		render(
			<CodeBlock>
				<code>const x = 1;</code>
			</CodeBlock>,
		);

		expect(document.querySelector('[data-slot="code-block"]')).toHaveAttribute(
			"data-theme",
			"light",
		);

		fireEvent.click(screen.getByRole("switch", { name: "切换为暗色代码块" }));

		await waitFor(() => {
			expect(document.querySelector('[data-slot="code-block"]')).toHaveAttribute(
				"data-theme",
				"dark",
			);
		});
		expect(window.localStorage.getItem(CODE_BLOCK_THEME_STORAGE_KEY)).toBe("dark");
		expect(screen.getByRole("switch", { name: "切换为亮色代码块" })).toBeInTheDocument();
	});

	it("restores the persisted theme and syncs across instances", async () => {
		window.localStorage.setItem(CODE_BLOCK_THEME_STORAGE_KEY, "dark");

		render(
			<>
				<CodeBlock>
					<code>one</code>
				</CodeBlock>
				<CodeBlock>
					<code>two</code>
				</CodeBlock>
			</>,
		);

		await waitFor(() => {
			const blocks = document.querySelectorAll('[data-slot="code-block"]');
			expect(blocks).toHaveLength(2);
			expect(blocks[0]).toHaveAttribute("data-theme", "dark");
			expect(blocks[1]).toHaveAttribute("data-theme", "dark");
		});

		const toggles = screen.getAllByRole("switch", { name: "切换为亮色代码块" });
		fireEvent.click(toggles[0]!);

		await waitFor(() => {
			const blocks = document.querySelectorAll('[data-slot="code-block"]');
			expect(blocks[0]).toHaveAttribute("data-theme", "light");
			expect(blocks[1]).toHaveAttribute("data-theme", "light");
		});
		expect(window.localStorage.getItem(CODE_BLOCK_THEME_STORAGE_KEY)).toBe("light");
	});

	it("applies containerClassName to the outer wrapper", () => {
		render(
			<CodeBlock containerClassName="my-0 flex-1">
				<code>html</code>
			</CodeBlock>,
		);

		expect(document.querySelector('[data-slot="code-block"]')).toHaveClass("my-0", "flex-1");
	});

	it("uses a higher-contrast scrollbar in dark theme", async () => {
		window.localStorage.setItem(CODE_BLOCK_THEME_STORAGE_KEY, "dark");

		render(
			<CodeBlock>
				<code>html</code>
			</CodeBlock>,
		);

		await waitFor(() => {
			const block = document.querySelector('[data-slot="code-block"]');
			expect(block).toHaveAttribute("data-theme", "dark");
			expect(block).toHaveStyle({
				"--scrollbar-thumb": "oklch(1 0 0 / 42%)",
				"--scrollbar-track": "oklch(1 0 0 / 12%)",
			});
		});
	});
});
