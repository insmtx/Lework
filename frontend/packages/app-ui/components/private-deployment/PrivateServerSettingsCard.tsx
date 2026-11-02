"use client";

import { API_BASE_URL, isPrivateDeployment } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@leros/ui/components/ui/card";
import { Input } from "@leros/ui/components/ui/input";
import { LoaderCircle, Save, Server } from "lucide-react";
import { useState } from "react";
import type { RenderServerDiscovery } from "./discovery";
import { reloadApp } from "./reload";
import { useServerConnection } from "./useServerConnection";

export function PrivateServerSettingsCard({
	onReload = reloadApp,
	renderDiscovery,
}: {
	onReload?: () => void;
	renderDiscovery?: RenderServerDiscovery;
}) {
	const [serverURL, setServerURL] = useState(API_BASE_URL);
	const { testing, errorMessage, pendingURL, connect, confirm, cancel, clearError } =
		useServerConnection(onReload, true);
	if (!isPrivateDeployment) return null;

	return (
		<Card className="border-indigo-100 bg-white/90 shadow-sm backdrop-blur">
			<CardHeader>
				<div className="flex items-center gap-3">
					<span className="flex size-10 items-center justify-center rounded-xl bg-indigo-100 text-indigo-600">
						<Server className="size-5" />
					</span>
					<div>
						<CardTitle>服务连接</CardTitle>
						<CardDescription className="mt-1">
							选择附近设备或手动输入地址，验证通过后保存连接。
						</CardDescription>
					</div>
				</div>
			</CardHeader>
			<CardContent className="space-y-4">
				{renderDiscovery?.(async (url) => {
					setServerURL(url);
					await connect(url, true);
				}, testing || Boolean(pendingURL))}
				<label className="block space-y-2" htmlFor="private-server-settings-url">
					<span className="text-sm font-medium text-slate-800">后端服务地址</span>
					<Input
						id="private-server-settings-url"
						type="url"
						value={serverURL}
						onChange={(event) => {
							setServerURL(event.target.value);
							cancel();
							clearError();
						}}
						placeholder="https://leros.example.com"
						disabled={testing}
						aria-invalid={Boolean(errorMessage)}
					/>
				</label>
				<p className="text-xs leading-5 text-slate-500">
					保存后会重新加载应用。当前地址保留登录状态；切换服务需要重新登录。
				</p>
				{errorMessage ? (
					<p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
						{errorMessage}
					</p>
				) : null}
				{pendingURL && (
					<div
						role="alertdialog"
						aria-label="确认切换服务"
						className="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-3"
					>
						<p className="text-sm">
							切换到此服务会清除当前登录状态，需要重新登录。请确认这是你要连接的服务。
						</p>
						<p className="break-all text-xs">{pendingURL}</p>
						<Button type="button" onClick={confirm}>
							确认切换
						</Button>
						<Button type="button" variant="outline" onClick={cancel}>
							取消
						</Button>
					</div>
				)}
			</CardContent>
			<CardFooter className="flex justify-end gap-2">
				<Button
					type="button"
					onClick={() => void connect(serverURL)}
					disabled={testing || Boolean(pendingURL) || !serverURL.trim()}
				>
					{testing ? <LoaderCircle className="animate-spin" /> : <Save />}
					{testing ? "正在验证连接" : "连接并保存，重新加载"}
				</Button>
			</CardFooter>
		</Card>
	);
}
