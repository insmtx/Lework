/// <reference types="vite/client" />

interface ImportMetaEnv {
	readonly VITE_LEROS_API_BASE_URL?: string;
	readonly VITE_LEROS_APP_VERSION?: string;
	readonly VITE_LEROS_DEPLOYMENT_MODE?: "public" | "private";
}
