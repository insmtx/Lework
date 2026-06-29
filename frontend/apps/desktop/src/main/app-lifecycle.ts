let appQuitting = false;

export function isAppQuitting(): boolean {
	return appQuitting;
}

export function markAppQuitting(): void {
	appQuitting = true;
}
