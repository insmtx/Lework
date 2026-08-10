/**
 * 对话输入框（composer）草稿与附件上传。
 *
 * 可以做：维护 inputText / inputAttachments / executionMode / 焦点与模型选择；
 * 本地附件与项目文件/文件夹上传、skill 指令插入、清空草稿并释放 blob URL。
 * 不可以做：发消息、开 SSE、改 messagesMap / isGenerating。
 */
import { projectFileApi } from "../api/projectFileApi";
import {
	buildComposerFolderUploadSummaryMessage,
	COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE,
	COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE,
	isComposerUploadAllowedFile,
	isEmptyUploadFile,
	partitionComposerFolderFiles,
} from "../constants/composer-upload";
import {
	FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE,
	getFileRelativePath,
	getFolderNameFromFiles,
	isFolderUploadSizeExceeded,
} from "../constants/upload";
import type { Attachment, ExecutionMode } from "../types/chat";
import { revokeAttachmentObjectUrls } from "../utils/messageAttachments";
import type { ChatState } from "./state";

/**
 * Composer 所需的 store 读写依赖。
 */
export type ComposerDeps = {
	/** 读取当前对话状态（取 inputAttachments 等） */
	get: () => ChatState;
	/** 部分更新 ChatState 中的 composer 字段 */
	set: (partial: Partial<ChatState> | ((state: ChatState) => Partial<ChatState>)) => void;
};

/** 转义正则特殊字符，供 skill 指令 token 匹配使用。 */
function escapeRegExp(value: string): string {
	return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** 规范化 skill 名：去空白并去掉前导 `/`。 */
function normalizeSkillDirectiveName(skillName: string): string {
	return skillName.trim().replace(/^\/+/, "");
}

/**
 * 在输入框文本前追加 `/{skill}` 指令；已存在则只保证末尾有空格，避免重复插入。
 */
function appendSkillDirectiveToInput(inputText: string, skillName: string): string {
	const normalizedName = normalizeSkillDirectiveName(skillName);
	if (!normalizedName) return inputText;

	const token = `/${normalizedName}`;
	const tokenPattern = new RegExp(`(^|\\s)${escapeRegExp(token)}(?=\\s|$)`);
	const trimmed = inputText.trimStart();
	if (tokenPattern.test(trimmed)) {
		return trimmed.endsWith(" ") ? trimmed : `${trimmed} `;
	}

	const directivePrefixMatch = trimmed.match(/^((?:\/[^\s/]+\s+)*)/);
	const directivePrefix = directivePrefixMatch?.[0] ?? "";
	const rest = trimmed.slice(directivePrefix.length);
	const nextDirectivePrefix = directivePrefix
		? `${directivePrefix.trimEnd()} ${token} `
		: `${token} `;

	return `${nextDirectivePrefix}${rest}`;
}

/**
 * 用单个 `/{skill}` 替换输入框开头的全部 skill 指令前缀（切换 skill 时用）。
 */
function replaceSkillDirectiveInInput(inputText: string, skillName: string): string {
	const normalizedName = normalizeSkillDirectiveName(skillName);
	if (!normalizedName) return inputText;

	const token = `/${normalizedName}`;
	const trimmed = inputText.trimStart();
	const rest = trimmed.replace(/^(?:\/[^\s/]+\s+)*/, "");
	return rest ? `${token} ${rest}` : `${token} `;
}

/**
 * 管理输入草稿、附件上传与 skill 指令，不触碰对话流状态机。
 */
export class Composer {
	readonly #deps: ComposerDeps;
	/** 进行中的上传 AbortController，按 attachmentId 索引，供取消/移除时 abort。 */
	#uploadAbortControllers = new Map<string, AbortController>();

	constructor(deps: ComposerDeps) {
		this.#deps = deps;
	}

	/** 更新输入框文本。 */
	setInputText = (text: string) => {
		this.#deps.set({ inputText: text });
	};

	/** 切换执行模式（default / plan），供发送时写入 execution_mode。 */
	setExecutionMode = (executionMode: ExecutionMode) => {
		this.#deps.set({ executionMode });
	};

	/** 清空草稿与附件，并释放本地 blob URL。 */
	clearComposerInput = () => {
		const state = this.#deps.get();
		revokeAttachmentObjectUrls(state.inputAttachments);
		this.#deps.set({ inputText: "", inputAttachments: [] });
	};

	/** 在输入框追加 `/{skill}` 指令（已存在则不重复）。 */
	appendSkillDirective = (skillName: string) => {
		this.#deps.set((state) => ({
			inputText: appendSkillDirectiveToInput(state.inputText, skillName),
		}));
	};

	/** 用单个 skill 指令替换输入框开头的全部 skill 前缀。 */
	replaceSkillDirective = (skillName: string) => {
		this.#deps.set((state) => ({
			inputText: replaceSkillDirectiveInInput(state.inputText, skillName),
		}));
	};

	/** 添加仅本地预览的附件（未走项目上传接口，如纯 chat）。 */
	addAttachment = (file: File) => {
		const id = `att-${Date.now()}`;
		const url = URL.createObjectURL(file);
		const attachment: Attachment = {
			id,
			type: file.type.startsWith("image/") ? "image" : "file",
			name: file.name,
			size: file.size,
			url,
			file,
			mimeType: file.type,
		};
		this.#deps.set((state) => ({
			inputAttachments: [...state.inputAttachments, attachment],
		}));
	};

	/**
	 * 上传单个文件到项目并挂到 composer。
	 * 先插 uploading 占位，成功后替换为 completed；失败移除占位。
	 */
	addUploadedAttachment = async (projectId: string, file: File) => {
		if (isEmptyUploadFile(file)) {
			throw new Error(COMPOSER_UPLOAD_EMPTY_FILE_MESSAGE);
		}
		if (!isComposerUploadAllowedFile(file)) {
			throw new Error(COMPOSER_UPLOAD_TYPE_REJECTED_MESSAGE);
		}

		const attachmentId = `att-${Date.now()}`;
		const previewUrl = file.type.startsWith("image/") ? URL.createObjectURL(file) : undefined;
		const abortController = new AbortController();
		this.#uploadAbortControllers.set(attachmentId, abortController);
		const placeholder: Attachment = {
			id: attachmentId,
			type: file.type.startsWith("image/") ? "image" : "file",
			name: file.name,
			size: file.size,
			url: previewUrl,
			file,
			mimeType: file.type,
			uploadStatus: "uploading",
		};

		this.#deps.set((state) => ({
			inputAttachments: [...state.inputAttachments, placeholder],
		}));

		try {
			const response = await projectFileApi.upload({
				projectId,
				projectPublicId: projectId,
				file,
				signal: abortController.signal,
			});
			const payload = response.data;

			const attachment: Attachment = {
				id: attachmentId,
				type: file.type.startsWith("image/") ? "image" : "file",
				name: payload.original_name || payload.filename || file.name,
				size: payload.file_size ?? payload.size ?? file.size,
				url: previewUrl,
				file,
				path: payload.public_id || payload.storage_uri || payload.path,
				fileUploadId: payload.public_id,
				mimeType: payload.mime_type || file.type,
				storageUri: payload.storage_uri,
				uploadStatus: "completed",
			};

			this.#deps.set((state) => ({
				inputAttachments: state.inputAttachments.map((item) =>
					item.id === attachmentId ? attachment : item,
				),
			}));

			return { attachment, message: response.message, cancelled: false as const };
		} catch (err) {
			if (abortController.signal.aborted) {
				return { attachment: placeholder, message: "", cancelled: true as const };
			}
			this.removeAttachment(attachmentId);
			throw err;
		} finally {
			this.#uploadAbortControllers.delete(attachmentId);
		}
	};

	/**
	 * 上传文件夹内多个文件，聚合成一条 type=folder 的 composer 附件。
	 * 跳过空文件/类型不符项，并在返回 message 中汇总。
	 */
	addUploadedFolderAttachment = async (projectId: string, files: File[]) => {
		if (!files.length) {
			throw new Error("未选择文件夹内容");
		}
		if (isFolderUploadSizeExceeded(files)) {
			throw new Error(FOLDER_UPLOAD_SIZE_EXCEEDED_MESSAGE);
		}

		const { uploadable, skippedEmpty, skippedType } = partitionComposerFolderFiles(files);
		if (uploadable.length === 0) {
			throw new Error(
				buildComposerFolderUploadSummaryMessage(0, skippedEmpty.length, skippedType.length),
			);
		}

		const folderName = getFolderNameFromFiles(files);
		const attachmentId = `att-folder-${Date.now()}`;
		const estimatedSize = uploadable.reduce((sum, file) => sum + file.size, 0);
		const abortController = new AbortController();
		this.#uploadAbortControllers.set(attachmentId, abortController);

		const placeholder: Attachment = {
			id: attachmentId,
			type: "folder",
			name: folderName,
			size: estimatedSize,
			uploadStatus: "uploading",
		};

		this.#deps.set((state) => ({
			inputAttachments: [...state.inputAttachments, placeholder],
		}));

		try {
			const folderFiles: NonNullable<Attachment["folderFiles"]> = [];
			let totalSize = 0;

			for (const file of uploadable) {
				if (abortController.signal.aborted) {
					return { attachment: placeholder, message: "", cancelled: true as const };
				}

				const response = await projectFileApi.upload({
					projectId,
					projectPublicId: projectId,
					file,
					signal: abortController.signal,
				});
				const payload = response.data;
				if (!payload?.public_id) {
					throw new Error("上传接口未返回 public_id");
				}

				const relativePath = getFileRelativePath(file);
				const displayName = payload.original_name || payload.filename || file.name;
				const fileSize = payload.file_size ?? payload.size ?? file.size;

				folderFiles.push({
					fileUploadId: payload.public_id,
					name: displayName,
					relativePath,
					mimeType: payload.mime_type || file.type || "application/octet-stream",
					size: fileSize,
				});
				totalSize += fileSize;
			}

			const attachment: Attachment = {
				id: attachmentId,
				type: "folder",
				name: folderName,
				size: totalSize,
				folderFiles,
				uploadStatus: "completed",
			};

			this.#deps.set((state) => ({
				inputAttachments: state.inputAttachments.map((item) =>
					item.id === attachmentId ? attachment : item,
				),
			}));

			return {
				attachment,
				cancelled: false as const,
				message: buildComposerFolderUploadSummaryMessage(
					uploadable.length,
					skippedEmpty.length,
					skippedType.length,
				),
			};
		} catch (err) {
			if (abortController.signal.aborted) {
				return { attachment: placeholder, message: "", cancelled: true as const };
			}
			this.removeAttachment(attachmentId);
			throw err;
		} finally {
			this.#uploadAbortControllers.delete(attachmentId);
		}
	};

	/** 移除附件：若上传中则 abort，并释放 blob URL。 */
	removeAttachment = (id: string) => {
		const abortController = this.#uploadAbortControllers.get(id);
		if (abortController) {
			abortController.abort();
			this.#uploadAbortControllers.delete(id);
		}

		const state = this.#deps.get();
		const att = state.inputAttachments.find((a) => a.id === id);
		revokeAttachmentObjectUrls(att ? [att] : undefined);
		this.#deps.set((s) => ({
			inputAttachments: s.inputAttachments.filter((a) => a.id !== id),
		}));
	};

	/** 标记输入框焦点，供 UI 控制快捷键/样式。 */
	setInputFocused = (focused: boolean) => {
		this.#deps.set({ inputFocused: focused });
	};

	/** 选择当前会话使用的模型 id（本地偏好，发送时可读取）。 */
	setSelectedModel = (modelId: string) => {
		this.#deps.set({ selectedModel: modelId });
	};
}
