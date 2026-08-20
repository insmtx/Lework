"use client";

import { Switch } from "@leros/ui/components/ui/switch";
import { cn } from "@leros/ui/lib/utils";
import { Moon, Sun } from "lucide-react";
import { type CSSProperties, type ReactNode, useEffect, useState } from "react";

import {
	CODE_BLOCK_THEME_EVENT,
	type CodeBlockTheme,
	readCodeBlockTheme,
	writeCodeBlockTheme,
} from "./codeBlockTheme";

type CodeBlockProps = {
	children?: ReactNode;
	className?: string;
	containerClassName?: string;
};

export function CodeBlock({ children, className, containerClassName }: CodeBlockProps) {
	const [theme, setTheme] = useState<CodeBlockTheme>("light");

	useEffect(() => {
		setTheme(readCodeBlockTheme());
		const onThemeChange = (event: Event) => {
			const next = (event as CustomEvent<CodeBlockTheme>).detail;
			if (next === "light" || next === "dark") {
				setTheme(next);
			}
		};
		window.addEventListener(CODE_BLOCK_THEME_EVENT, onThemeChange);
		return () => window.removeEventListener(CODE_BLOCK_THEME_EVENT, onThemeChange);
	}, []);

	const isDark = theme === "dark";
	const scrollbarVars = isDark
		? {
				"--scrollbar-thumb": "oklch(1 0 0 / 42%)",
				"--scrollbar-thumb-hover": "oklch(1 0 0 / 62%)",
				"--scrollbar-track": "oklch(1 0 0 / 12%)",
			}
		: {
				"--scrollbar-thumb": "oklch(0 0 0 / 22%)",
				"--scrollbar-thumb-hover": "oklch(0 0 0 / 34%)",
				"--scrollbar-track": "oklch(0 0 0 / 6%)",
			};

	return (
		<div
			data-slot="code-block"
			data-theme={theme}
			style={scrollbarVars as CSSProperties}
			className={cn(
				"not-prose my-2 flex flex-col overflow-hidden rounded-lg border shadow-none",
				isDark
					? "border-slate-600 bg-slate-800 text-slate-100"
					: "border-slate-200 bg-slate-50 text-slate-800",
				containerClassName,
			)}
		>
			<div
				className={cn(
					"flex shrink-0 items-center justify-end gap-1 px-3 pt-3",
					isDark ? "text-slate-400" : "text-slate-500",
				)}
			>
				<Sun
					aria-hidden
					className={cn("size-3.5 shrink-0", isDark ? "text-slate-400" : "text-amber-500")}
				/>
				<Switch
					size="sm"
					checked={isDark}
					aria-label={isDark ? "切换为亮色代码块" : "切换为暗色代码块"}
					onCheckedChange={(checked) => {
						const next: CodeBlockTheme = checked ? "dark" : "light";
						setTheme(next);
						writeCodeBlockTheme(next);
					}}
				/>
				<Moon
					aria-hidden
					className={cn("size-3.5 shrink-0", isDark ? "text-sky-300" : "text-slate-400")}
				/>
			</div>
			<pre
				className={cn(
					"m-0 overflow-x-auto p-3 pt-0 text-[13px] leading-6",
					isDark ? "text-slate-100" : "text-slate-800",
					"[&_code]:bg-transparent [&_code]:p-0 [&_code]:text-[13px] [&_code]:leading-6 [&_code]:text-inherit",
					className,
				)}
			>
				{children}
			</pre>
		</div>
	);
}
