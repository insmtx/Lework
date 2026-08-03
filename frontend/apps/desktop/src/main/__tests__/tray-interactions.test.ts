import type { Menu, Tray } from "electron";
import { describe, expect, it, vi } from "vitest";
import { configureTrayInteractions } from "../tray-interactions";

type TrayEvent = "click" | "right-click";

function createTrayMock() {
	const listeners = new Map<TrayEvent, () => void>();
	const setContextMenu = vi.fn();
	const popUpContextMenu = vi.fn();
	const tray = {
		setContextMenu,
		popUpContextMenu,
		on: vi.fn((event: TrayEvent, listener: () => void) => {
			listeners.set(event, listener);
		}),
	} as unknown as Tray;

	return { listeners, popUpContextMenu, setContextMenu, tray };
}

describe("configureTrayInteractions", () => {
	it("Linux 应注册 StatusNotifierItem 原生菜单，并在激活事件中恢复窗口", () => {
		const { listeners, popUpContextMenu, setContextMenu, tray } = createTrayMock();
		const menu = {} as Menu;
		const buildMenu = vi.fn(() => menu);
		const showWindow = vi.fn();

		configureTrayInteractions(tray, "linux", buildMenu, showWindow);

		expect(setContextMenu).toHaveBeenCalledWith(menu);
		expect(popUpContextMenu).not.toHaveBeenCalled();
		expect(listeners.has("right-click")).toBe(false);

		listeners.get("click")?.();
		expect(showWindow).toHaveBeenCalledOnce();
	});

	it("非 Linux 平台应继续在左右键时动态弹出菜单", () => {
		const { listeners, popUpContextMenu, setContextMenu, tray } = createTrayMock();
		const firstMenu = {} as Menu;
		const secondMenu = {} as Menu;
		const buildMenu = vi.fn().mockReturnValueOnce(firstMenu).mockReturnValueOnce(secondMenu);

		configureTrayInteractions(tray, "win32", buildMenu, vi.fn());

		expect(setContextMenu).not.toHaveBeenCalled();
		listeners.get("click")?.();
		listeners.get("right-click")?.();
		expect(popUpContextMenu).toHaveBeenNthCalledWith(1, firstMenu);
		expect(popUpContextMenu).toHaveBeenNthCalledWith(2, secondMenu);
	});
});
