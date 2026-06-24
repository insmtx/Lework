import type { NextConfig } from "next";

const nextConfig: NextConfig = {
	transpilePackages: ["@leros/ui", "@leros/store", "@leros/app-ui"],
	allowedDevOrigins: ["172.16.0.160", "*", "*.*.*.*", "192.144.198.60"],
	async rewrites() {
		// 中文注释：本地 Web 开发统一走同源 /v1，再由 Next dev 反代到本地后端，避免浏览器跨域干扰调试。
		return [
			{
				source: "/v1/:path*",
				destination: "http://localhost:18080/v1/:path*",
			},
		];
	},
};

export default nextConfig;
