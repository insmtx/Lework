/** 移动端节点颜色与桌面控制面共用同一批 token。 */
export interface NodeColorOption {
	token: string;
	label: string;
	/** 手机端用实色圆点展示，不跟随 Tailwind 的渐变类。 */
	swatch: string;
}

export const NODE_COLOR_OPTIONS: readonly NodeColorOption[] = [
	{ token: "blue", label: "蓝色", swatch: "#38bdf8" },
	{ token: "violet", label: "紫色", swatch: "#a78bfa" },
	{ token: "pink", label: "粉色", swatch: "#f472b6" },
	{ token: "red", label: "红色", swatch: "#fb7185" },
	{ token: "orange", label: "橙色", swatch: "#fb923c" },
	{ token: "green", label: "绿色", swatch: "#34d399" },
	{ token: "teal", label: "青色", swatch: "#2dd4bf" },
	{ token: "gray", label: "灰色", swatch: "#94a3b8" },
];

/**
 * nodeColorSwatch 把服务端 token 转成手机端色值。
 *
 * 服务端只校验 token 形状，因此只认预设列表：直接用下标会把 "constructor"
 * 这类原型链名字当成命中。未知或空值按未设色处理。
 */
export function nodeColorSwatch(token?: string | null): string | null {
	if (!token) return null;
	return NODE_COLOR_OPTIONS.find((option) => option.token === token)?.swatch ?? null;
}
