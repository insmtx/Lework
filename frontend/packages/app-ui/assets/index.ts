export const APP_LOGO_SRC = new URL("./logo.svg", import.meta.url).href;
export const APP_TERMS_OF_SERVICE_PDF_SRC = new URL("./terms-of-service.pdf", import.meta.url).href;
export const APP_PRIVACY_POLICY_PDF_SRC = new URL("./privacy-policy.pdf", import.meta.url).href;

/** 聊天区 AI 助手头像，将 assistant-avatar.png 放入同目录即可 */
export const ASSISTANT_CHAT_AVATAR_SRC = new URL("./assistant-avatar.png", import.meta.url).href;

/** 工作台欢迎区章鱼助手形象 */
export const WORKBENCH_HERO_OCTOPUS_SRC = new URL("./workbench-hero-octopus.png", import.meta.url)
	.href;

/** 项目新建任务空状态章鱼助手形象 */
export const PROJECT_NEW_TASK_HERO_OCTOPUS_SRC = new URL(
	"./project-new-task-hero.png",
	import.meta.url,
).href;

/** 组织未上传自定义图标时展示的默认头像。 */
export const ORGANIZATION_DEFAULT_AVATAR_SRC = new URL(
	"./organization-default-avatar.png",
	import.meta.url,
).href;

/** 自定义 AI 队友未上传头像时展示的固定默认头像。 */
export const CUSTOM_ASSISTANT_DEFAULT_AVATAR_SRC = new URL(
	"./custom-assistant-default-avatar.svg",
	import.meta.url,
).href;
