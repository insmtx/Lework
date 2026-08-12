"use client";

import { isBrandingSettingsEnabled, isPrivateDeployment } from "@leros/store";
import { BrandingSettingsCard } from "./BrandingSettingsCard";
import { PrivateServerSettingsCard } from "./PrivateServerSettingsCard";

export function PrivateSettingsPage({ onReload }: { onReload?: () => void }) {
	if (!isPrivateDeployment) {
		return null;
	}

	const showBrandingSettings = isBrandingSettingsEnabled();

	return (
		<div className="min-h-0 flex-1 overflow-y-auto bg-[linear-gradient(180deg,#f4f7fb_0%,#eef3ff_100%)]">
			<div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-6 py-8">
				<div className="space-y-2">
					<h1 className="text-2xl font-semibold tracking-tight text-slate-950">系统设置</h1>
					<p className="max-w-2xl text-sm leading-6 text-slate-600">
						{showBrandingSettings
							? "管理私有化后端服务连接与系统品牌展示。"
							: "管理私有化后端服务连接。"}
					</p>
				</div>

				<PrivateServerSettingsCard onReload={onReload} />
				{showBrandingSettings ? <BrandingSettingsCard /> : null}
			</div>
		</div>
	);
}
