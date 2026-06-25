import type { LucideIcon } from "lucide-react";
import {
	BarChart3,
	BrainCircuit,
	Code2,
	FileText,
	Gavel,
	Layers,
	LineChart,
	Megaphone,
	Palette,
	Server,
	ShieldCheck,
	TestTube2,
	Users,
} from "lucide-react";

export type AiTeammateCategory =
	| "solo"
	| "finance"
	| "content"
	| "office"
	| "marketing"
	| "tech"
	| "hr";

export type AiTeammateItem = {
	id: string;
	name: string;
	description: string;
	category: AiTeammateCategory;
	provider: string;
	views: string;
	likes: string;
	icon: LucideIcon;
	iconBg: string;
	iconColor: string;
};

export const AI_TEAMMATE_CATEGORIES: Array<{ value: "" | AiTeammateCategory; label: string }> = [
	{ value: "", label: "全部" },
	{ value: "solo", label: "一人公司" },
	{ value: "finance", label: "金融投资" },
	{ value: "content", label: "内容创作" },
	{ value: "office", label: "办公协同" },
	{ value: "marketing", label: "营销增长" },
	{ value: "tech", label: "技术研发" },
	{ value: "hr", label: "人力资源" },
];

/** 中文注释：静态 mock 数据，后续接入接口时可整体替换。 */
export const AI_TEAMMATE_ITEMS: AiTeammateItem[] = [
	{
		id: "1",
		name: "高级数据分析师",
		description: "擅长数据清洗、可视化与业务洞察，快速输出分析报告与决策建议。",
		category: "finance",
		provider: "QClaw",
		views: "6.1w",
		likes: "4.4k",
		icon: BarChart3,
		iconBg: "bg-sky-100",
		iconColor: "text-sky-600",
	},
	{
		id: "2",
		name: "Python 全栈工程师",
		description: "精通 Python 后端与自动化脚本，可协助搭建 API、数据处理与运维工具。",
		category: "tech",
		provider: "QClaw",
		views: "2.4w",
		likes: "2.8k",
		icon: Code2,
		iconBg: "bg-emerald-100",
		iconColor: "text-emerald-600",
	},
	{
		id: "3",
		name: "资深前端工程师",
		description: "熟悉 React/Vue 生态，专注组件化开发与性能优化，交付高质量界面。",
		category: "tech",
		provider: "QClaw",
		views: "1.7w",
		likes: "1.2k",
		icon: Layers,
		iconBg: "bg-violet-100",
		iconColor: "text-violet-600",
	},
	{
		id: "4",
		name: "高级算法工程师",
		description: "覆盖机器学习建模、特征工程与模型部署，助力智能化业务升级。",
		category: "tech",
		provider: "QClaw",
		views: "5.5w",
		likes: "5.3k",
		icon: BrainCircuit,
		iconBg: "bg-indigo-100",
		iconColor: "text-indigo-600",
	},
	{
		id: "5",
		name: "高级数据科学家",
		description: "结合统计方法与深度学习，提供预测分析、A/B 测试与实验设计支持。",
		category: "finance",
		provider: "QClaw",
		views: "2.5w",
		likes: "2.1k",
		icon: LineChart,
		iconBg: "bg-cyan-100",
		iconColor: "text-cyan-600",
	},
	{
		id: "6",
		name: "高级产品经理",
		description: "从需求调研到 PRD 输出，协助梳理产品路线、用户故事与迭代计划。",
		category: "office",
		provider: "QClaw",
		views: "5.2w",
		likes: "5.1k",
		icon: FileText,
		iconBg: "bg-amber-100",
		iconColor: "text-amber-600",
	},
	{
		id: "7",
		name: "首席 HR 专家",
		description: "覆盖招聘策略、绩效体系与组织发展，提供人力资源全流程建议。",
		category: "hr",
		provider: "QClaw",
		views: "3.0w",
		likes: "3.1k",
		icon: Users,
		iconBg: "bg-rose-100",
		iconColor: "text-rose-600",
	},
	{
		id: "8",
		name: "资深新媒体策划",
		description: "擅长选题策划、内容排期与平台运营，提升品牌曝光与用户互动。",
		category: "content",
		provider: "QClaw",
		views: "2.4w",
		likes: "2.1k",
		icon: Palette,
		iconBg: "bg-fuchsia-100",
		iconColor: "text-fuchsia-600",
	},
	{
		id: "9",
		name: "法务专家",
		description: "提供合同审查、合规咨询与风险识别，保障业务合法稳健运行。",
		category: "office",
		provider: "QClaw",
		views: "2.6w",
		likes: "2.2k",
		icon: Gavel,
		iconBg: "bg-stone-100",
		iconColor: "text-stone-600",
	},
	{
		id: "10",
		name: "高级测试工程师",
		description: "设计测试用例与自动化方案，保障产品质量与发布稳定性。",
		category: "tech",
		provider: "QClaw",
		views: "1.9w",
		likes: "1.3k",
		icon: TestTube2,
		iconBg: "bg-lime-100",
		iconColor: "text-lime-700",
	},
	{
		id: "11",
		name: "高级运维工程师",
		description: "熟悉 CI/CD、监控告警与云原生部署，保障系统高可用。",
		category: "tech",
		provider: "QClaw",
		views: "2.8w",
		likes: "3.1k",
		icon: Server,
		iconBg: "bg-blue-100",
		iconColor: "text-blue-600",
	},
	{
		id: "12",
		name: "增长营销专家",
		description: "制定获客策略、投放优化与转化漏斗分析，驱动业务持续增长。",
		category: "marketing",
		provider: "QClaw",
		views: "4.3w",
		likes: "3.8k",
		icon: Megaphone,
		iconBg: "bg-orange-100",
		iconColor: "text-orange-600",
	},
	{
		id: "13",
		name: "一人公司顾问",
		description: "为独立创业者提供商业规划、成本管控与轻量团队搭建建议。",
		category: "solo",
		provider: "QClaw",
		views: "1.5w",
		likes: "980",
		icon: ShieldCheck,
		iconBg: "bg-teal-100",
		iconColor: "text-teal-600",
	},
	{
		id: "14",
		name: "品牌内容主编",
		description: "统筹品牌叙事与多平台内容，保持统一调性并提升传播效率。",
		category: "content",
		provider: "QClaw",
		views: "3.6w",
		likes: "2.9k",
		icon: FileText,
		iconBg: "bg-pink-100",
		iconColor: "text-pink-600",
	},
	{
		id: "15",
		name: "办公效率教练",
		description: "优化协作流程、会议机制与文档规范，提升团队日常办公效率。",
		category: "office",
		provider: "QClaw",
		views: "2.1w",
		likes: "1.6k",
		icon: Users,
		iconBg: "bg-slate-100",
		iconColor: "text-slate-600",
	},
];
