"use client";

import type { FocusEvent, ReactNode } from "react";
import { useCallback, useState } from "react";

export function RequiredMark() {
	return (
		<span aria-hidden="true" className="ml-0.5 text-red-500">
			*
		</span>
	);
}

export function FieldWithError({ children, error }: { children: ReactNode; error?: string }) {
	return (
		<div className="space-y-1">
			{children}
			{error && <div className="px-1 text-xs font-medium text-red-500">{error}</div>}
		</div>
	);
}

export function useFormFieldValidation() {
	const [submitted, setSubmitted] = useState(false);
	const [touched, setTouched] = useState<Record<string, boolean>>({});

	const resetValidation = useCallback(() => {
		setSubmitted(false);
		setTouched({});
	}, []);

	const shouldShowError = useCallback(
		(field: string) => submitted || Boolean(touched[field]),
		[submitted, touched],
	);

	const touchField = useCallback((field: string) => {
		setTouched((current) => ({ ...current, [field]: true }));
	}, []);

	const markSubmitted = useCallback(() => {
		setSubmitted(true);
	}, []);

	const handleFieldBlur = useCallback(
		(field: string) => (event: FocusEvent<HTMLElement>) => {
			if (!shouldValidateFieldBlur(event)) return;
			touchField(field);
		},
		[touchField],
	);

	return {
		resetValidation,
		shouldShowError,
		touchField,
		markSubmitted,
		handleFieldBlur,
	};
}

export function shouldValidateFieldBlur(event: FocusEvent<HTMLElement>): boolean {
	const relatedTarget = event.relatedTarget;
	if (!(relatedTarget instanceof HTMLElement)) return false;

	const dialogContent = event.currentTarget.closest('[data-slot="dialog-content"]');
	if (!dialogContent?.contains(relatedTarget)) return false;
	if (relatedTarget.closest('[data-slot="dialog-close"]')) return false;

	return true;
}

export const PHONE_PATTERN = /^1[3-9]\d{9}$/;

export function isValidPhone(value: string) {
	return PHONE_PATTERN.test(value.trim());
}
