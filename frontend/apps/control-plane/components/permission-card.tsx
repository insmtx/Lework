"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Loader2, ShieldAlert } from "lucide-react";
import { useState } from "react";
import type { PermissionRequest } from "../lib/types";

type PermissionDecision = "once" | "always" | "reject";

interface PermissionCardProps {
	permission: PermissionRequest;
	/** 节点离线时按钮不可点，避免把必然失败的请求发出去。 */
	disabled?: boolean;
	onResolve: (permission: PermissionRequest, decision: PermissionDecision) => Promise<void>;
}

export function PermissionCard({ permission, disabled, onResolve }: PermissionCardProps) {
	const [busy, setBusy] = useState<PermissionDecision | null>(null);
	const [error, setError] = useState<string | null>(null);

	const resolve = async (decision: PermissionDecision) => {
		setBusy(decision);
		setError(null);
		try {
			await onResolve(permission, decision);
		} catch (caught) {
			setError(caught instanceof Error ? caught.message : "Unable to reply");
		} finally {
			setBusy(null);
		}
	};

	return (
		<div className="rounded-xl border border-amber-300/70 bg-amber-50 p-3 text-sm shadow-sm dark:border-amber-900/60 dark:bg-amber-950/30">
			<div className="flex items-start gap-2">
				<ShieldAlert className="mt-0.5 size-4 shrink-0 text-amber-700 dark:text-amber-400" />
				<div className="min-w-0 flex-1">
					<div className="font-medium text-amber-950 dark:text-amber-100">
						Permission required: <span className="font-mono">{permission.action}</span>
					</div>
					{permission.message ? (
						<p className="mt-1 text-xs text-amber-900/80 dark:text-amber-200/80">
							{permission.message}
						</p>
					) : null}
					{permission.resources?.length ? (
						<div className="mt-2 flex flex-wrap gap-1">
							{permission.resources.slice(0, 6).map((resource) => (
								<code
									key={resource}
									className="max-w-full truncate rounded bg-amber-100 px-1.5 py-0.5 font-mono text-[10px] text-amber-950 dark:bg-amber-900/60 dark:text-amber-100"
								>
									{resource}
								</code>
							))}
						</div>
					) : null}
					<div className="mt-3 flex flex-wrap gap-2">
						<Button
							size="sm"
							onClick={() => void resolve("once")}
							disabled={disabled || busy !== null}
						>
							{busy === "once" ? <Loader2 className="size-3.5 animate-spin" /> : null}
							Allow once
						</Button>
						<Button
							size="sm"
							variant="secondary"
							onClick={() => void resolve("always")}
							disabled={disabled || busy !== null}
						>
							{busy === "always" ? <Loader2 className="size-3.5 animate-spin" /> : null}
							Always allow
						</Button>
						<Button
							size="sm"
							variant="outline"
							onClick={() => void resolve("reject")}
							disabled={disabled || busy !== null}
						>
							{busy === "reject" ? <Loader2 className="size-3.5 animate-spin" /> : null}
							Reject
						</Button>
					</div>
					<p className="mt-2 text-[11px] text-amber-900/70 dark:text-amber-200/70">
						Always allow saves a project-level rule. Reject also declines other pending requests in
						this session.
					</p>
					{error ? <p className="mt-2 text-xs text-destructive">{error}</p> : null}
				</div>
			</div>
		</div>
	);
}
