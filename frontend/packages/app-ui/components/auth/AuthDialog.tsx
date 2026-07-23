"use client";

import {
	AUTH_SESSION_EXPIRED_EVENT,
	type AuthOrgInfo,
	type AuthTokenResponse,
	type AuthUser,
	authApi,
	type PendingOrganizationLoginResponse,
	useAuthStore,
	useChatStore,
	useDAStore,
	useLayoutStore,
	usePermissionStore,
	useSkillStore,
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
import { getRequestErrorMessage } from "@leros/ui/lib/request";
import { cn } from "@leros/ui/lib/utils";
import { ShieldCheck, Smartphone } from "lucide-react";
import {
	createContext,
	type FocusEvent,
	type FormEvent,
	type MouseEvent,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import {
	APP_LOGO_SRC,
	APP_PRIVACY_POLICY_PDF_SRC,
	APP_TERMS_OF_SERVICE_PDF_SRC,
} from "../../assets";
import { OrganizationSwitchPanel } from "../org-admin/OrganizationSwitchPanel";

type AuthMode = "login";
type PolicyDocument = "terms" | "privacy";
type PendingOrganizationLoginState = PendingOrganizationLoginResponse & {
	organizations: AuthOrgInfo[];
};
type DesktopPolicyApi = {
	openPolicyPdf?: (document: PolicyDocument) => Promise<boolean>;
};

type AuthContextValue = {
	isHydrated: boolean;
	isAuthenticated: boolean;
	user: AuthUser | null;
	openAuthDialog: (mode?: AuthMode) => void;
	requireAuth: (afterAuth?: () => void, mode?: AuthMode) => boolean;
	logout: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({
	children,
	logoSrc = APP_LOGO_SRC,
}: {
	children: ReactNode;
	logoSrc?: string;
}) {
	const authUser = useAuthStore((s) => s.authUser);
	const setAuthToken = useAuthStore((s) => s.setAuthToken);
	const refreshAuthSession = useAuthStore((s) => s.refreshAuthSession);
	const logoutAuth = useAuthStore((s) => s.logout);
	const fetchProjects = useLayoutStore((s) => s.fetchProjects);
	const resetAuthScopedData = useLayoutStore((s) => s.resetAuthScopedData);
	const resetDAAuthScopedData = useDAStore((s) => s.resetAuthScopedData);
	const resetSkillAuthScopedData = useSkillStore((s) => s.resetAuthScopedData);
	const fetchAssistants = useDAStore((s) => s.fetchAssistants);
	const fetchInstalledSkills = useSkillStore((s) => s.fetchInstalledSkills);
	const resetLocalMessages = useChatStore((s) => s.resetLocalMessages);
	const clearComposerInput = useChatStore((s) => s.clearComposerInput);
	const invalidateAllPermissions = usePermissionStore((s) => s.invalidateAll);
	const hasRestoredSessionRef = useRef(false);
	const [hydrated, setHydrated] = useState(false);
	const [dialogOpen, setDialogOpen] = useState(false);
	const [pendingOrganizationLogin, setPendingOrganizationLogin] =
		useState<PendingOrganizationLoginState | null>(null);
	const [pendingAction, setPendingAction] = useState<(() => void) | null>(null);

	useEffect(() => {
		setHydrated(true);
	}, []);

	const clearAuthScopedStoreData = useCallback(() => {
		resetAuthScopedData();
		resetDAAuthScopedData();
		resetSkillAuthScopedData();
		resetLocalMessages();
		clearComposerInput();
		invalidateAllPermissions();
	}, [
		clearComposerInput,
		invalidateAllPermissions,
		resetAuthScopedData,
		resetDAAuthScopedData,
		resetLocalMessages,
		resetSkillAuthScopedData,
	]);

	useEffect(() => {
		const handleExpiredSession = () => {
			logoutAuth();
			clearAuthScopedStoreData();
			setPendingAction(null);
			setDialogOpen(true);
		};
		window.addEventListener(AUTH_SESSION_EXPIRED_EVENT, handleExpiredSession);
		return () => window.removeEventListener(AUTH_SESSION_EXPIRED_EVENT, handleExpiredSession);
	}, [clearAuthScopedStoreData, logoutAuth]);

	useEffect(() => {
		if (!hydrated || hasRestoredSessionRef.current || !authUser?.jwtToken) return;
		hasRestoredSessionRef.current = true;
		void refreshAuthSession().then((ok) => {
			if (ok) return;
			logoutAuth();
			clearAuthScopedStoreData();
			setPendingAction(null);
			setDialogOpen(true);
		});
	}, [authUser, clearAuthScopedStoreData, hydrated, logoutAuth, refreshAuthSession]);

	const openAuthDialog = useCallback((_nextMode: AuthMode = "login") => {
		setDialogOpen(true);
	}, []);

	const completeOrganizationLogin = useCallback(
		(token: AuthTokenResponse, initializeOrganizationData = true) => {
			setAuthToken(token);
			setPendingOrganizationLogin(null);
			setDialogOpen(false);
			if (!initializeOrganizationData) return;
			void Promise.all([fetchProjects(), fetchAssistants(), fetchInstalledSkills()]);
			const action = pendingAction;
			setPendingAction(null);
			action?.();
		},
		[fetchAssistants, fetchInstalledSkills, fetchProjects, pendingAction, setAuthToken],
	);

	const chooseOrganization = useCallback(
		async (
			login: PendingOrganizationLoginResponse,
			uin: number,
			initializeOrganizationData = true,
		) => {
			const response = await authApi.chooseUin({
				refresh_token: login.refresh_token,
				uin,
				user_id: login.user_id,
				// 中文注释：后端当前阶段暂不要求登录方式，后续按实际契约恢复传递登录方式。
				// login_way: login.login_way,
			});
			const result = response.data;
			if (result.code !== 0) throw new Error(result.message || "选择组织失败");
			completeOrganizationLogin(
				{
					...result.data,
					user_id: login.user_id,
					login_way: login.login_way,
				},
				initializeOrganizationData,
			);
		},
		[completeOrganizationLogin],
	);

	const handleAuthenticated = useCallback(
		async (login: PendingOrganizationLoginResponse) => {
			// 中文注释：无组织账号的真实响应会省略 organizations，前端统一按空列表进入创建流程。
			const pendingLogin: PendingOrganizationLoginState = {
				...login,
				organizations: login.organizations ?? [],
			};
			const [onlyOrganization] = pendingLogin.organizations;
			if (onlyOrganization && pendingLogin.organizations.length === 1) {
				await chooseOrganization(pendingLogin, onlyOrganization.uin);
				return;
			}
			setPendingOrganizationLogin(pendingLogin);
			setDialogOpen(false);
		},
		[chooseOrganization],
	);

	const handlePendingOrganizationCreate = useCallback(
		async (name: string, userDisplayName: string) => {
			if (!pendingOrganizationLogin) throw new Error("登录状态已失效，请重新登录");
			const response = await authApi.createOrganizationForPendingLogin({
				name,
				refresh_token: pendingOrganizationLogin.refresh_token,
				user_id: pendingOrganizationLogin.user_id,
				// 中文注释：用户在创建组织时填写的昵称需要作为组织成员名称提交。
				user_display_name: userDisplayName,
			});
			const result = response.data;
			if (result.code !== 0) throw new Error(result.message || "创建组织失败");
			if (!result.data.uin) throw new Error("创建组织响应缺少 UIN");
			await chooseOrganization(pendingOrganizationLogin, result.data.uin, false);
		},
		[pendingOrganizationLogin, chooseOrganization],
	);

	const handlePendingOrganizationDone = useCallback(() => {
		const action = pendingAction;
		setPendingAction(null);
		action?.();
	}, [pendingAction]);

	const requireAuth = useCallback(
		(afterAuth?: () => void, _nextMode: AuthMode = "login") => {
			if (isActiveOrganizationSession(authUser)) {
				afterAuth?.();
				return true;
			}
			setPendingAction(() => afterAuth ?? null);
			setDialogOpen(true);
			return false;
		},
		[authUser],
	);

	const logout = useCallback(() => {
		logoutAuth();
		clearAuthScopedStoreData();
		setPendingAction(null);
	}, [clearAuthScopedStoreData, logoutAuth]);

	const value = useMemo<AuthContextValue>(
		() => ({
			isHydrated: hydrated,
			isAuthenticated: hydrated && isActiveOrganizationSession(authUser),
			user: hydrated ? authUser : null,
			openAuthDialog,
			requireAuth,
			logout,
		}),
		[authUser, hydrated, openAuthDialog, requireAuth, logout],
	);

	return (
		<AuthContext.Provider value={value}>
			{children}
			<AuthDialog
				open={dialogOpen}
				logoSrc={logoSrc}
				onOpenChange={(open) => {
					setDialogOpen(open);
					if (!open) setPendingAction(null);
				}}
				onAuthenticated={handleAuthenticated}
			/>
			<Dialog
				open={Boolean(pendingOrganizationLogin)}
				onOpenChange={(open) => {
					if (open) return;
					// 中文注释：首次选择阶段尚未建立正式登录态，关闭时只丢弃待选组织上下文。
					setPendingOrganizationLogin(null);
					setDialogOpen(true);
				}}
			>
				<DialogContent
					className="flex max-h-[min(70dvh,calc(100dvh-2rem))] w-full max-w-none flex-col overflow-hidden p-6"
					style={{ width: "min(33vw, calc(100vw - 2rem))" }}
					showCloseButton
				>
					<OrganizationSwitchPanel
						active={Boolean(pendingOrganizationLogin)}
						initialMode={pendingOrganizationLogin?.organizations.length === 0 ? "create" : "switch"}
						pendingLogin={
							pendingOrganizationLogin
								? {
										organizations: pendingOrganizationLogin.organizations,
										onChoose: (org) => chooseOrganization(pendingOrganizationLogin, org.uin, false),
										onCreate: handlePendingOrganizationCreate,
									}
								: undefined
						}
						onDone={handlePendingOrganizationDone}
					/>
				</DialogContent>
			</Dialog>
		</AuthContext.Provider>
	);
}

function isActiveOrganizationSession(user: AuthUser | null): boolean {
	return Boolean(user?.jwtToken && user.currentOrg);
}

export function useAuth() {
	const context = useContext(AuthContext);
	if (!context) {
		throw new Error("useAuth must be used inside AuthProvider");
	}
	return context;
}

function AuthDialog({
	open,
	logoSrc,
	onOpenChange,
	onAuthenticated,
}: {
	open: boolean;
	logoSrc: string;
	onOpenChange: (open: boolean) => void;
	onAuthenticated: (login: PendingOrganizationLoginResponse) => Promise<void>;
}) {
	const [phone, setPhone] = useState("");
	const [code, setCode] = useState("");
	const [agreed, setAgreed] = useState(true);
	const [submitting, setSubmitting] = useState(false);
	const [sendingCode, setSendingCode] = useState(false);
	const [countdown, setCountdown] = useState(0);
	const [errorMessage, setErrorMessage] = useState("");
	const [submitted, setSubmitted] = useState(false);
	const [touched, setTouched] = useState<Record<string, boolean>>({});

	useEffect(() => {
		if (!open) return;
		setPhone("");
		setCode("");
		setAgreed(true);
		setSendingCode(false);
		setCountdown(0);
		setSubmitted(false);
		setTouched({});
		setErrorMessage("");
	}, [open]);

	useEffect(() => {
		if (countdown <= 0) return;
		const timer = window.setTimeout(
			() => setCountdown((current) => Math.max(0, current - 1)),
			1000,
		);
		return () => window.clearTimeout(timer);
	}, [countdown]);

	const normalizedPhone = phone.trim();
	const normalizedCode = code.trim();
	const phoneValid = /^1[3-9]\d{9}$/.test(normalizedPhone);
	const codeValid = /^\d{4,8}$/.test(normalizedCode);
	const canSubmit = phoneValid && codeValid && agreed;
	const canSendCode = phoneValid && !sendingCode && countdown === 0;
	const shouldShowError = (field: string) => submitted || Boolean(touched[field]);
	const showPhoneError = shouldShowError("phone") && !phoneValid;
	const showCodeError = shouldShowError("code") && !codeValid;

	const handleFieldBlur = (field: string) => (event: FocusEvent<HTMLInputElement>) => {
		if (!shouldValidateFieldBlur(event)) return;
		setTouched((current) => ({ ...current, [field]: true }));
	};
	const handleOpenPolicyPdf = async (
		event: MouseEvent<HTMLAnchorElement>,
		document: PolicyDocument,
	) => {
		const desktopApi = (window as typeof window & { lerosDesktop?: DesktopPolicyApi }).lerosDesktop;
		if (!desktopApi?.openPolicyPdf) return;

		event.preventDefault();
		await desktopApi.openPolicyPdf(document);
	};

	const handleSendCode = async () => {
		setTouched((current) => ({ ...current, phone: true }));
		if (!canSendCode) return;

		setSendingCode(true);
		setErrorMessage("");
		try {
			const response = await authApi.sendPhoneLoginCode({ phone: normalizedPhone });
			const result = response.data;
			if (result.code !== 0) {
				setErrorMessage(result.message || "验证码发送失败");
				return;
			}
			setCountdown(Math.max(1, Math.floor(result.data.resend_after || 120)));
		} catch (err) {
			console.error("send phone login code error:", err);
			setErrorMessage(getRequestErrorMessage(err) ?? "验证码发送失败，请稍后再试");
		} finally {
			setSendingCode(false);
		}
	};

	const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
		event.preventDefault();
		setSubmitted(true);
		if (!canSubmit || submitting) return;

		setSubmitting(true);
		setErrorMessage("");
		try {
			const response = await authApi.loginByPhoneCode({
				phone: normalizedPhone,
				code: normalizedCode,
			});

			const result = response.data;
			if (result.code !== 0) {
				setErrorMessage(result.message || "登录失败");
				return;
			}

			await onAuthenticated(result.data);
		} catch (err) {
			console.error("login by phone code error:", err);
			setErrorMessage(getRequestErrorMessage(err) ?? "登录失败，请稍后再试");
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
					<img src={logoSrc} alt="Lework" className="size-[60px] object-contain" />
					<DialogTitle className="mt-5 text-center text-3xl font-semibold tracking-normal">
						欢迎来到Lework
					</DialogTitle>
					<DialogDescription className="mt-2 text-center text-sm text-[#8b95a5]">
						手机号验证码登录，首次登录将自动创建账号
					</DialogDescription>

					<form onSubmit={handleSubmit} className="mt-6 flex w-full flex-col gap-3">
						<FieldWithError error={showPhoneError ? "请输入正确的手机号" : undefined}>
							<AuthField icon={<Smartphone className="size-4" />} invalid={showPhoneError}>
								<Input
									type="tel"
									inputMode="numeric"
									value={phone}
									onChange={(event) => setPhone(event.target.value.replace(/\D/g, "").slice(0, 11))}
									onBlur={handleFieldBlur("phone")}
									placeholder="请输入手机号"
									className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
								/>
							</AuthField>
						</FieldWithError>
						<FieldWithError error={showCodeError ? "请输入验证码" : undefined}>
							<AuthField icon={<ShieldCheck className="size-4" />} invalid={showCodeError}>
								<Input
									type="text"
									inputMode="numeric"
									value={code}
									onChange={(event) => setCode(event.target.value.replace(/\D/g, "").slice(0, 8))}
									onBlur={handleFieldBlur("code")}
									placeholder="请输入验证码"
									className="h-[52px] border-0 bg-transparent px-0 text-base text-[#070d1c] shadow-none placeholder:text-[#9aa3b2] focus-visible:ring-0"
								/>
								<button
									type="button"
									onClick={handleSendCode}
									disabled={!canSendCode}
									className="shrink-0 text-sm font-semibold text-[#070d1c] transition-colors hover:text-[#4d5cff] disabled:text-[#b8bfcc]"
								>
									{sendingCode ? "发送中" : countdown > 0 ? `${countdown}s` : "获取验证码"}
								</button>
							</AuthField>
						</FieldWithError>

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
								<a
									href={APP_TERMS_OF_SERVICE_PDF_SRC}
									onClick={(event) => void handleOpenPolicyPdf(event, "terms")}
									target="_blank"
									rel="noreferrer"
									className="mx-1 text-[#64748b] transition-colors hover:text-[#4d5cff]"
								>
									《服务条款》
								</a>
								和
								<a
									href={APP_PRIVACY_POLICY_PDF_SRC}
									onClick={(event) => void handleOpenPolicyPdf(event, "privacy")}
									target="_blank"
									rel="noreferrer"
									className="mx-1 text-[#64748b] transition-colors hover:text-[#4d5cff]"
								>
									《隐私政策》
								</a>
							</span>
						</div>

						<Button
							type="submit"
							disabled={submitting}
							className={cn(
								"mt-2 h-[52px] rounded-[16px] bg-[#070d1c] text-base font-semibold text-white hover:bg-[#182033] disabled:bg-[#d2d5de] disabled:text-white",
								!canSubmit && !submitting && "bg-[#d2d5de] hover:bg-[#d2d5de]",
							)}
						>
							{submitting ? "登录中..." : "登录 / 注册"}
						</Button>
					</form>
				</div>
			</DialogContent>
		</Dialog>
	);
}

function shouldValidateFieldBlur(event: FocusEvent<HTMLInputElement>): boolean {
	const relatedTarget = event.relatedTarget;
	if (!(relatedTarget instanceof HTMLElement)) return false;

	const dialogContent = event.currentTarget.closest('[data-slot="dialog-content"]');
	if (!dialogContent?.contains(relatedTarget)) return false;
	if (relatedTarget.closest('[data-slot="dialog-close"]')) return false;

	return true;
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
			<span className="inline-flex size-4 shrink-0 items-center justify-center overflow-visible">
				{icon}
			</span>
			{children}
		</div>
	);
}
