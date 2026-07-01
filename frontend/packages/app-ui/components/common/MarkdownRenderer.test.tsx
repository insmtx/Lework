import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

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
