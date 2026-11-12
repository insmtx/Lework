import type { Metadata, Viewport } from "next";
import "./globals.css";

export const metadata: Metadata = {
	title: "Lework Control Plane",
	description: "Browser control plane for Lework OpenCode nodes",
};

// 手机浏览器：让页面延伸到刘海/圆角下方，同时用 resizes-content 让软键盘
// 压缩可视区域而不是盖住底部 composer。
export const viewport: Viewport = {
	width: "device-width",
	initialScale: 1,
	viewportFit: "cover",
	interactiveWidget: "resizes-content",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
	return (
		<html lang="zh-CN" className="h-full">
			<body className="h-full bg-background text-foreground antialiased">{children}</body>
		</html>
	);
}
