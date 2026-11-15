/**
 * contentTextStyle 按会话字号倍率缩放消息内容里的文字。
 *
 * 字号与行高必须一起缩放：只改 fontSize 会让大字号挤成一行行，
 * 只改行高又会让文字在行内偏上。倍率来自「设置 → 界面 → 会话字号」。
 */
export function contentTextStyle(
	scale: number,
	fontSize: number,
	lineHeight: number,
): { fontSize: number; lineHeight: number } {
	return {
		fontSize: Math.round(fontSize * scale),
		lineHeight: Math.round(lineHeight * scale),
	};
}
