/**
 * 发送管道公共出口。
 * ChatActionImpl 从这里装配；UI 仍只调 chatSlice 上的同名方法。
 */

export type { BootstrapNewTaskOptions } from "./bootstrap";
export { bootstrapNewTaskSession } from "./bootstrap";
export {
	formatTaskDisplayTitle,
	hasComposerSkillTokens,
	parseSkillChips,
	prepareOutgoingComposer,
	skillChipMarkup,
	skillChipsToComposerState,
	skillChipsToPlainText,
	skillCodeFromToken,
} from "./composerSkills";
export type { ParsedSkillChip } from "./composerSkills";
export type { SendPipelineDeps } from "./deps";
export {
	buildBackendMessageMetadata,
	extractAssistantIdsFromMetadata,
} from "./metadata";
export { createOptimisticUserMessage, createWaitingAssistantMessage } from "./optimistic";
export type { SendProjectMessageOptions } from "./sendProjectMessage";
export { sendProjectMessage } from "./sendProjectMessage";
export type { SendTaskRoomParams, SendTaskRoomResult } from "./sendTaskRoomMessage";
export { sendTaskRoomMessage } from "./sendTaskRoomMessage";
