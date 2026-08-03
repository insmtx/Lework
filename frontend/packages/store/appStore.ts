import { devtools, subscribeWithSelector } from "zustand/middleware";
import { createWithEqualityFn } from "zustand/traditional";
import { type AuthAction, type AuthStore, authSlice } from "./slices/authSlice";
import { type ChatAction, type ChatStore, chatSlice } from "./slices/chatSlice";
import { type DAStore, type DigitalAssistantAction, daSlice } from "./slices/digitalAssistantSlice";
import {
	type GlobalConfigAction,
	type GlobalConfigStore,
	globalConfigSlice,
} from "./slices/globalConfigSlice";
import { type LayoutAction, type LayoutStore, layoutSlice } from "./slices/layoutSlice";
import {
	type PermissionAction,
	type PermissionStore,
	permissionSlice,
} from "./slices/permissionSlice";
import { type TopicAction, type TopicStore, topicSlice } from "./slices/topicSlice";
import type { SliceCreator } from "./types";

export type AppStore = AuthStore &
	LayoutStore &
	TopicStore &
	ChatStore &
	DAStore &
	PermissionStore &
	GlobalConfigStore;
export type AppAction = AuthAction &
	LayoutAction &
	TopicAction &
	ChatAction &
	DigitalAssistantAction &
	PermissionAction &
	GlobalConfigAction;

const createStore: SliceCreator<AppStore> = (...params) => ({
	...authSlice(...params),
	...layoutSlice(...params),
	...topicSlice(...params),
	...chatSlice(...params),
	...daSlice(...params),
	...permissionSlice(...params),
	...globalConfigSlice(...params),
});

export const useAppStore = createWithEqualityFn<AppStore>()(
	subscribeWithSelector(devtools(createStore)),
	Object.is,
);

export const usePermissionStore = <T>(
	selector: (state: PermissionStore & PermissionAction) => T,
): T => useAppStore(selector);

export const useAuthStore = <T>(selector: (state: AuthStore & AuthAction) => T): T =>
	useAppStore(selector);

export const useLayoutStore = <T>(selector: (state: LayoutStore & LayoutAction) => T): T =>
	useAppStore(selector);

export const useTopicStore = <T>(selector: (state: TopicStore & TopicAction) => T): T =>
	useAppStore(selector);

export const useChatStore = <T>(selector: (state: ChatStore & ChatAction) => T): T =>
	useAppStore(selector);

export const useDAStore = <T>(selector: (state: DAStore & DigitalAssistantAction) => T): T =>
	useAppStore(selector);

export const useGlobalConfigStore = <T>(
	selector: (state: GlobalConfigStore & GlobalConfigAction) => T,
): T => useAppStore(selector);
