import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
	title: "Lework Control Plane",
	description: "Browser control plane for Lework OpenCode nodes",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
	return (
		<html lang="zh-CN" className="h-full">
			<body className="h-full bg-background text-foreground antialiased">{children}</body>
		</html>
	);
}
