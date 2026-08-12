import { redirect } from "next/navigation";

// 中文注释：资源库路由暂时隐藏，直接访问时回退到工作台。
export default function KnowledgePage() {
	redirect("/workbench");
}
