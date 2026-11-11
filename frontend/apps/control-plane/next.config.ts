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
				rewrites: async () => [
					{ source: "/api/:path*", destination: "http://127.0.0.1:8787/api/:path*" },
				],
			}),
};

export default nextConfig;
