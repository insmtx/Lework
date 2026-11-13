import path from "node:path";
import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";

const isProd = process.env.NODE_ENV === "production";
const workspaceRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

const nextConfig: NextConfig = {
	// Production is a static export copied into backend/internal/controlplane/webui/dist.
	output: isProd ? "export" : undefined,
	turbopack: { root: workspaceRoot },
	trailingSlash: true,
	images: { unoptimized: true },
	transpilePackages: ["@lework/ui", "@lework/api"],
	// Dev only: the browser and the control plane share one origin in production.
	...(isProd
		? {}
		: {
				// 手机上通过统一入口（如 dev.ckeyer.com）或局域网 IP 访问时，Next 会把这些
				// Host 当成跨源，直接屏蔽 /_next/* 开发资源，页面只停在 SSR 外壳、不 hydrate。
				// 列出允许的主机名（不含端口）才能让真机调试跑起来；生产构建不涉及。
				allowedDevOrigins: ["localhost", "127.0.0.1", "dev.ckeyer.com"],
				// 开发指示器（左下角 N 气泡）在 390px 宽的手机上正好压住
				// 输入框和发送按钮，真机验收时容易误判成布局问题。
				devIndicators: false,
				rewrites: async () => [
					{ source: "/api/:path*", destination: "http://127.0.0.1:8787/api/:path*" },
				],
			}),
};

export default nextConfig;
