'use client'

import { ASSISTANT_CHAT_AVATAR_SRC } from '../../assets'

/** 聊天区 AI 助手固定头像，替换 assets/assistant-avatar.png 即可更新图标 */
export function AssistantChatAvatar() {
  return (
    <img
      src={ASSISTANT_CHAT_AVATAR_SRC}
      alt="Lework"
      className="size-8 shrink-0 object-contain"
    />
  )
}
