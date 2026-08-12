import type { Input } from "electron";

export const productionDevToolsAccelerator = "CommandOrControl+Alt+Shift+I";

type DevToolsShortcutInput = Pick<
	Input,
	"alt" | "code" | "control" | "isAutoRepeat" | "meta" | "shift" | "type"
>;

export function isProductionDevToolsShortcut(
	input: DevToolsShortcutInput,
	platform: NodeJS.Platform,
): boolean {
	if (input.type !== "keyDown" || input.isAutoRepeat || input.code !== "KeyI") {
		return false;
	}

	if (platform === "darwin") {
		return input.meta && input.alt && input.shift && !input.control;
	}

	return input.control && input.alt && input.shift && !input.meta;
}
