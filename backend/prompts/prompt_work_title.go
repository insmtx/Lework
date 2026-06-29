package prompts

func init() {
	Register(KeyWorkTitle, `用户消息：
{user_message}

你是一位专业的任务命名助手。请根据用户的首次消息，生成一个简洁、准确的项目/任务标题。

要求：
- 不超过50字
- 概括用户的核心诉求，避免"帮帮我"等泛化表述
- 只输出标题本身，不要任何解释或额外内容

标题：`)
}
