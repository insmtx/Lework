package dm

import (
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	streamNameTask    = "TASK_STREAM"
	streamNameSession = "SESSION_STREAM"
)

// StreamSubjects 定义各 Stream 的完整 NATS 配置，包括 subject 匹配模式和保留策略。
var StreamSubjects = map[string]nats.StreamConfig{
	streamNameTask: {
		Name:              streamNameTask,
		Subjects:          []string{"org.*.worker.*.task", "org.*.worker.*.approval"},
		Storage:           nats.FileStorage,
		Retention:         nats.LimitsPolicy,
		Discard:           nats.DiscardOld,
		MaxAge:            72 * time.Hour,
		MaxMsgsPerSubject: 200,
	},
	streamNameSession: {
		Name:              streamNameSession,
		Subjects:          []string{"org.*.session.*.message.*"},
		Storage:           nats.FileStorage,
		Retention:         nats.LimitsPolicy,
		Discard:           nats.DiscardOld,
		MaxAge:            24 * time.Hour,
		MaxMsgsPerSubject: 10000,
	},
}

func SessionStream() string {
	return streamNameSession
}

// StreamNameFromTopic 根据 topic 返回 Stream 名称。
func StreamNameFromTopic(topic string) string {
	parts := strings.SplitN(topic, ".", 4)
	if len(parts) < 4 {
		return ""
	}
	subject := parts[2]
	switch subject {
	case "worker":
		return streamNameTask
	case "session":
		return streamNameSession
	default:
		return ""
	}
}
