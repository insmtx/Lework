import { devtools, subscribeWithSelector } from "zustand/middleware";
import { createWithEqualityFn } from "zustand/traditional";
import { type AuthAction, type AuthStore, authSlice } from "./slices/authSlice";
import {
	type AutomationAction,
	type AutomationStore,
	automationSlice,
} from "./slices/automationSlice";
import { type ChatAction, type ChatStore, chatSlice } from "./slices/chatSlice";
import { type DAStore, type DigitalAssistantAction, daSlice } from "./slices/digitalAssistantSlice";
import {
	type GlobalConfigAction,
	type GlobalConfigStore,
	globalConfigSlice,
} from "./slices/globalConfigSlice";
import { type LayoutAction, type LayoutStore, layoutSlice } from "./slices/layoutSlice";
import { type ModelAction, type ModelStore, modelSlice } from "./slices/modelSlice";
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
	GlobalConfigStore &
	AutomationStore &
	ModelStore;
export type AppAction = AuthAction &
	LayoutAction &
	TopicAction &
	ChatAction &
	DigitalAssistantAction &
	PermissionAction &
	GlobalConfigAction &
	AutomationAction &
	ModelAction;

const createStore: SliceCreator<AppStore> = (...params) => {
	const layout = layoutSlice(...params);
	const da = daSlice(...params);
	return {
		...authSlice(...params),
		...layout,
		...topicSlice(...params),
		...chatSlice(...params),
		...da,
		...permissionSlice(...params),
		...globalConfigSlice(...params),
		...automationSlice(...params),
		...modelSlice(...params),
		// 中文注释：layout 与 DA 都导出了 resetAuthScopedData，对象展开时后者会盖掉前者，
		// 导致登出只清助手、不清 projects。这里合并成一次调用，两个入口都生效。
		resetAuthScopedData: () => {
			layout.resetAuthScopedData();
			da.resetAuthScopedData();
			automationSlice(...params).resetAuthScopedData();
			modelSlice(...params).resetAuthScopedData();
		},
	};
};

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

export const useAutomationStore = <T>(
	selector: (state: AutomationStore & AutomationAction) => T,
): T => useAppStore(selector);

export const useModelStore = <T>(selector: (state: ModelStore & ModelAction) => T): T =>
	useAppStore(selector);
