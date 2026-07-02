import type { NextConfig } from "next";

const nextConfig: NextConfig = {
	// Docker 部署需要：生成自包含的 .next/standalone 产物供运行阶段使用
	output: "standalone",
	transpilePackages: ["@leros/ui", "@leros/store", "@leros/app-ui"],
	allowedDevOrigins: ["172.16.0.160", "*", "*.*.*.*", "192.144.198.60"],
};

export default nextConfig;
