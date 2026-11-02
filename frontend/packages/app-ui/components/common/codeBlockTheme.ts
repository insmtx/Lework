export type CodeBlockTheme = "light" | "dark";

export const CODE_BLOCK_THEME_STORAGE_KEY = "lework-code-block-theme";
export const CODE_BLOCK_THEME_EVENT = "lework-code-block-theme-change";

export function readCodeBlockTheme(): CodeBlockTheme {
	if (typeof window === "undefined") return "light";
	try {
		return window.localStorage.getItem(CODE_BLOCK_THEME_STORAGE_KEY) === "dark" ? "dark" : "light";
	} catch {
		return "light";
	}
}

export function writeCodeBlockTheme(theme: CodeBlockTheme): void {
	if (typeof window === "undefined") return;
	try {
		window.localStorage.setItem(CODE_BLOCK_THEME_STORAGE_KEY, theme);
	} catch {
		// ignore quota / private mode
	}
	window.dispatchEvent(new CustomEvent<CodeBlockTheme>(CODE_BLOCK_THEME_EVENT, { detail: theme }));
}
