"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Loader2, ShieldAlert } from "lucide-react";
import { useState } from "react";
import type { PermissionRequest } from "../lib/types";

interface PermissionCardProps {
	permission: PermissionRequest;
	onResolve: (permission: PermissionRequest, decision: "once" | "reject") => Promise<void>;
}

export function PermissionCard({ permission, onResolve }: PermissionCardProps) {
	const [busy, setBusy] = useState<"once" | "reject" | null>(null);
	const [error, setError] = useState<string | null>(null);

	const resolve = async (decision: "once" | "reject") => {
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
					<div className="mt-3 flex gap-2">
						<Button size="sm" onClick={() => void resolve("once")} disabled={busy !== null}>
							{busy === "once" ? <Loader2 className="size-3.5 animate-spin" /> : null}
							Allow once
						</Button>
						<Button
							size="sm"
							variant="outline"
							onClick={() => void resolve("reject")}
							disabled={busy !== null}
						>
							{busy === "reject" ? <Loader2 className="size-3.5 animate-spin" /> : null}
							Reject
						</Button>
					</div>
					{error ? <p className="mt-2 text-xs text-destructive">{error}</p> : null}
				</div>
			</div>
		</div>
	);
}
