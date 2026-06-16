import { FileText, ImageIcon, Table2 } from "lucide-react";

type ProjectArtifactIconType = "document" | "spreadsheet" | "image";

/** 根据文件名后缀或产物类型返回与文件/产物列表一致的图标资源路径 */
export function getProjectFileIconSrc(fileName: string): string {
	const lowerPath = fileName.toLowerCase();

	if (lowerPath.endsWith(".jpg")) {
		return "/assets/icons/file-picture-jpg.svg";
	}
	if (lowerPath.endsWith(".jpeg")) {
		return "/assets/icons/file-picture-jpeg.svg";
	}
	if (lowerPath.endsWith(".png")) {
		return "/assets/icons/file-picture-png.svg";
	}
	if (lowerPath.endsWith(".pdf")) {
		return "/assets/icons/file-pdf.svg";
	}

	return "/assets/icons/file-text.svg";
}

/** 文件 Tab 与产物卡片共用的类型图标组件 */
export function ProjectFileTypeIcon({
	fileName,
	artifactType,
	className = "size-6 object-contain",
}: {
	fileName: string;
	artifactType?: ProjectArtifactIconType;
	className?: string;
}) {
	// 产物场景优先使用后端返回的类型，避免无扩展名时图标退化。
	if (artifactType === "spreadsheet") {
		return <Table2 className={className} aria-hidden="true" />;
	}
	if (artifactType === "image") {
		return <ImageIcon className={className} aria-hidden="true" />;
	}
	if (artifactType === "document") {
		return <FileText className={className} aria-hidden="true" />;
	}

	return (
		<img src={getProjectFileIconSrc(fileName)} alt="" className={className} aria-hidden="true" />
	);
}

/** 任务卡片左侧占位图标，先复用文档图标保证视觉统一 */
export function TaskCardIcon({ className = "size-6 object-contain" }: { className?: string }) {
	return <ProjectFileTypeIcon fileName="" className={className} />;
}

/** 右侧紧凑列表最多展示 5 条，超出后内部滚动并隐藏滚动条 */
export const SIDEBAR_COMPACT_LIST_CLASS =
	"max-h-[23rem] space-y-3 overflow-y-auto [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden";
