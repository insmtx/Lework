"use client";

import {
	type AuthTokenResponse,
	type AuthUser,
	authApi,
	useAuthStore,
	useChatStore,
	useLayoutStore,
} from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Checkbox } from "@leros/ui/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogTitle,
} from "@leros/ui/components/ui/dialog";
import { Input } from "@leros/ui/components/ui/input";
import { cn } from "@leros/ui/lib/utils";
import { Eye, EyeOff, LockKeyhole, Mail, UserRound } from "lucide-react";
import {
	createContext,
	type FormEvent,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useState,
} from "react";

type AuthMode = "login" | "register";

type AuthContextValue = {
	isAuthenticated: boolean;
	user: AuthUser | null;
	openAuthDialog: (mode?: AuthMode) => void;
	requireAuth: (afterAuth?: () => void, mode?: AuthMode) => boolean;
	logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({
	children,
	logoSrc = "/logo.svg",
}: {
	children: ReactNode;
	logoSrc?: string;
}) {
	const authUser = useAuthStore((s) => s.authUser);
	const setAuthToken = useAuthStore((s) => s.setAuthToken);
	const logoutAuth = useAuthStore((s) => s.logout);
	const fetchProjects = useLayoutStore((s) => s.fetchProjects);
	const resetAuthScopedData = useLayoutStore((s) => s.resetAuthScopedData);
	const resetLocalMessages = useChatStore((s) => s.resetLocalMessages);
	const [dialogOpen, setDialogOpen] = useState(false);
	const [mode, setMode] = useState<AuthMode>("login");
	const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);

	const openAuthDialog = useCallback((nextMode: AuthMode = "login") => {
		setMode(nextMode);
		setDialogOpen(true);
	}, []);

	const handleAuthenticated = useCallback(
		(token: AuthTokenResponse) => {
			setAuthToken(token);
			setDialogOpen(false);
			void fetchProjects();
			const action = pendingAction;
			setPendingAction(null);
			action?.();
		},
		[fetchProjects, pendingAction, setAuthToken],
	);

	const requireAuth = useCallback(
		(afterAuth?: () => void, nextMode: AuthMode = "login") => {
			if (authUser) {
				afterAuth?.();
				return true;
			}
			setPendingAction(() => afterAuth ?? null);
			setMode(nextMode);
			setDialogOpen(true);
			return false;
		},
		[authUser],
	);

	const logout = useCallback(() => {
		logoutAuth();
		resetAuthScopedData();
		resetLocalMessages();
		setPendingAction(null);
	}, [logoutAuth, resetAuthScopedData, resetLocalMessages]);

	const value = useMemo<AuthContextValue>(
		() => ({
			isAuthenticated: Boolean(authUser),
			user: authUser,
			openAuthDialog,
			requireAuth,
			logout,
		}),
		[authUser, openAuthDialog, requireAuth, logout],
	);

	return (
		<AuthContext.Provider value={value}>
			{children}
			<AuthDialog
				mode={mode}
				open={dialogOpen}
				logoSrc={logoSrc}
				onModeChange={setMode}
				onOpenChange={(open) => {
					setDialogOpen(open);
					if (!open) setPendingAction(null);
				}}
				onAuthenticated={handleAuthenticated}
			/>
		</AuthContext.Provider>
	);
}

export function useAuth() {
	const context = useContext(AuthContext);
	if (!context) {
		throw new Error("useAuth must be used inside AuthProvider");
	}
	return context;
}

function AuthDialog({
	mode,
	open,
	logoSrc,
	onModeChange,
	onOpenChange,
	onAuthenticated,
}: {
	mode: AuthMode;
	open: boolean;
	logoSrc: string;
	onModeChange: (mode: AuthMode) => void;
	onOpenChange: (open: boolean) => void;
	onAuthenticated: (token: AuthTokenResponse) => void;
}) {
	const [name, setName] = useState("");
	const [email, setEmail] = useState("");
	const [password, setPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [agreed, setAgreed] = useState(true);
	const [passwordVisible, setPasswordVisible] = useState(false);
	const [confirmVisible, setConfirmVisible] = useState(false);
	const [submitting, setSubmitting] = useState(false);
	const [errorMessage, setErrorMessage] = useState("");
	const [submitted, setSubmitted] = useState(false);
	const [touched, setTouched] = useState<Record<string, boolean>>({});

	useEffect(() => {
		if (!open) return;
		setName("");
		setEmail("");
		setPassword("");
		setConfirmPassword("");
		setAgreed(true);
		setPasswordVisible(false);
		setConfirmVisible(false);
		setSubmitted(false);
		setTouched({});
		setErrorMessage("");
	}, [open, mode]);

	const emailValid = /\S+@\S+\.\S+/.test(email);
	const passwordValid = isRegisterPasswordValid(password);
	const registerValid =
		name.trim().length > 0 && emailValid && passwordValid && password === confirmPassword && agreed;
	const loginValid = emailValid && password.length > 0;
	const canSubmit = mode === "register" ? registerValid : loginValid;
	const shouldShowError = (field: string) => submitted || Boolean(touched[field]);
	const showNameError = shouldShowError("name") && mode === "register" && name.trim().length === 0;
	const showEmailError = shouldShowError("email") && !emailValid;
	const showLoginPasswordError =
		shouldShowError("password") && mode === "login" && password.length === 0;
	const showRegisterPasswordError =
		shouldShowError("password") && mode === "register" && password.length > 0 && !passwordValid;
	const showConfirmPasswordError =
		shouldShowError("confirmPassword") && mode === "register" && confirmPassword !== password;
	const confirmPasswordErrorMessage =
		showConfirmPasswordError && confirmPassword.length > 0 ? "密码不一致" : "请再次输入密码";
	const markTouched = (field: string) => {
		setTouched((current) => ({ ...current, [field]: true }));
	};

	const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		setSubmitted(true);
		if (!canSubmit || submitting) return;

		setSubmitting(true);
		setErrorMessage("");
		try {
			const response =
				mode === "register"
					? await authApi.registerByEmail({
							email: email.trim(),
							password,
							confirm_password: confirmPassword,
							name: name.trim(),
						})
					: await authApi.loginByEmail({
							email: email.trim(),
							password,
						});

			const result = response.data;
			if (result.code !== 0) {
				setErrorMessage(result.message || (mode === "register" ? "注册失败" : "登录失败"));
				return;
			}

			onAuthenticated(result.data);
		} catch (err) {
			console.error(`${mode === "register" ? "register" : "login"} by email error:`, err);
			setErrorMessage(
				getAuthErrorMessage(err) ??
					(mode === "register" ? "注册失败，请稍后再试" : "登录失败，请稍后再试"),
			);
		} finally {
			setSubmitting(false);
		}
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				className="max-w-[640px] rounded-[24px] border-0 bg-[#f8f9fd] px-8 pb-8 pt-9 text-[#070d1c] shadow-[0_24px_70px_rgba(15,23,42,0.26)] sm:px-12"
				showCloseButton
			>
				<div className="mx-auto flex w-full max-w-[430px] flex-col items-center">
					<img src={logoSrc} alt="Leros" className="size-[60px] object-contain" />
					<DialogTitle className="mt-5 text-center text-3xl font-bold tracking-normal">
						欢迎来到Leros
					</DialogTitle>
					<DialogDescription className="sr-only">使用邮箱登录或注册 Leros 账号</DialogDescription>

					<div className="mt-8 grid w-full grid-cols-2 rounded-full bg-white/70 p-1">
						<button
							type="button"
							onClick={() => onModeChange("login")}
							className={cn(
								"h-10 rounded-full text-sm font-semibold transition-colors",
								mode === "login"
									? "bg-[#070d1c] text-white shadow-sm"
									: "text-[#8b95a5] hover:text-[#070d1c]",
							)}
						>
							邮箱登录
						</button>
						<button
							type="button"
							onClick={() => onModeChange("register")}
							className={cn(
								"h-10 rounded-full text-sm font-semibold transition-colors",
								mode === "register"
									? "bg-[#070d1c] text-white shadow-sm"
									: "text-[#8b95a5] hover:text-[#070d1c]",
							)}
						>
							邮箱注册
						</button>
					</div>

					<form onSubmit={handleSubmit} className="mt-6 flex w-full flex-col gap-3">
						{mode === "register" && (
							<FieldWithError error={showNameError ? "请输入名称" : undefined}>
								<AuthField icon={<UserRound className="size-4" />} invalid={showNameError}>
									<Input
										value={name}
										onChange={(event) => setName(event.target.value)}
										onBlur={() => markTouched("name")}
										placeholder="请输入名称"
										className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
									/>
								</AuthField>
							</FieldWithError>
						)}
						<FieldWithError error={showEmailError ? "请输入正确的邮箱" : undefined}>
							<AuthField icon={<Mail className="size-4" />} invalid={showEmailError}>
								<Input
									type="email"
									value={email}
									onChange={(event) => setEmail(event.target.value)}
									onBlur={() => markTouched("email")}
									placeholder="请输入邮箱"
									className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
								/>
							</AuthField>
						</FieldWithError>
						<FieldWithError
							error={
								showRegisterPasswordError
									? "8-20位,数字/大写字母/小写字母/字符至少3种"
									: showLoginPasswordError
										? "请输入密码"
										: undefined
							}
						>
							<AuthField
								icon={<LockKeyhole className="size-4" />}
								invalid={showRegisterPasswordError || showLoginPasswordError}
							>
								<Input
									type={passwordVisible ? "text" : "password"}
									value={password}
									onChange={(event) => setPassword(event.target.value)}
									onBlur={() => markTouched("password")}
									placeholder={
										mode === "register" ? "8-20位,数字/大/小写字母/字符至少3种" : "请输入密码"
									}
									className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
								/>
								<button
									type="button"
									onClick={() => setPasswordVisible((visible) => !visible)}
									className="text-[#9aa3b2] transition-colors hover:text-[#070d1c]"
									aria-label={passwordVisible ? "隐藏密码" : "显示密码"}
								>
									{passwordVisible ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
								</button>
							</AuthField>
						</FieldWithError>
						{mode === "register" && (
							<FieldWithError
								error={showConfirmPasswordError ? confirmPasswordErrorMessage : undefined}
							>
								<AuthField
									icon={<LockKeyhole className="size-4" />}
									invalid={showConfirmPasswordError}
								>
									<Input
										type={confirmVisible ? "text" : "password"}
										value={confirmPassword}
										onChange={(event) => setConfirmPassword(event.target.value)}
										onBlur={() => markTouched("confirmPassword")}
										placeholder="请再次输入密码"
										className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
									/>
									<button
										type="button"
										onClick={() => setConfirmVisible((visible) => !visible)}
										className="text-[#9aa3b2] transition-colors hover:text-[#070d1c]"
										aria-label={confirmVisible ? "隐藏确认密码" : "显示确认密码"}
									>
										{confirmVisible ? <Eye className="size-4" /> : <EyeOff className="size-4" />}
									</button>
								</AuthField>
							</FieldWithError>
						)}

						<div className="mt-1 flex items-center justify-between text-xs font-medium text-[#070d1c]">
							{mode === "login" ? (
								<>
									<button
										type="button"
										onClick={() => onModeChange("register")}
										className="hover:text-[#4d5cff]"
									>
										注册账号
									</button>
									<button type="button" className="hover:text-[#4d5cff]">
										忘记密码
									</button>
								</>
							) : (
								<button
									type="button"
									onClick={() => onModeChange("login")}
									className="text-[#8b95a5] hover:text-[#070d1c]"
								>
									返回登录
								</button>
							)}
						</div>

						{errorMessage && (
							<div className="rounded-xl bg-red-50 px-4 py-2 text-xs font-medium text-red-600">
								{errorMessage}
							</div>
						)}

						<div className="mt-2 flex items-center gap-2.5 text-xs text-[#9aa3b2]">
							<Checkbox
								checked={agreed}
								onCheckedChange={(checked) => setAgreed(checked === true)}
								aria-label="同意服务条款和隐私政策"
								className="size-4 rounded border-[#a6afbd] bg-white data-checked:bg-[#070d1c] data-checked:border-[#070d1c]"
							/>
							<span>
								我已阅读并同意
								<span className="mx-1 text-[#64748b]">《服务条款》</span>和
								<span className="mx-1 text-[#64748b]">《隐私政策》</span>
							</span>
						</div>

						<Button
							type="submit"
							disabled={submitting}
							className={cn(
								"mt-2 h-[52px] rounded-[16px] bg-[#070d1c] text-base font-bold text-white hover:bg-[#182033] disabled:bg-[#d2d5de] disabled:text-white",
								!canSubmit && !submitting && "bg-[#d2d5de] hover:bg-[#d2d5de]",
							)}
						>
							{submitting ? "提交中..." : mode === "register" ? "提交" : "登录"}
						</Button>
					</form>
				</div>
			</DialogContent>
		</Dialog>
	);
}

function FieldWithError({ children, error }: { children: ReactNode; error?: string }) {
	return (
		<div className="space-y-1">
			{children}
			{error && <div className="px-1 text-xs font-medium text-red-500">{error}</div>}
		</div>
	);
}

function AuthField({
	children,
	icon,
	invalid = false,
}: {
	children: ReactNode;
	icon: ReactNode;
	invalid?: boolean;
}) {
	return (
		<div
			className={cn(
				"flex h-[52px] items-center gap-3.5 rounded-[16px] border border-transparent bg-white px-5 text-[#9aa3b2] shadow-[0_8px_22px_rgba(15,23,42,0.03)] transition-colors",
				invalid && "border-red-400 text-red-500 ring-1 ring-red-400",
			)}
		>
			{icon}
			{children}
		</div>
	);
}

function isRegisterPasswordValid(password: string): boolean {
	if (password.length < 8 || password.length > 20) return false;

	const categories = [
		/\d/.test(password),
		/[A-Z]/.test(password),
		/[a-z]/.test(password),
		/[^A-Za-z0-9]/.test(password),
	].filter(Boolean);

	return categories.length >= 3;
}

function getAuthErrorMessage(error: unknown): string | undefined {
	if (!error || typeof error !== "object") return undefined;

	const responseData = (error as { response?: { data?: unknown } }).response?.data;
	if (
		responseData &&
		typeof responseData === "object" &&
		"message" in responseData &&
		typeof (responseData as { message?: unknown }).message === "string"
	) {
		return (responseData as { message: string }).message;
	}

	if ("message" in error && typeof (error as { message?: unknown }).message === "string") {
		return (error as { message: string }).message;
	}

	return undefined;
}
