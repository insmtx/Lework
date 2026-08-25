"use client";

import {
	clearStoredAuthUser,
	isPrivateDeployment,
	normalizeAPIBaseURL,
	readServerBaseURL,
	saveServerBaseURL,
	testServerConnection,
} from "@leros/store";
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
import { CheckCircle2, LoaderCircle, Save, Server, Wifi } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { reloadApp } from "./reload";

export function PrivateServerSettingsCard({ onReload = reloadApp }: { onReload?: () => void }) {
	const currentServerURL = readServerBaseURL() ?? "";
	const [serverURL, setServerURL] = useState(currentServerURL);
	const [testedURL, setTestedURL] = useState<string | null>(null);
	const [testing, setTesting] = useState(false);
	const [saving, setSaving] = useState(false);
	const [errorMessage, setErrorMessage] = useState("");

	if (!isPrivateDeployment) {
		return null;
	}

	const handleTest = async () => {
		setTesting(true);
		setErrorMessage("");
		setTestedURL(null);

		try {
			const normalized = await testServerConnection(serverURL);
			setServerURL(normalized);
			setTestedURL(normalized);
			toast.success("服务连接测试通过");
		} catch (error) {
			setErrorMessage(error instanceof Error ? error.message : "无法连接后端服务");
		} finally {
			setTesting(false);
		}
	};

	const handleSave = () => {
		let normalized: string;
		try {
			normalized = normalizeAPIBaseURL(serverURL);
		} catch (error) {
			setErrorMessage(error instanceof Error ? error.message : "服务地址格式无效");
			return;
		}

		if (testedURL !== normalized) {
			setErrorMessage("保存前请先测试当前服务地址");
			return;
		}

		setSaving(true);
		saveServerBaseURL(normalized);
		if (normalized !== currentServerURL) {
			clearStoredAuthUser();
		}
		onReload();
	};

	const canSave = Boolean(testedURL) && !testing && !saving;

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
							修改私有化后端地址。新地址测试通过后才可保存。
						</CardDescription>
					</div>
				</div>
			</CardHeader>
			<CardContent className="space-y-4">
				<label className="block space-y-2" htmlFor="private-server-settings-url">
					<span className="text-sm font-medium text-slate-800">后端服务地址</span>
					<Input
						id="private-server-settings-url"
						type="url"
						value={serverURL}
						onChange={(event) => {
							setServerURL(event.target.value);
							setTestedURL(null);
							setErrorMessage("");
						}}
						placeholder="https://leros.example.com"
						disabled={testing || saving}
						aria-invalid={Boolean(errorMessage)}
					/>
				</label>
				<p className="text-xs leading-5 text-slate-500">
					切换服务会清除当前登录状态，并在保存后重新加载应用。
				</p>
				{errorMessage ? (
					<p className="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
						{errorMessage}
					</p>
				) : null}
				{testedURL ? (
					<p className="flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">
						<CheckCircle2 className="size-4" />
						连接测试通过
					</p>
				) : null}
			</CardContent>
			<CardFooter className="flex justify-end gap-2">
				<Button
					type="button"
					variant="outline"
					onClick={handleTest}
					disabled={testing || saving || !serverURL.trim()}
				>
					{testing ? <LoaderCircle className="animate-spin" /> : <Wifi />}
					{testing ? "测试中" : "测试连接"}
				</Button>
				<Button type="button" onClick={handleSave} disabled={!canSave}>
					{saving ? <LoaderCircle className="animate-spin" /> : <Save />}
					保存并重新加载
				</Button>
			</CardFooter>
		</Card>
	);
}
