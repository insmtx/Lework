/**
 * attachmentNote 生成发送给模型的附件提示。
 *
 * 只列文件名而不贴内容：文件已经在项目根目录，模型可以用自己的文件工具读取。
 */
export function attachmentNote(paths: string[]): string {
	if (paths.length === 0) return "";
	return `\n\n已上传到项目根目录：${paths.join(", ")}`;
}
