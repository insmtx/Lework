"use client";

import { ApiError } from "@lework/api";
import { Button } from "@lework/ui/components/ui/button";
import { Input } from "@lework/ui/components/ui/input";
import { Loader2, LockKeyhole, RefreshCw, ServerCrash } from "lucide-react";
import { type FormEvent, type ReactNode, useCallback, useEffect, useState } from "react";
import { useControlApi } from "../context";

type GateStatus = "checking" | "authenticated" | "anonymous" | "unreachable";

/**
 * ControlAuthGate 在渲染控制台之前确认控制面会话。
 *
 * 控制面用 HttpOnly Cookie 鉴权，PC 与内嵌页都通过同源 `/api` 访问；远端部署
 * 时浏览器没有控制面会话，页面只会收到一串 401。把「未登录」与「连不上」
 * 分开处理，避免网络故障被误判成登录过期。本机 loopback 直通时 `/api/me`
 * 直接通过，不显示登录框。
 */
export function ControlAuthGate({ children }: { children: ReactNode }) {
	const { client } = useControlApi();
	const [status, setStatus] = useState<GateStatus>("checking");
	const [error, setError] = useState<string | null>(null);

	const check = useCallback(async () => {
		setStatus("checking");
		setError(null);
		try {
			await client.me();
			setStatus("authenticated");
		} catch (caught) {
			if (caught instanceof ApiError && caught.status === 401) {
				setStatus("anonymous");
				return;
			}
			setStatus("unreachable");
			setError(caught instanceof Error ? caught.message : "无法连接控制面");
		}
	}, [client.me]);

	useEffect(() => {
		void check();
	}, [check]);

	if (status === "authenticated") return children;

	if (status === "checking") {
		return (
			<div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
				<Loader2 className="size-4 animate-spin" /> 正在连接控制面…
			</div>
		);
	}

	return (
		<div className="flex h-full items-center justify-center bg-[var(--lework-app-bg)] p-6">
			<div className="w-full max-w-sm rounded-2xl border border-[var(--lework-control-border)] bg-white p-6 shadow-sm">
				<div className="mb-4 flex size-11 items-center justify-center rounded-2xl bg-[var(--lework-primary-soft)] text-[var(--lework-primary)]">
					{status === "unreachable" ? (
						<ServerCrash className="size-5" />
					) : (
						<LockKeyhole className="size-5" />
					)}
				</div>
				{status === "unreachable" ? (
					<>
						<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">
							连接不上控制面
						</h1>
						<p className="mt-1 text-sm text-[var(--lework-text-muted)]">
							{error ?? "请确认控制面已启动，或检查反向代理配置。"}
						</p>
						<Button className="mt-4 w-full" variant="outline" onClick={() => void check()}>
							<RefreshCw className="size-4" /> 重试
						</Button>
					</>
				) : (
					<LoginForm onAuthenticated={() => setStatus("authenticated")} />
				)}
			</div>
		</div>
	);
}

function LoginForm({ onAuthenticated }: { onAuthenticated: () => void }) {
	const { client } = useControlApi();
	const [mode, setMode] = useState<"login" | "register">("login");
	const [email, setEmail] = useState("");
	const [password, setPassword] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [canRegister, setCanRegister] = useState(false);

	useEffect(() => {
		let cancelled = false;
		void client
			.getAuthMethods()
			.then((methods) => {
				// 只有「还没有账号且开放注册」时才需要注册入口；已有账号时
				// 多余按钮只会让用户误以为可以再建一个管理员。
				if (!cancelled)
					setCanRegister(!methods.initialized && methods.registration_mode === "open");
			})
			.catch(() => {
				if (!cancelled) setCanRegister(false);
			});
		return () => {
			cancelled = true;
		};
	}, [client.getAuthMethods]);

	const submit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		setBusy(true);
		setError(null);
		try {
			const normalizedEmail = email.trim();
			if (mode === "register") await client.register(normalizedEmail, password);
			else await client.login(normalizedEmail, password);
			onAuthenticated();
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "登录失败");
		} finally {
			setBusy(false);
		}
	};

	return (
		<form onSubmit={(event) => void submit(event)} className="space-y-3">
			<h1 className="text-lg font-semibold text-[var(--lework-text-strong)]">
				{mode === "register" ? "创建管理员账号" : "登录控制面"}
			</h1>
			<p className="text-sm text-[var(--lework-text-muted)]">
				工作区、节点与审批都来自控制面，需要使用控制面账号登录。
			</p>
			<label className="block space-y-1.5" htmlFor="control-plane-email">
				<span className="text-xs font-medium text-[var(--lework-text)]">邮箱</span>
				<Input
					id="control-plane-email"
					type="text"
					autoComplete="username"
					value={email}
					onChange={(event) => setEmail(event.target.value)}
					placeholder="admin"
					disabled={busy}
					autoFocus
				/>
			</label>
			<label className="block space-y-1.5" htmlFor="control-plane-password">
				<span className="text-xs font-medium text-[var(--lework-text)]">密码</span>
				<Input
					id="control-plane-password"
					type="password"
					autoComplete={mode === "register" ? "new-password" : "current-password"}
					value={password}
					onChange={(event) => setPassword(event.target.value)}
					disabled={busy}
				/>
			</label>
			{error ? (
				<p className="rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700" role="alert">
					{error}
				</p>
			) : null}
			<Button type="submit" className="w-full" disabled={busy || !email.trim() || !password}>
				{busy ? <Loader2 className="size-4 animate-spin" /> : null}
				{busy ? "正在提交" : mode === "register" ? "创建并登录" : "登录"}
			</Button>
			{canRegister ? (
				<Button
					type="button"
					variant="ghost"
					className="w-full"
					disabled={busy}
					onClick={() => {
						setError(null);
						setMode((current) => (current === "login" ? "register" : "login"));
					}}
				>
					{mode === "register" ? "已有账号，去登录" : "还没有账号，创建第一个管理员"}
				</Button>
			) : null}
		</form>
	);
}
