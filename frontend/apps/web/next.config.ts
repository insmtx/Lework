import type { NextConfig } from "next";

const nextConfig: NextConfig = {
	transpilePackages: ["@leros/ui", "@leros/store"],
	allowedDevOrigins: ["172.16.0.160", "*", "*.*.*.*", "192.144.198.60"],
};

export default nextConfig;
