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
