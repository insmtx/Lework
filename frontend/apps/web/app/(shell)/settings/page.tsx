"use client";

import { PrivateSettingsPage } from "@leros/app-ui";
import { isPrivateDeployment } from "@leros/store";
import { redirect } from "next/navigation";

export default function SettingsPage() {
	// 中文注释：模型管理已迁入组织管理；非私有化不再保留独立系统设置页。
	if (!isPrivateDeployment) {
		redirect("/workbench");
	}

	return <PrivateSettingsPage />;
}
