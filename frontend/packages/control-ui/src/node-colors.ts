export type NodeColorToken =
	| "blue"
	| "violet"
	| "pink"
	| "red"
	| "orange"
	| "green"
	| "teal"
	| "gray";

export interface NodeColorStyle {
	label: string;
	/** 节点行左侧 3px 竖向色条。 */
	bar: string;
	/** 会话头部 2px 横向渐变线。 */
	line: string;
	/** 选中行 / 工作区徽标的淡渐变雾面。 */
	wash: string;
	/** 色板圆点与徽标小圆点。 */
	dot: string;
}

export const NODE_COLOR_TOKENS: readonly NodeColorToken[] = [
	"blue",
	"violet",
	"pink",
	"red",
	"orange",
	"green",
	"teal",
	"gray",
];

export const NODE_COLOR_STYLES: Record<NodeColorToken, NodeColorStyle> = {
	blue: {
		label: "Blue",
		bar: "bg-gradient-to-b from-sky-400/65 via-sky-400/25 to-transparent",
		line: "bg-gradient-to-r from-sky-400/60 via-sky-400/20 to-transparent",
		wash: "bg-gradient-to-r from-sky-300/10 to-transparent",
		dot: "bg-gradient-to-br from-sky-300 to-blue-400/70 ring-1 ring-sky-400/30 dark:ring-sky-300/40",
	},
	violet: {
		label: "Violet",
		bar: "bg-gradient-to-b from-violet-400/65 via-violet-400/25 to-transparent",
		line: "bg-gradient-to-r from-violet-400/60 via-violet-400/20 to-transparent",
		wash: "bg-gradient-to-r from-violet-300/10 to-transparent",
		dot: "bg-gradient-to-br from-violet-300 to-purple-400/70 ring-1 ring-violet-400/30 dark:ring-violet-300/40",
	},
	pink: {
		label: "Pink",
		bar: "bg-gradient-to-b from-pink-400/65 via-pink-400/25 to-transparent",
		line: "bg-gradient-to-r from-pink-400/60 via-pink-400/20 to-transparent",
		wash: "bg-gradient-to-r from-pink-300/10 to-transparent",
		dot: "bg-gradient-to-br from-pink-300 to-rose-400/70 ring-1 ring-pink-400/30 dark:ring-pink-300/40",
	},
	red: {
		label: "Red",
		bar: "bg-gradient-to-b from-rose-400/65 via-rose-400/25 to-transparent",
		line: "bg-gradient-to-r from-rose-400/60 via-rose-400/20 to-transparent",
		wash: "bg-gradient-to-r from-rose-300/10 to-transparent",
		dot: "bg-gradient-to-br from-rose-300 to-red-400/70 ring-1 ring-rose-400/30 dark:ring-rose-300/40",
	},
	orange: {
		label: "Orange",
		bar: "bg-gradient-to-b from-orange-400/65 via-orange-400/25 to-transparent",
		line: "bg-gradient-to-r from-orange-400/60 via-orange-400/20 to-transparent",
		wash: "bg-gradient-to-r from-orange-300/10 to-transparent",
		dot: "bg-gradient-to-br from-orange-300 to-amber-400/70 ring-1 ring-orange-400/30 dark:ring-orange-300/40",
	},
	green: {
		label: "Green",
		bar: "bg-gradient-to-b from-emerald-400/65 via-emerald-400/25 to-transparent",
		line: "bg-gradient-to-r from-emerald-400/60 via-emerald-400/20 to-transparent",
		wash: "bg-gradient-to-r from-emerald-300/10 to-transparent",
		dot: "bg-gradient-to-br from-emerald-300 to-green-400/70 ring-1 ring-emerald-400/30 dark:ring-emerald-300/40",
	},
	teal: {
		label: "Teal",
		bar: "bg-gradient-to-b from-teal-400/65 via-teal-400/25 to-transparent",
		line: "bg-gradient-to-r from-teal-400/60 via-teal-400/20 to-transparent",
		wash: "bg-gradient-to-r from-teal-300/10 to-transparent",
		dot: "bg-gradient-to-br from-teal-300 to-cyan-400/70 ring-1 ring-teal-400/30 dark:ring-teal-300/40",
	},
	gray: {
		label: "Gray",
		bar: "bg-gradient-to-b from-slate-400/65 via-slate-400/25 to-transparent",
		line: "bg-gradient-to-r from-slate-400/60 via-slate-400/20 to-transparent",
		wash: "bg-gradient-to-r from-slate-300/10 to-transparent",
		dot: "bg-gradient-to-br from-slate-300 to-slate-400/70 ring-1 ring-slate-400/30 dark:ring-slate-300/40",
	},
};

/** nodeColorStyle 把服务端 token 转成 Tailwind 类；未知值按未设色处理。 */
export function nodeColorStyle(token?: string | null): NodeColorStyle | null {
	if (!token) return null;
	// 服务端只校验 token 形状，因此必须只认预设列表；直接用对象下标会把
	// "constructor" 这类原型链名字当成命中，返回函数并渲染出空圆点。
	const matched = NODE_COLOR_TOKENS.find((candidate) => candidate === token);
	return matched ? NODE_COLOR_STYLES[matched] : null;
}
