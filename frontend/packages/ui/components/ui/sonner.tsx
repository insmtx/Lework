"use client";

import { useTheme } from "@leros/ui/components/common/theme-provider";
import { AlertOctagon, AlertTriangle, CircleCheck, Info, Loader } from "lucide-react";
import { Toaster as Sonner, type ToasterProps } from "sonner";

const Toaster = ({ ...props }: ToasterProps) => {
	const { theme = "light" } = useTheme();

	return (
		<Sonner
			theme={theme as ToasterProps["theme"]}
			className="toaster group"
			icons={{
				success: <CircleCheck className="size-4" />,
				info: <Info className="size-4" />,
				warning: <AlertTriangle className="size-4" />,
				error: <AlertOctagon className="size-4" />,
				loading: <Loader className="size-4 animate-spin" />,
			}}
			style={
				{
					"--normal-bg": "var(--popover)",
					"--normal-text": "var(--popover-foreground)",
					"--normal-border": "var(--border)",
					"--border-radius": "var(--radius)",
				} as React.CSSProperties
			}
			toastOptions={{
				classNames: {
					toast: "cn-toast",
				},
			}}
			{...props}
		/>
	);
};

export { Toaster };
