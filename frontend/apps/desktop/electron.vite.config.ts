import { resolve } from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import desktopPackage from "./package.json";

const deploymentMode = process.env.VITE_LEROS_DEPLOYMENT_MODE ?? "public";

export default defineConfig({
	main: {
		plugins: [externalizeDepsPlugin()],
		define: {
			"process.env.LEROS_DEPLOYMENT_MODE": JSON.stringify(deploymentMode),
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
		},
		define: {
			"import.meta.env.VITE_LEROS_APP_VERSION": JSON.stringify(desktopPackage.version),
			"import.meta.env.VITE_LEROS_DEPLOYMENT_MODE": JSON.stringify(deploymentMode),
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
