import { automationApi } from "../api/automationApi";
import type {
	BackendAutomation,
	BackendAutomationExecution,
	BackendAutomationScheduleFormConfig,
} from "../api/types";
import type { SliceCreator } from "../types";
import { flattenActions } from "../utils";
import { readStoredAuthUser } from "../utils/authStorage";

export type AutomationItem = {
	publicId: string;
	name: string;
	instruction?: string;
	enabled: boolean;
	scheduleMode: string;
	formConfig?: BackendAutomationScheduleFormConfig;
	scheduleSpec?: BackendAutomation["schedule_spec"];
	timezone: string;
	assistantId: number;
	nextRunAt?: string;
	summary: string;
	hasActiveExecution: boolean;
	lastExecutionStatus?: string;
	lastExecutionTime?: string;
	lastExecutionPublicId?: string;
	lastTaskId?: number;
	projectId?: number;
	createdAt: number;
	updatedAt: number;
};

export type AutomationState = {
	automations: AutomationItem[];
	loaded: boolean;
	loading: boolean;
	error: string | null;
	// 执行历史（阶段三）
	executions: AutomationExecutionItem[];
	executionsLoading: boolean;
	executionsError: string | null;
};

export type AutomationExecutionItem = {
	publicId: string;
	automationId: number;
	orgId: number;
	triggerType: string;
	status: string;
	scheduledAt: string;
	notAfter?: string;
	startedAt?: string;
	finishedAt?: string;
	nameSnapshot: string;
	instructionSnapshot?: string;
	missedCount: number;
	projectId?: number;
	taskId?: number;
	sessionId?: number;
	messageId?: number;
	projectPublicId?: string;
	taskPublicId?: string;
	sessionPublicId?: string;
	messagePublicId?: string;
	runId?: string;
	attemptCount: number;
	errorCode?: string;
	errorMsg?: string;
	createdAt: string;
};

export type AutomationAction = Pick<AutomationSliceImpl, keyof AutomationSliceImpl>;
export type AutomationStore = AutomationState & AutomationAction;

function mapBackendExecution(e: BackendAutomationExecution): AutomationExecutionItem {
	return {
		publicId: e.public_id,
		automationId: e.automation_id,
		orgId: e.org_id,
		triggerType: e.trigger_type,
		status: e.status,
		scheduledAt: e.scheduled_at,
		notAfter: e.not_after,
		startedAt: e.started_at,
		finishedAt: e.finished_at,
		nameSnapshot: e.name_snapshot,
		instructionSnapshot: e.instruction_snapshot,
		missedCount: e.missed_count,
		projectId: e.project_id,
		taskId: e.task_id,
		sessionId: e.session_id,
		messageId: e.message_id,
		projectPublicId: e.project_public_id,
		taskPublicId: e.task_public_id,
		sessionPublicId: e.session_public_id,
		messagePublicId: e.message_public_id,
		runId: e.run_id,
		attemptCount: e.attempt_count,
		errorCode: e.error_code,
		errorMsg: e.error_msg,
		createdAt: e.created_at,
	};
}

function mapBackendAutomation(a: BackendAutomation): AutomationItem {
	return {
		publicId: a.public_id,
		name: a.name,
		instruction: a.instruction ?? "",
		enabled: a.enabled,
		scheduleMode: a.schedule_mode,
		formConfig: a.schedule_spec?.form_config,
		scheduleSpec: a.schedule_spec,
		timezone: a.timezone,
		assistantId: a.assistant_id,
		nextRunAt: a.next_run_at,
		summary: a.summary ?? "",
		hasActiveExecution: a.has_active_execution ?? false,
		lastExecutionStatus: a.last_execution_status,
		lastExecutionTime: a.last_execution_time,
		lastExecutionPublicId: a.last_execution_public_id,
		lastTaskId: a.last_task_id,
		projectId: a.project_id,
		createdAt: new Date(a.created_at).getTime(),
		updatedAt: new Date(a.updated_at).getTime(),
	};
}

const _initialState: AutomationState = {
	automations: [],
	loaded: false,
	loading: false,
	error: null,
	executions: [],
	executionsLoading: false,
	executionsError: null,
};

type SetState = (
	partial:
		| AutomationStore
		| Partial<AutomationStore>
		| ((state: AutomationStore) => AutomationStore | Partial<AutomationStore>),
	replace?: boolean,
) => void;

export const createAutomationSlice = (set: SetState) => new AutomationSliceImpl(set);

export class AutomationSliceImpl {
	readonly #set: SetState;
	#fetchPromise: Promise<void> | null = null;
	#fetchEpoch = 0;

	constructor(set: SetState) {
		this.#set = set;
	}

	fetchAutomations = async (): Promise<void> => {
		if (!readStoredAuthUser()?.jwtToken) return;
		const epoch = this.#fetchEpoch;
		if (!this.#fetchPromise) {
			this.#fetchPromise = (async () => {
				this.#set({ loading: true });
				try {
					const res = await automationApi.list({ limit: 100 });
					if (epoch !== this.#fetchEpoch) return;
					const items = res.data.data?.items ?? [];
					this.#set({
						automations: items.map(mapBackendAutomation),
						loaded: true,
						loading: false,
						error: null,
					});
				} catch (err) {
					console.error("fetchAutomations error:", err);
					if (epoch === this.#fetchEpoch) {
						this.#set({ loading: false, error: "加载自动化列表失败" });
					}
				} finally {
					if (epoch === this.#fetchEpoch) {
						this.#fetchPromise = null;
					}
				}
			})();
		}
		return this.#fetchPromise;
	};

	refreshAutomations = async (): Promise<void> => {
		this.#fetchPromise = null;
		await this.fetchAutomations();
	};

	createAutomation = async (params: {
		name: string;
		instruction?: string;
		enabled?: boolean;
		schedule_mode: string;
		schedule: BackendAutomationScheduleFormConfig;
		timezone?: string;
	}): Promise<AutomationItem | null> => {
		try {
			const res = await automationApi.create(params);
			const data = res.data.data;
			if (!data) throw new Error("No data returned");
			const item = mapBackendAutomation(data);
			this.#set((state) => ({
				automations: [item, ...state.automations],
				loaded: true,
				error: null,
			}));
			return item;
		} catch (err) {
			console.error("createAutomation error:", err);
			return null;
		}
	};

	updateAutomation = async (
		publicId: string,
		params: {
			name?: string;
			instruction?: string;
			enabled?: boolean;
			schedule_mode?: string;
			schedule?: BackendAutomationScheduleFormConfig;
			timezone?: string;
		},
	): Promise<AutomationItem | null> => {
		try {
			const res = await automationApi.update({ public_id: publicId, ...params });
			const data = res.data.data;
			if (!data) throw new Error("No data returned");
			const item = mapBackendAutomation(data);
			this.#set((state) => ({
				automations: state.automations.map((a) => (a.publicId === item.publicId ? item : a)),
				error: null,
			}));
			return item;
		} catch (err) {
			console.error("updateAutomation error:", err);
			return null;
		}
	};

	toggleAutomation = async (publicId: string, enabled: boolean): Promise<boolean> => {
		// 乐观更新
		this.#set((state) => ({
			automations: state.automations.map((a) => (a.publicId === publicId ? { ...a, enabled } : a)),
		}));
		try {
			const res = await automationApi.update({ public_id: publicId, enabled });
			const data = res.data.data;
			if (!data) throw new Error("No data returned");
			this.#set((state) => ({
				automations: state.automations.map((a) =>
					a.publicId === publicId ? mapBackendAutomation(data) : a,
				),
			}));
			return true;
		} catch (err) {
			// 失败回滚
			console.error("toggleAutomation error:", err);
			this.#set((state) => ({
				automations: state.automations.map((a) =>
					a.publicId === publicId ? { ...a, enabled: !enabled } : a,
				),
			}));
			return false;
		}
	};

	deleteAutomation = async (publicId: string): Promise<boolean> => {
		try {
			await automationApi.delete({ public_id: publicId });
			this.#set((state) => ({
				automations: state.automations.filter((a) => a.publicId !== publicId),
				error: null,
			}));
			return true;
		} catch (err) {
			console.error("deleteAutomation error:", err);
			return false;
		}
	};

	/** 立即运行；有活动执行时返回 "conflict"。 */
	runAutomationNow = async (publicId: string): Promise<"ok" | "conflict" | "error"> => {
		try {
			await automationApi.runNow({ public_id: publicId });
			return "ok";
		} catch (err) {
			const status = (err as { response?: { status?: number } })?.response?.status;
			if (status === 409) return "conflict";
			console.error("runAutomationNow error:", err);
			return "error";
		}
	};

	/** 获取某个自动化的执行历史列表。 */
	fetchExecutions = async (
		automationPublicId: string,
		status?: string,
		offset = 0,
		limit = 50,
	): Promise<void> => {
		if (!readStoredAuthUser()?.jwtToken) return;
		this.#set({ executionsLoading: true });
		try {
			const res = await automationApi.listExecutions({
				public_id: automationPublicId,
				status,
				offset,
				limit,
			});
			const items = res.data.data?.items ?? [];
			this.#set({
				executions: items.map(mapBackendExecution),
				executionsLoading: false,
				executionsError: null,
			});
		} catch (err) {
			console.error("fetchExecutions error:", err);
			this.#set({ executionsLoading: false, executionsError: "加载执行历史失败" });
		}
	};

	/** 获取单条执行详情。 */
	fetchExecutionDetail = async (
		executionPublicId: string,
	): Promise<AutomationExecutionItem | null> => {
		if (!readStoredAuthUser()?.jwtToken) return null;
		try {
			const res = await automationApi.getExecution({ public_id: executionPublicId });
			const data = res.data.data;
			if (!data) return null;
			return mapBackendExecution(data);
		} catch (err) {
			console.error("fetchExecutionDetail error:", err);
			return null;
		}
	};

	resetAuthScopedData = () => {
		this.#fetchEpoch += 1;
		this.#fetchPromise = null;
		this.#set(_initialState);
	};
}

export const automationSlice: SliceCreator<AutomationStore> = (...params) => ({
	..._initialState,
	...flattenActions<AutomationAction>([createAutomationSlice(params[0] as SetState)]),
});
