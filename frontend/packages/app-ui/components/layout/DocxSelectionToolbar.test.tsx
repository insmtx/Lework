import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DocxSelectionToolbar } from "./DocxSelectionToolbar";

afterEach(() => {
	document.body.replaceChildren();
});

describe("DocxSelectionToolbar", () => {
	it("offers add-to-conversation and polish actions without sending immediately", () => {
		const onPolish = vi.fn();
		const onAddToConversation = vi.fn();
		render(
			<DocxSelectionToolbar
				anchor={{ x: 100, y: 200, width: 240, height: 30 }}
				busy={false}
				onPolish={onPolish}
				onAddToConversation={onAddToConversation}
			/>,
		);

		fireEvent.click(screen.getByRole("button", { name: "添加到对话" }));
		expect(onAddToConversation).toHaveBeenCalledTimes(1);
		expect(onPolish).not.toHaveBeenCalled();

		fireEvent.click(screen.getByRole("button", { name: "AI 润色" }));
		fireEvent.click(screen.getByRole("button", { name: "优化表达" }));
		expect(onPolish).toHaveBeenCalledWith("improve-expression");

		fireEvent.click(screen.getByRole("button", { name: "AI 润色" }));
		fireEvent.mouseEnter(screen.getByRole("button", { name: "调整语气" }));
		fireEvent.click(screen.getByRole("button", { name: "有说服力" }));
		expect(onPolish).toHaveBeenLastCalledWith({ kind: "tone", tone: "有说服力" });
	});

	it("anchors inside the document scroll layer without recalculating while scrolling", () => {
		const container = document.createElement("div");
		Object.defineProperty(container, "getBoundingClientRect", {
			value: () => new DOMRect(40, 80, 800, 600),
		});
		container.scrollLeft = 20;
		container.scrollTop = 300;
		document.body.appendChild(container);

		render(
			<DocxSelectionToolbar
				anchor={{ x: 140, y: 280, width: 100, height: 24 }}
				portalContainer={container}
				busy={false}
				onPolish={vi.fn()}
				onAddToConversation={vi.fn()}
			/>,
		);

		const toolbar = container.querySelector<HTMLElement>("[data-docx-selection-toolbar]");
		expect(toolbar?.classList.contains("absolute")).toBe(true);
		expect(toolbar?.style.left).toBe("170px");
		expect(toolbar?.style.top).toBe("490px");

		container.scrollTop = 500;
		fireEvent.scroll(container);
		expect(toolbar?.style.top).toBe("490px");
	});

	it("opens the polish menu downward when the top boundary has insufficient space", () => {
		const container = document.createElement("div");
		Object.defineProperties(container, {
			clientHeight: { value: 600 },
			getBoundingClientRect: {
				value: () => new DOMRect(40, 80, 800, 600),
			},
		});
		document.body.appendChild(container);

		render(
			<DocxSelectionToolbar
				anchor={{ x: 140, y: 120, width: 100, height: 24 }}
				portalContainer={container}
				busy={false}
				onPolish={vi.fn()}
				onAddToConversation={vi.fn()}
			/>,
		);

		const trigger = screen.getByRole("button", { name: "AI 润色" });
		const menu = container.querySelector<HTMLElement>("[data-docx-polish-menu]");
		Object.defineProperty(trigger, "getBoundingClientRect", {
			value: () => new DOMRect(160, 100, 120, 44),
		});
		Object.defineProperty(menu, "offsetHeight", { value: 220 });

		fireEvent.mouseEnter(trigger);

		expect(menu?.dataset.placement).toBe("bottom");
		expect(menu?.dataset.origin).toBe("top-left");
		expect(menu?.classList.contains("top-full")).toBe(true);
	});

	it("keeps the preferred upward placement when the menu fits", () => {
		const container = document.createElement("div");
		Object.defineProperties(container, {
			clientHeight: { value: 600 },
			getBoundingClientRect: {
				value: () => new DOMRect(40, 80, 800, 600),
			},
		});
		document.body.appendChild(container);

		render(
			<DocxSelectionToolbar
				anchor={{ x: 140, y: 420, width: 100, height: 24 }}
				portalContainer={container}
				busy={false}
				onPolish={vi.fn()}
				onAddToConversation={vi.fn()}
			/>,
		);

		const trigger = screen.getByRole("button", { name: "AI 润色" });
		const menu = container.querySelector<HTMLElement>("[data-docx-polish-menu]");
		Object.defineProperty(trigger, "getBoundingClientRect", {
			value: () => new DOMRect(160, 400, 120, 44),
		});
		Object.defineProperty(menu, "offsetHeight", { value: 220 });

		fireEvent.mouseEnter(trigger);

		expect(menu?.dataset.placement).toBe("top");
		expect(menu?.dataset.origin).toBe("bottom-left");
		expect(menu?.classList.contains("bottom-full")).toBe(true);
	});
});
