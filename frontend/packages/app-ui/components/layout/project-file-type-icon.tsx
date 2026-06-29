import { ClipboardList } from "lucide-react";

const FILE_DOCX_ICON_SRC = new URL("../../assets/icons/file-docx.svg", import.meta.url).href;
const FILE_XLSX_ICON_SRC = new URL("../../assets/icons/file-xlsx.svg", import.meta.url).href;
const FILE_PPTX_ICON_SRC = new URL("../../assets/icons/file-pptx.svg", import.meta.url).href;
const FILE_MD_ICON_SRC = new URL("../../assets/icons/file-md.svg", import.meta.url).href;
const FILE_TEXT_ICON_SRC = new URL("../../assets/icons/file-text.svg", import.meta.url).href;
const FILE_PDF_ICON_SRC = new URL("../../assets/icons/file-pdf.svg", import.meta.url).href;
const FILE_PICTURE_JPEG_ICON_SRC = new URL(
	"../../assets/icons/file-picture-jpeg.svg",
	import.meta.url,
).href;
const FILE_PICTURE_JPG_ICON_SRC = new URL(
	"../../assets/icons/file-picture-jpg.svg",
	import.meta.url,
).href;
const FILE_PICTURE_PNG_ICON_SRC = new URL(
	"../../assets/icons/file-picture-png.svg",
	import.meta.url,
).href;

/** 根据文件名后缀返回与文件 Tab 一致的图标资源路径 */
export function getProjectFileIconSrc(fileName: string): string {
	const lowerPath = fileName.toLowerCase();

	if (lowerPath.endsWith(".jpg")) {
		return FILE_PICTURE_JPG_ICON_SRC;
	}
	if (lowerPath.endsWith(".jpeg")) {
		return FILE_PICTURE_JPEG_ICON_SRC;
	}
	if (lowerPath.endsWith(".png")) {
		return FILE_PICTURE_PNG_ICON_SRC;
	}
	if (lowerPath.endsWith(".pdf")) {
		return FILE_PDF_ICON_SRC;
	}
	if (lowerPath.endsWith(".docx")) {
		return FILE_DOCX_ICON_SRC;
	}
	if (lowerPath.endsWith(".xlsx") || lowerPath.endsWith(".xls") || lowerPath.endsWith(".csv")) {
		return FILE_XLSX_ICON_SRC;
	}
	if (lowerPath.endsWith(".pptx")) {
		return FILE_PPTX_ICON_SRC;
	}
	if (lowerPath.endsWith(".md") || lowerPath.endsWith(".markdown")) {
		return FILE_MD_ICON_SRC;
	}

	return FILE_TEXT_ICON_SRC;
}

/** 文件 Tab 与文件卡片共用的类型图标组件 */
export function ProjectFileTypeIcon({
	fileName,
	className = "size-6 object-contain",
}: {
	fileName: string;
	className?: string;
}) {
	return (
		<img src={getProjectFileIconSrc(fileName)} alt="" className={className} aria-hidden="true" />
	);
}

/** 任务卡片统一使用固定任务图标，和右侧任务列表保持一致 */
export function TaskCardIcon({ className = "size-6 object-contain" }: { className?: string }) {
	return <ClipboardList className={className} aria-hidden="true" />;
}

/** 右侧紧凑列表最多展示 5 条，超出后内部滚动并隐藏滚动条 */
export const SIDEBAR_COMPACT_LIST_CLASS =
	"max-h-[23rem] space-y-3 overflow-y-auto [scrollbar-width:none] [-ms-overflow-style:none] [&::-webkit-scrollbar]:hidden";
