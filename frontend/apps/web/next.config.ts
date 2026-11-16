import type { NextConfig } from "next";

// PC 工作台与 OpenCode 控制面同源：浏览器请求 /api/* 由 Next 代理到控制面，
// HttpOnly Cookie 不需要放宽 SameSite，也不需要 CORS。
// PTY 是 WebSocket，Next rewrite 不保证转发 Upgrade；生产环境需由前置代理
// 保留 Host/Origin 并放行 Upgrade，或用 NEXT_PUBLIC_LEWORK_CONTROL_PLANE_WS_URL
// 显式指向控制面。
const controlPlaneUrl = (process.env.LEWORK_CONTROL_PLANE_URL ?? "http://127.0.0.1:8787").replace(
	/\/$/,
	"",
);

const nextConfig: NextConfig = {
	// Docker 部署需要：生成自包含的 .next/standalone 产物供运行阶段使用
	output: "standalone",
	transpilePackages: [
		"@lework/ui",
		"@lework/store",
		"@lework/app-ui",
		"@lework/api",
		"@lework/control-ui",
	],
	allowedDevOrigins: ["172.16.0.160", "*", "*.*.*.*", "192.144.198.60"],
	rewrites: async () => [{ source: "/api/:path*", destination: `${controlPlaneUrl}/api/:path*` }],
};

export default nextConfig;
