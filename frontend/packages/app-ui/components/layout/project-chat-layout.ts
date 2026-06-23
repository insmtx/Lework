export type ProjectChatLayoutMode = 'sidebar-expanded' | 'sidebar-collapsed'

type ProjectChatLayoutClasses = {
  shell: string
  inner: string
  timelineInner: string
}

/** 根据右侧栏状态返回聊天区布局（shell 管留白，inner 管内容宽度） */
export function getProjectChatLayoutClasses(
  mode: ProjectChatLayoutMode,
): ProjectChatLayoutClasses {
  if (mode === 'sidebar-collapsed') {
    // 侧栏收起后主区域变宽：放宽内容列、减小固定留白，避免两侧大面积空白
    const inner = 'mx-auto w-full min-w-0 max-w-[min(1040px,82%)]'
    return {
      shell: 'px-5 sm:px-6 lg:px-8',
      inner,
      timelineInner: `${inner} flex flex-col gap-4 py-8`,
    }
  }

  // 右侧栏展开：保持较窄内容列，与任务侧栏并存时不显拥挤
  const inner = 'mx-auto w-full min-w-0 max-w-[900px]'
  return {
    shell: 'px-5 sm:px-8 lg:px-10 xl:px-12',
    inner,
    timelineInner: `${inner} flex flex-col gap-4 py-8`,
  }
}
