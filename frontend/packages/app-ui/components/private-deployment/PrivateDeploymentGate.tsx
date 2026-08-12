"use client";

import {
	hasPrivateServerConfiguration,
	isPrivateDeployment,
	savePrivateServerBaseURL,
	testServerConnection,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Input } from "@leros/ui/components/ui/input";
import { CheckCircle2, LoaderCircle, Server } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { reloadApp } from "./reload";

export function PrivateDeploymentGate({
	children,
	onReload = reloadApp,
}: {
	children: ReactNode;
	onReload?: () => void;
}) {
	const requiresConfiguration = isPrivateDeployment && !hasPrivateServerConfiguration();

	if (!requiresConfiguration) {
		return children;
	}

	return <PrivateServerSetupDialog onReload={onReload} />;
}

function PrivateServerSetupDialog({ onReload }: { onReload: () => void }) {
	const [serverURL, setServerURL] = useState("");
	const [testing, setTesting] = useState(false);
	const [errorMessage, setErrorMessage] = useState("");
	const [connectionPassed, setConnectionPassed] = useState(false);

	const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		setTesting(true);
		setErrorMessage("");
		setConnectionPassed(false);

		try {
			const normalized = await testServerConnection(serverURL);
			setConnectionPassed(true);
			savePrivateServerBaseURL(normalized);
			onReload();
		} catch (error) {
			setErrorMessage(error instanceof Error ? error.message : "无法连接后端服务");
		} finally {
			setTesting(false);
		}
	};

	return (
		<Dialog open disablePointerDismissal onOpenChange={() => undefined}>
			<DialogContent
				className="max-w-130 rounded-2xl border-slate-200 bg-white p-0 shadow-2xl"
				showCloseButton={false}
			>
				<form onSubmit={handleSubmit}>
					<div className="border-b border-slate-100 bg-slate-50 px-7 py-6">
						<div className="mb-4 flex size-12 items-center justify-center rounded-2xl bg-indigo-100 text-indigo-600">
							<Server className="size-6" />
						</div>
						<DialogHeader>
							<DialogTitle className="text-2xl text-slate-950">连接私有化服务</DialogTitle>
							<DialogDescription className="leading-6 text-slate-600">
								首次使用前，请配置 Lework 后端服务地址。连接测试通过后才能进入应用。
							</DialogDescription>
						</DialogHeader>
					</div>

					<div className="space-y-4 px-7 py-6">
						<label className="block space-y-2" htmlFor="private-server-url">
							<span className="text-sm font-medium text-slate-800">后端服务地址</span>
							<Input
								id="private-server-url"
								type="url"
								value={serverURL}
								onChange={(event) => {
									setServerURL(event.target.value);
									setErrorMessage("");
									setConnectionPassed(false);
								}}
								placeholder="https://leros.example.com"
								autoFocus
								disabled={testing}
								aria-invalid={Boolean(errorMessage)}
							/>
						</label>
						<p className="text-xs leading-5 text-slate-500">
							支持 http:// 或 https:// 地址；未填写 /v1 时将自动补齐。
						</p>

						{errorMessage ? (
							<p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
								{errorMessage}
							</p>
						) : null}
						{connectionPassed ? (
							<p className="flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
								<CheckCircle2 className="size-4" />
								连接测试通过，正在进入应用
							</p>
						) : null}
					</div>

					<DialogFooter className="border-t border-slate-100 px-7 py-5">
						<Button type="submit" disabled={testing || !serverURL.trim()}>
							{testing ? <LoaderCircle className="animate-spin" /> : <Server />}
							{testing ? "正在测试连接" : "测试并保存"}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
