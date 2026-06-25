"use client";

/** 中文注释：项目列表页占位，后续接入真实项目概览与创建入口。 */
export function ProjectsHubView() {
	return (
		<div
			data-slot="projects-hub-view"
			className="flex min-h-0 h-full flex-1 flex-col bg-[var(--leros-app-bg)]"
		>
			<div className="border-b border-[var(--leros-control-border)] px-6 py-5">
				<h1 className="text-xl font-bold text-[var(--leros-text-strong)]">项目</h1>
				<p className="mt-2 text-sm text-[var(--leros-text-muted)]">
					项目概览页正在建设中，您仍可通过左侧项目列表进入具体项目。
				</p>
			</div>
		</div>
	);
}
