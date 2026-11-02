"use client";

import type { ComposerToken } from "@lework/store/types/chat";
import { useSyncExternalStore } from "react";
import type { FilePreviewItem } from "./file-preview-utils";
import type { OfficeTextSelection } from "./office-selection";

export type DocxSelectionComposerDraft = {
	id: string;
	referenceId: string;
	referenceLabel: string;
	file: FilePreviewItem;
	selection: OfficeTextSelection;
	suggestedPrompt?: string;
	selectedVersionPublicId: string;
};

export type PendingDocxVersionSync = {
	id: string;
	projectId: string;
	taskId?: string;
	chainFilePublicId: string;
	expectedPreviewPublicId: string;
	baselinePublicId: string;
	baselineVersionNo: number;
	selectedVersionPublicId: string;
};

type DocxSelectionComposerState = {
	draft: DocxSelectionComposerDraft | null;
	submission: PendingDocxVersionSync | null;
};

let sequence = 0;
let state: DocxSelectionComposerState = { draft: null, submission: null };
const listeners = new Set<() => void>();

function emit(nextState: DocxSelectionComposerState) {
	state = nextState;
	for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function useDocxSelectionComposerStore(): DocxSelectionComposerState {
	return useSyncExternalStore(
		subscribe,
		() => state,
		() => state,
	);
}

export const docxSelectionComposerActions = {
	setDraft(input: {
		file: FilePreviewItem;
		selection: OfficeTextSelection;
		suggestedPrompt?: string;
		selectedVersionPublicId?: string;
	}) {
		sequence += 1;
		const id = `docx-selection-draft-${Date.now()}-${sequence}`;
		const referenceId = input.file.versionPublicId?.trim() || input.file.publicId?.trim() || id;
		emit({
			...state,
			draft: {
				id,
				referenceId,
				referenceLabel: buildDocxSelectionReferenceLabel(input.selection.text),
				file: input.file,
				selection: input.selection,
				suggestedPrompt: input.suggestedPrompt,
				selectedVersionPublicId: input.selectedVersionPublicId ?? "",
			},
		});
	},
	clearDraft(expectedId?: string) {
		if (expectedId && state.draft?.id !== expectedId) return;
		emit({ ...state, draft: null });
	},
	markSubmitted(submission: PendingDocxVersionSync | null) {
		emit({ ...state, submission });
	},
	clearSubmission(expectedId?: string) {
		if (expectedId && state.submission?.id !== expectedId) return;
		emit({ ...state, submission: null });
	},
};

export function buildDocxSelectionReferenceLabel(text: string): string {
	const normalized = text.trim().replace(/\s+/g, " ");
	if (!normalized) return "文档选区";
	return normalized.length > 24 ? `${normalized.slice(0, 24)}…` : normalized;
}

export function applyDocxSelectionDraftToComposer({
	value,
	tokens,
	draft,
	previousSuggestedPrompt,
}: {
	value: string;
	tokens: ComposerToken[];
	draft: DocxSelectionComposerDraft;
	previousSuggestedPrompt?: string;
}): { value: string; tokens: ComposerToken[] } {
	let snapshot = { value, tokens: normalizeTokens(value, tokens) };
	const expectedReferenceStart = draft.suggestedPrompt ? draft.suggestedPrompt.length + 1 : 0;
	const expectedReferenceEnd = expectedReferenceStart + draft.referenceLabel.length;
	if (
		!snapshot.tokens.some((token) => token.kind === "reference") &&
		value.slice(expectedReferenceStart, expectedReferenceEnd) === draft.referenceLabel &&
		(expectedReferenceEnd === value.length || /\s/.test(value[expectedReferenceEnd] ?? ""))
	) {
		const restoredReference: ComposerToken = {
			kind: "reference",
			id: draft.referenceId,
			label: draft.referenceLabel,
			start: expectedReferenceStart,
			end: expectedReferenceEnd,
		};
		return {
			value,
			tokens: [
				restoredReference,
				...snapshot.tokens.filter(
					(token) => token.end <= expectedReferenceStart || token.start >= expectedReferenceEnd,
				),
			].sort((left, right) => left.start - right.start),
		};
	}
	for (const token of [...snapshot.tokens].reverse()) {
		if (token.kind === "reference") snapshot = removeToken(snapshot, token);
	}
	snapshot = trimSnapshot(snapshot);

	if (
		previousSuggestedPrompt &&
		snapshot.value.startsWith(previousSuggestedPrompt) &&
		(snapshot.value.length === previousSuggestedPrompt.length ||
			/\s/.test(snapshot.value[previousSuggestedPrompt.length] ?? ""))
	) {
		snapshot = removeRange(snapshot, 0, previousSuggestedPrompt.length);
		snapshot = trimSnapshot(snapshot);
	}

	const promptPrefix = draft.suggestedPrompt ? `${draft.suggestedPrompt} ` : "";
	const remainingSuffix = snapshot.value ? ` ${snapshot.value}` : "";
	const referenceStart = promptPrefix.length;
	const valueWithReference = `${promptPrefix}${draft.referenceLabel}${remainingSuffix}`;
	const shiftedTokens = snapshot.tokens.map((token) => ({
		...token,
		start: token.start + promptPrefix.length + draft.referenceLabel.length + 1,
		end: token.end + promptPrefix.length + draft.referenceLabel.length + 1,
	}));
	return {
		value: valueWithReference,
		tokens: [
			{
				kind: "reference",
				id: draft.referenceId,
				label: draft.referenceLabel,
				start: referenceStart,
				end: referenceStart + draft.referenceLabel.length,
			},
			...shiftedTokens,
		],
	};
}

export function removeDocxReferenceFromComposer(
	value: string,
	tokens: ComposerToken[],
): { value: string; tokens: ComposerToken[] } {
	let snapshot = { value, tokens: normalizeTokens(value, tokens) };
	for (const token of [...snapshot.tokens].reverse()) {
		if (token.kind === "reference") snapshot = removeToken(snapshot, token);
	}
	return trimSnapshot(snapshot);
}

function normalizeTokens(value: string, tokens: ComposerToken[]): ComposerToken[] {
	return [...tokens]
		.filter((token) => value.slice(token.start, token.end) === token.label)
		.sort((left, right) => left.start - right.start);
}

function removeToken(snapshot: { value: string; tokens: ComposerToken[] }, token: ComposerToken) {
	let start = token.start;
	let end = token.end;
	if (snapshot.value[end] === " ") end += 1;
	else if (start > 0 && snapshot.value[start - 1] === " ") start -= 1;
	return removeRange(snapshot, start, end);
}

function removeRange(
	snapshot: { value: string; tokens: ComposerToken[] },
	start: number,
	end: number,
) {
	const nextValue = `${snapshot.value.slice(0, start)}${snapshot.value.slice(end)}`;
	const delta = start - end;
	const nextTokens = snapshot.tokens.flatMap((token) => {
		if (token.start >= start && token.end <= end) return [];
		if (token.start >= end) {
			return [{ ...token, start: token.start + delta, end: token.end + delta }];
		}
		return token.end <= start ? [token] : [];
	});
	return { value: nextValue, tokens: normalizeTokens(nextValue, nextTokens) };
}

function trimSnapshot(snapshot: { value: string; tokens: ComposerToken[] }) {
	const leading = snapshot.value.length - snapshot.value.trimStart().length;
	const value = snapshot.value.trim();
	const tokens = snapshot.tokens
		.map((token) => ({ ...token, start: token.start - leading, end: token.end - leading }))
		.filter((token) => token.start >= 0 && value.slice(token.start, token.end) === token.label);
	return { value, tokens };
}
