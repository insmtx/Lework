// event 包提供事件定义的共享类型
//
// 该包定义了 Lework 中事件的核心数据结构，
// 用于在不同模块间共享和传递事件信息。
package event

// 事件主题常量定义
const (
	// TopicGithubIssueComment GitHub Issue 评论事件主题
	TopicGithubIssueComment = "interaction.github.issue_comment"
	// TopicGithubPullRequest GitHub Pull Request 事件主题
	TopicGithubPullRequest = "interaction.github.pull_request"
	// TopicGithubPush GitHub Push 提交事件主题
	TopicGithubPush = "interaction.github.push"
)
