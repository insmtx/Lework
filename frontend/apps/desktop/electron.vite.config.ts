import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import desktopPackage from "./package.json";

const deploymentMode = process.env.VITE_LEWORK_DEPLOYMENT_MODE ?? "public";
const legacyAPIBaseURL = process.env.VITE_LEWORK_API_BASE_URL ?? "http://localhost:8080/v1";
const controlPlaneBaseURL =
	process.env.LEWORK_CONTROL_PLANE_URL ?? "http://127.0.0.1:8787";
// /v1 的请求路径已经带前缀，代理目标只保留 origin，避免拼成 /v1/v1。
const legacyAPIOrigin = /^https?:\/\//.test(legacyAPIBaseURL)
	? legacyAPIBaseURL.replace(/\/v1\/?$/, "")
	: "http://localhost:8080";

/**
 * 开发态由 Vite 代理模拟打包态的同源源。
 *
 * 控制面按 RemoteAddr 认本机管理员、PTY 又比较 Origin 与 Host，所以转发前
 * 必须剥掉 Origin / X-Forwarded-*，否则 dev 与打包版行为不一致。
 */
function configureProxy(proxy: {
	on(event: string, listener: (request: { removeHeader(name: string): void }) => void): void;
}) {
	const strip = (request: { removeHeader(name: string): void }) => {
		request.removeHeader("origin");
		request.removeHeader("x-forwarded-for");
		request.removeHeader("x-forwarded-proto");
	};
	proxy.on("proxyReq", strip);
	proxy.on("proxyReqWs", strip);
}

export default defineConfig({
	main: {
		plugins: [externalizeDepsPlugin()],
		define: {
			"process.env.LEWORK_DEPLOYMENT_MODE": JSON.stringify(deploymentMode),
			"process.env.LEWORK_API_UPSTREAM": JSON.stringify(legacyAPIBaseURL),
			"process.env.LEWORK_CONTROL_PLANE_URL": JSON.stringify(controlPlaneBaseURL),
		},
	},
	preload: {
		plugins: [externalizeDepsPlugin()],
	},
	renderer: {
		publicDir: resolve("src/renderer/public"),
		server: {
			port: Number(process.env.DESKTOP_RENDERER_PORT) || 5175,
			strictPort: true,
			proxy: {
				"/api": {
					target: controlPlaneBaseURL,
					changeOrigin: true,
					ws: true,
					configure: configureProxy,
				},
				"/v1": {
					target: legacyAPIOrigin,
					changeOrigin: true,
					configure: configureProxy,
				},
			},
		},
		define: {
			"import.meta.env.VITE_LEWORK_APP_VERSION": JSON.stringify(desktopPackage.version),
			"import.meta.env.VITE_LEWORK_DEPLOYMENT_MODE": JSON.stringify(deploymentMode),
			// 打包态由主进程提供同源 /v1；渲染层只用相对地址。
			"import.meta.env.VITE_LEWORK_API_BASE_URL": JSON.stringify("/v1"),
		},
		plugins: [react(), tailwindcss()],
		resolve: {
			alias: {
				"@": resolve("src/renderer/src"),
			},
			dedupe: ["react", "react-dom"],
		},
	},
});
