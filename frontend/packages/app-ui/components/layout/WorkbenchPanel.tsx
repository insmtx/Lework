"use client";

import { useLayoutStore } from "@leros/store";
import { Button } from "@leros/ui/components/ui/button";
import { Bell, ChevronDown, Folder, Plus, Search, SendHorizonal } from "lucide-react";
import { useState } from "react";

const mockActivities = [
	{
		id: "activity-1",
		avatar: "SK",
		name: "Sarah K.",
		project: "backend-v2",
		time: "2 分钟前",
		description: "完成了 API 追踪",
		note: "解决了 auth-middleware 模块中的 4 个延迟问题。系统开销降低了 12%。",
	},
	{
		id: "activity-2",
		avatar: "AL",
		name: "Ada Lovelace",
		project: "frontend-core",
		time: "45 分钟前",
		description: "更新了文档",
		tags: ["文档", "修订版本 3"],
	},
];

export function WorkbenchPanel() {
	const { projects, activeProjectId, selectWorkbenchProject, sendWorkbenchMessage, switchProject } =
		useLayoutStore((s) => s);
	const [input, setInput] = useState("");

	const handleSend = () => {
		if (!input.trim()) return;
		sendWorkbenchMessage(input, activeProjectId);
		setInput("");
	};

	const activeProject = projects.find((project) => project.id === activeProjectId);
	const latestProject = projects[0];

	return (
		<div data-slot="workbench-panel" className="min-h-0 flex-1 overflow-y-auto bg-[#f7f8fd]">
			<div className="mx-auto flex min-h-full w-full max-w-[1120px] flex-col px-8 py-9">
				<div className="flex justify-end gap-8 text-slate-700">
					<Search className="size-6" />
					<div className="relative">
						<Bell className="size-6" />
						<span className="absolute -right-1 -top-1 size-1.5 rounded-full bg-red-500" />
					</div>
				</div>

				<section className="mx-auto mt-20 w-full max-w-[820px]">
					<h1 className="text-5xl font-bold tracking-normal text-slate-950">
						Hi, <span className="text-blue-600">Mia</span>
					</h1>
					<p className="mt-6 text-xl font-semibold tracking-[0.12em] text-slate-400">
						以 LEROS 赋能您的工作流。
					</p>

					<div className="mt-16 rounded-[28px] border border-slate-200 bg-white shadow-sm">
						<textarea
							value={input}
							onChange={(event) => setInput(event.target.value)}
							onKeyDown={(event) => {
								if (event.key === "Enter" && !event.shiftKey) {
									event.preventDefault();
									handleSend();
								}
							}}
							placeholder="在这里开始新任务，或输入指令以同步您的项目进度..."
							className="min-h-[112px] w-full resize-none rounded-t-[28px] bg-transparent px-8 py-8 text-base text-slate-700 outline-none placeholder:text-slate-300"
						/>
						<div className="mx-7 border-t border-slate-100" />
						<div className="flex items-center justify-between px-7 py-5">
							<div className="flex items-center gap-4">
								<button
									type="button"
									className="flex size-8 items-center justify-center rounded-lg text-slate-700 transition-colors hover:bg-slate-100"
									aria-label="添加附件"
								>
									<Plus className="size-5" />
								</button>
								<div className="relative">
									<Folder className="pointer-events-none absolute left-4 top-1/2 size-4 -translate-y-1/2 text-slate-600" />
									<select
										value={activeProjectId ?? ""}
										onChange={(event) => selectWorkbenchProject(event.target.value || null)}
										className="h-10 min-w-[164px] appearance-none rounded-full border border-slate-200 bg-white pl-11 pr-10 text-sm font-semibold text-slate-700 outline-none transition-colors hover:border-slate-300"
										aria-label="选择项目"
									>
										<option value="">选择项目</option>
										{projects.map((project) => (
											<option key={project.id} value={project.id}>
												{project.name}
											</option>
										))}
									</select>
									<ChevronDown className="pointer-events-none absolute right-4 top-1/2 size-4 -translate-y-1/2 text-slate-500" />
								</div>
								{activeProject && (
									<button
										type="button"
										onClick={() => switchProject(activeProject.id)}
										className="text-sm font-medium text-blue-600 hover:text-blue-700"
									>
										打开项目
									</button>
								)}
							</div>
							<Button
								size="icon"
								onClick={handleSend}
								disabled={!input.trim()}
								className="size-12 rounded-2xl bg-blue-600 text-white shadow-sm hover:bg-blue-700 disabled:bg-slate-100 disabled:text-slate-300"
							>
								<SendHorizonal className="size-5" />
							</Button>
						</div>
					</div>
				</section>

				<section className="mx-auto mt-12 w-full max-w-[820px] pb-16">
					<div className="flex items-center justify-between">
						<h2 className="text-2xl font-bold text-slate-950">动态流</h2>
						<div className="rounded-xl bg-slate-100 p-1">
							<button
								type="button"
								className="rounded-lg bg-white px-5 py-2 text-sm font-semibold text-slate-900 shadow-sm"
							>
								今日
							</button>
							<button type="button" className="px-5 py-2 text-sm font-semibold text-slate-600">
								本周
							</button>
						</div>
					</div>
					<div className="mt-6 border-t border-slate-200 pt-8">
						<div className="space-y-9">
							{mockActivities.map((activity) => (
								<ActivityItem key={activity.id} activity={activity} />
							))}
							{latestProject && (
								<div className="rounded-2xl border border-blue-100 bg-blue-50/60 px-5 py-4 text-sm text-blue-900">
									最近项目：{latestProject.name} · {latestProject.description}
								</div>
							)}
						</div>
					</div>
				</section>
			</div>
		</div>
	);
}

function ActivityItem({ activity }: { activity: (typeof mockActivities)[number] }) {
	return (
		<div className="flex gap-5">
			<div className="flex size-11 shrink-0 items-center justify-center rounded-full bg-slate-900 text-sm font-bold text-white">
				{activity.avatar}
			</div>
			<div className="min-w-0 flex-1">
				<div className="flex items-start justify-between gap-4">
					<p className="text-base text-slate-900">
						<span className="font-bold">{activity.name}</span>
						<span> 在 </span>
						<span className="font-semibold text-blue-600">{activity.project}</span>
						<span> 中{activity.description}</span>
					</p>
					<span className="shrink-0 text-sm text-slate-500">{activity.time}</span>
				</div>
				{activity.note && (
					<div className="mt-3 rounded-xl bg-[#eef2ff] px-5 py-4 text-base text-slate-700">
						“{activity.note}”
					</div>
				)}
				{activity.tags && (
					<div className="mt-4 flex gap-3">
						{activity.tags.map((tag) => (
							<span
								key={tag}
								className="rounded-md bg-slate-100 px-3 py-1 text-xs font-semibold text-slate-700"
							>
								{tag}
							</span>
						))}
					</div>
				)}
			</div>
		</div>
	);
}
