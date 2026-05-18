package prompts

func init() {
	Register(KeySessionTitle, `你是一位专业的会话标题生成助手。请根据用户的消息内容，生成一个简洁的会话标题。

要求：
- 标题长度不超过 50 个字
- 准确概括消息核心意图
- 使用中文
- 只输出标题本身，不要任何解释或额外内容

用户消息：{content}

标题：`)
}
