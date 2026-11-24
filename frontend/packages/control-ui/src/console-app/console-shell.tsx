"use client";

import { type AppNavigation, PrivateDeploymentGate, Shell } from "@lework/app-ui";
import type { ReactNode } from "react";

/**
 * ConsoleShell 是 Web 与桌面共用的控制台外壳。
 *
 * 私有化部署的地址配置必须包在所有页面之外，否则用户会先看到接口报错；
 * 宿主只需要提供导航和加载路径，壳本身不读任何路由库。
 */
export function ConsoleShell({
	navigation,
	logoSrc,
	onReload,
	children,
}: {
	navigation?: AppNavigation;
	logoSrc?: string;
	onReload?: () => void;
	children?: ReactNode;
}) {
	return (
		<PrivateDeploymentGate onReload={onReload}>
			<Shell logoSrc={logoSrc} navigation={navigation}>
				{children}
			</Shell>
		</PrivateDeploymentGate>
	);
}
