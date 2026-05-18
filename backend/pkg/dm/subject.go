package dm

// 消费者视角

import (
	"errors"
	"fmt"
)

// WorkerTaskSubject 构造 worker 任务 topic，格式为 "org.{org_id}.worker.{worker_id}.task"。
func WorkerTaskSubject(orgid, workerid uint) (string, error) {
	if orgid == 0 {
		return "", errors.New("orgid is required")
	}
	if workerid == 0 {
		return "", errors.New("workerid is required")
	}
	return fmt.Sprintf("org.%d.worker.%d.task", orgid, workerid), nil
}

// SessionMessageRequestSubject 构造会话请求 topic，格式为 "org.{org_id}.session.{session_id}.request"。
func SessionMessageRequestSubject(orgid uint, sessionid string) (string, error) {
	if orgid == 0 {
		return "", errors.New("orgid is required")
	}
	if sessionid == "" {
		return "", errors.New("sessionid is required")
	}
	return fmt.Sprintf("org.%d.session.%s.message.request", orgid, sessionid), nil
}

// SessionResultStreamSubject 构造会话结果流 topic，格式为 "org.{org_id}.session.{session_id}.stream"。
func SessionResultStreamSubject(orgid uint, sessionid string) (string, error) {
	if orgid == 0 {
		return "", errors.New("orgid is required")
	}
	if sessionid == "" {
		return "", errors.New("sessionid is required")
	}
	return fmt.Sprintf("org.%d.session.%s.message.stream", orgid, sessionid), nil
}

// SessionMessageCompletedSubject 构造会话完成 topic，格式为 "org.{org_id}.session.{session_id}.completed"。
func SessionMessageCompletedSubject(orgid uint, sessionid string) (string, error) {
	if orgid == 0 {
		return "", errors.New("orgid is required")
	}
	if sessionid == "" {
		return "", errors.New("sessionid is required")
	}
	return fmt.Sprintf("org.%d.session.%s.message.completed", orgid, sessionid), nil
}

// SessionMessageCompletedWildcardSubject 构造会话完成 topic 的通配符模式，格式为 "org.*.session.*.completed"。
func SessionMessageCompletedWildcardSubject() string {
	return "org.*.session.*.message.completed"
}

// SessionMessageRequestWildcardSubject 构造会话请求 topic 的通配符模式，格式为 "org.*.session.*.message.request"。
func SessionMessageRequestWildcardSubject() string {
	return "org.*.session.*.message.request"
}
