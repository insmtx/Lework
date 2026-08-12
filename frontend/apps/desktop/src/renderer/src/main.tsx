import {
	PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY,
	PRIVATE_SERVER_CONFIG_STORAGE_KEY,
	savePrivateServerBaseURL,
	saveServerBaseURL,
} from "@leros/store";
import ReactDOM from "react-dom/client";
import App from "./App";
import "./globals.css";

const root = document.getElementById("root");

window.lerosDesktop?.onOpenServer((serverURL) => {
	try {
		// 中文注释：深链传入的服务地址代表 Web 当前所在环境，桌面端应继承它。
		saveServerBaseURL(serverURL);
		const isPrivateBuild = import.meta.env.VITE_LEROS_DEPLOYMENT_MODE === "private";
		if (isPrivateBuild) {
			savePrivateServerBaseURL(serverURL);
			window.localStorage.setItem(PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY, "1");
		} else {
			// 公开构建只清理旧版本错误留下的私有化标记，保留通用服务地址覆盖。
			window.localStorage.removeItem(PRIVATE_DEPLOYMENT_MODE_STORAGE_KEY);
			window.localStorage.removeItem(PRIVATE_SERVER_CONFIG_STORAGE_KEY);
		}
		window.location.reload();
	} catch {
		// 中文注释：深链地址由主进程初步校验，仍在渲染端复用服务地址校验，拒绝无效配置。
	}
});

if (root) {
	ReactDOM.createRoot(root).render(<App />);
}
