/// <reference types="vite/client" />

interface ImportMetaEnv {
	readonly VITE_LEWORK_API_BASE_URL?: string;
	readonly VITE_LEWORK_APP_VERSION?: string;
	readonly VITE_LEWORK_DEPLOYMENT_MODE?: "public" | "private";
}

interface Window {
	__DEPLOYCONFIG?: {
		version?: "public" | "private";
		mode?: string;
		appName?: string;
		logo?: string;
	};
}
