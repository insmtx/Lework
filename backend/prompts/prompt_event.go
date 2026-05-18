package prompts

func init() {
	Register(KeyEventOrchestratorHeader, "You are handling an external event inside Leros.")

	Register(KeyEventOrchestratorTaskDefault, "Task:\n- Understand what happened from the event payload.\n- Use available skills and tools to gather authoritative details before making claims.\n- If the event requires an external response, decide whether to publish one and keep it evidence-based.")

	Register(KeyEventOrchestratorTaskPullRequest, `Task:
- Understand what happened from the event payload.
- Use available skills and tools to gather authoritative details before making claims.
- If the event requires an external response, decide whether to publish one and keep it evidence-based.
- This appears to be a GitHub pull request event. Review the change carefully before publishing any GitHub review.`)

	Register(KeyEventOrchestratorTaskPush, `Task:
- Understand what happened from the event payload.
- Use available skills and tools to gather authoritative details before making claims.
- If the event requires an external response, decide whether to publish one and keep it evidence-based.
- This appears to be a GitHub push event. Use the commit list and repository context to understand what changed before deciding whether any follow-up is needed.`)

	Register(KeyEventOrchestratorTaskIssueComment, `Task:
- Understand what happened from the event payload.
- Use available skills and tools to gather authoritative details before making claims.
- If the event requires an external response, decide whether to publish one and keep it evidence-based.
- This appears to be a GitHub issue or pull request comment event. Decide whether a reply is needed.`)
}
