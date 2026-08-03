"use client";

import type React from "react";
import { createContext, useContext, useEffect } from "react";

type Theme = "dark" | "light" | "system";

type ThemeProviderProps = {
	children: React.ReactNode;
};

type ThemeProviderState = {
	theme: Theme;
	setTheme: (theme: Theme) => void;
};

const initialState: ThemeProviderState = {
	theme: "light",
	setTheme: () => null,
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

function ThemeProvider({ children, ...props }: ThemeProviderProps) {
	useEffect(() => {
		const root = window.document.documentElement;
		root.classList.remove("dark");
		root.classList.add("light");
	}, []);

	const value = {
		theme: "light" as const,
		setTheme: () => null,
	};

	return (
		<ThemeProviderContext.Provider {...props} value={value}>
			{children}
		</ThemeProviderContext.Provider>
	);
}

function useTheme() {
	const context = useContext(ThemeProviderContext);

	if (context === undefined) {
		throw new Error("useTheme must be used within a ThemeProvider");
	}

	return context;
}

export { ThemeProvider, useTheme };
