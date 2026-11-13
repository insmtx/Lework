/** MenuItem 是右上角菜单的一项；href 走 expo-router 的栈跳转。 */
export interface MenuItem {
	key: string;
	label: string;
	href: string;
	/** badge 标记需要显示未读计数的项；只有待审批需要。 */
	badge?: "pending";
}

// 项目是唯一一级对象，节点与设置都收进菜单：底部不再有 tab bar。
export const MENU_ITEMS: MenuItem[] = [
	{ key: "projects", label: "项目", href: "/projects" },
	{ key: "pending", label: "待审批", href: "/pending", badge: "pending" },
	{ key: "nodes", label: "节点", href: "/nodes" },
	{ key: "settings", label: "设置", href: "/settings" },
];
