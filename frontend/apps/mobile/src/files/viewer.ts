/**
 * viewer 决定一个文件怎么打开：Markdown / 图片应用内渲染，PDF、Office、压缩包
 * 等下载后交给系统应用。HTML 只按文本显示，不执行脚本。
 */

export type PreviewKind = "markdown" | "text" | "image" | "binary" | "auto";

const MARKDOWN_EXTENSIONS = new Set(["md", "markdown", "mdx"]);

// SVG 不在这里：RN 的 Image 不渲染 SVG，按文本看源码更可靠。
const IMAGE_EXTENSIONS = new Set(["png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "heif"]);

const BINARY_EXTENSIONS = new Set([
	"pdf",
	"doc",
	"docx",
	"xls",
	"xlsx",
	"ppt",
	"pptx",
	"odt",
	"ods",
	"odp",
	"zip",
	"tar",
	"gz",
	"tgz",
	"bz2",
	"xz",
	"7z",
	"rar",
	"apk",
	"ipa",
	"dmg",
	"iso",
	"exe",
	"mp3",
	"wav",
	"ogg",
	"mp4",
	"mov",
	"webm",
	"sqlite",
	"db",
	"ttf",
	"otf",
	"woff",
	"woff2",
]);

const MIME_TYPES: Record<string, string> = {
	md: "text/markdown",
	markdown: "text/markdown",
	mdx: "text/markdown",
	txt: "text/plain",
	log: "text/plain",
	ini: "text/plain",
	cfg: "text/plain",
	conf: "text/plain",
	csv: "text/csv",
	json: "application/json",
	yaml: "text/yaml",
	yml: "text/yaml",
	xml: "text/xml",
	html: "text/html",
	htm: "text/html",
	css: "text/css",
	js: "text/javascript",
	mjs: "text/javascript",
	cjs: "text/javascript",
	ts: "text/plain",
	png: "image/png",
	jpg: "image/jpeg",
	jpeg: "image/jpeg",
	gif: "image/gif",
	webp: "image/webp",
	bmp: "image/bmp",
	heic: "image/heic",
	heif: "image/heif",
	svg: "image/svg+xml",
	pdf: "application/pdf",
	doc: "application/msword",
	docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	xls: "application/vnd.ms-excel",
	xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	ppt: "application/vnd.ms-powerpoint",
	pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	zip: "application/zip",
	tar: "application/x-tar",
	gz: "application/gzip",
	tgz: "application/gzip",
	bz2: "application/x-bzip2",
	xz: "application/x-xz",
	"7z": "application/x-7z-compressed",
	rar: "application/vnd.rar",
	apk: "application/vnd.android.package-archive",
	mp3: "audio/mpeg",
	wav: "audio/wav",
	ogg: "audio/ogg",
	mp4: "video/mp4",
	mov: "video/quicktime",
	webm: "video/webm",
};

function extension(path: string): string {
	const name = path.slice(Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1);
	const dot = name.lastIndexOf(".");
	// dot <= 0 时是 .gitignore、Dockerfile 这类无扩展名文件。
	return dot <= 0 ? "" : name.slice(dot + 1).toLowerCase();
}

export function previewKind(path: string): PreviewKind {
	const ext = extension(path);
	if (MARKDOWN_EXTENSIONS.has(ext)) {
		return "markdown";
	}
	if (IMAGE_EXTENSIONS.has(ext)) {
		return "image";
	}
	if (BINARY_EXTENSIONS.has(ext)) {
		return "binary";
	}
	return "auto";
}

export function mimeTypeFor(path: string): string | undefined {
	return MIME_TYPES[extension(path)];
}

export function truncationNotice(truncated: boolean): string | null {
	return truncated ? "文件较大，仅显示前 512 KiB" : null;
}

// openWithSystemApp 把下载好的文件交给系统查看器；没有可用的分享/打开
// 能力时抛错，由页面展示。
export async function openWithSystemApp(uri: string, path: string): Promise<void> {
	const Sharing = await import("expo-sharing");
	if (!(await Sharing.isAvailableAsync())) {
		throw new Error("当前设备无法调用系统应用打开文件");
	}
	await Sharing.shareAsync(uri, {
		mimeType: mimeTypeFor(path),
		dialogTitle: "打开文件",
	});
}
