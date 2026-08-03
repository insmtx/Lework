import { apiClient } from "./client";
import type { BackendDataResponse } from "./types";

export type ProjectActivityActor = {
	id: string;
	name?: string;
	avatar_url?: string;
};

export type ProjectActivitySkill = {
	id: string;
	name?: string;
	icon?: string;
};

export type ProjectActivityPayload = {
	added_skills: ProjectActivitySkill[];
	removed_skills: ProjectActivitySkill[];
	added_mcps?: ProjectActivitySkill[];
	removed_mcps?: ProjectActivitySkill[];
	added_members: ProjectActivityActor[];
	removed_members: ProjectActivityActor[];
	added_ai_teammates: ProjectActivityActor[];
	removed_ai_teammates: ProjectActivityActor[];
};

export type ProjectActivityItem = {
	id: number;
	project_id: string;
	operator_id: string;
	operator?: ProjectActivityActor | null;
	action_type: string;
	payload: ProjectActivityPayload;
	created_at: string;
};

export type ListProjectActivitiesParams = {
	project_id?: string;
	operator_id?: string;
	operator_ids?: string[];
	cursor?: string;
	limit?: number;
};

export type ProjectActivityListData = {
	items: ProjectActivityItem[];
	next_cursor?: string;
};

const ENDPOINT = "/ListProjectActivities";

const listProjectActivitiesInflight = new Map<
	string,
	ReturnType<typeof apiClient.post<BackendDataResponse<ProjectActivityListData>>>
>();

function getListProjectActivitiesKey(params: ListProjectActivitiesParams): string {
	const operatorIds = params.operator_ids?.slice().sort().join(",") ?? params.operator_id ?? "";
	return [
		params.project_id ?? "",
		operatorIds,
		params.cursor ?? "",
		String(params.limit ?? ""),
	].join(":");
}

export const projectActivityApi = {
	list: (params: ListProjectActivitiesParams = {}) => {
		const key = getListProjectActivitiesKey(params);
		const inflight = listProjectActivitiesInflight.get(key);
		if (inflight) return inflight;

		const promise = apiClient
			.post<BackendDataResponse<ProjectActivityListData>>(ENDPOINT, params)
			.finally(() => {
				listProjectActivitiesInflight.delete(key);
			});
		listProjectActivitiesInflight.set(key, promise);
		return promise;
	},
};
