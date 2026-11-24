/**
 * DesktopConfig 是主进程持有的桌面本地配置。
 *
 * 上游地址必须在创建窗口之前就确定（本地源要按它反代 /api 与 /v1），所以
 * 这份配置落在主进程的 userData 目录，而不是渲染进程的 localStorage。
 */
export interface DesktopConfig {
	apiBaseURL?: string;
	controlPlaneBaseURL?: string;
	/** 旧 file:// origin 的 localStorage 是否已经迁移过。 */
	migratedFromFileOrigin?: boolean;
}
