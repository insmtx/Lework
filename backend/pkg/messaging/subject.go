package messaging

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// ---- Subject 构建 ----

// WorkerCommandSubject 构造指定 lane 的 worker 命令 subject。
//
// 格式：org.{org_id}.worker.{worker_id}.{lane}
// lane 可选值：cmd.run, cmd.control, cmd.interaction, cmd.skill
func WorkerCommandSubject(orgID, workerID uint, lane Lane) (string, error) {
	if orgID == 0 {
		return "", errors.New("orgID is required")
	}
	if workerID == 0 {
		return "", errors.New("workerID is required")
	}
	if lane == "" {
		return "", errors.New("lane is required")
	}
	return fmt.Sprintf("org.%d.worker.%d.%s", orgID, workerID, lane), nil
}

// WorkerCommandWildcard 返回匹配所有 worker command 的 wildcard subject。
//
// 格式：org.*.worker.*.cmd.>
func WorkerCommandWildcard() string {
	return "org.*.worker.*.cmd.>"
}

// RunEventSubject 构造指定 lane 的运行事件 subject。
//
// 格式：org.{org_id}.session.{session_id}.{lane}
// lane 可选值：run.stream, run.state
func RunEventSubject(orgID uint, sessionID string, lane RunEventLane) (string, error) {
	if orgID == 0 {
		return "", errors.New("orgID is required")
	}
	if sessionID == "" {
		return "", errors.New("sessionID is required")
	}
	if lane == "" {
		return "", errors.New("lane is required")
	}
	return fmt.Sprintf("org.%d.session.%s.%s", orgID, sessionID, lane), nil
}

// RunEventWildcard 返回匹配所有 run event 的 wildcard subject。
//
// 格式：org.*.session.*.run.>
func RunEventWildcard() string {
	return "org.*.session.*.run.>"
}

// RunEventStateWildcard 返回匹配所有 state lane 事件的 wildcard subject。
//
// 格式：org.*.session.*.run.state
func RunEventStateWildcard() string {
	return "org.*.session.*.run.state"
}

// RunEventStreamWildcard 返回匹配所有 stream lane 事件的 wildcard subject。
//
// 格式：org.*.session.*.run.stream
func RunEventStreamWildcard() string {
	return "org.*.session.*.run.stream"
}

// ---- Consumer 名称 ----

// WorkerRunConsumer 返回 cmd.run lane 的持久化消费者名称。
func WorkerRunConsumer() string {
	return "worker-run-consumer"
}

// WorkerControlConsumer 返回 cmd.control lane 的持久化消费者名称。
func WorkerControlConsumer() string {
	return "worker-control-consumer"
}

// WorkerInteractionConsumer 返回 cmd.interaction lane 的持久化消费者名称。
func WorkerInteractionConsumer() string {
	return "worker-interaction-consumer"
}

// WorkerSkillConsumer 返回 cmd.skill lane 的持久化消费者名称。
func WorkerSkillConsumer() string {
	return "worker-skill-consumer"
}

// SessionRunStateConsumer 返回 run state projector 的持久化消费者名称。
func SessionRunStateConsumer() string {
	return "session-run-state-projector"
}

// ---- Stream 配置 ----

const (
	// StreamNameWorker 是 server -> worker 方向的 JetStream stream 名称。
	StreamNameWorker = "WORKER_CMD_STREAM"
	// StreamNameSession 是 worker -> server/UI 方向的 JetStream stream 名称。
	StreamNameSession = "SESSION_RUN_STREAM"
)

// StreamConfigs 返回所有预配置的 JetStream stream 配置。
//
// WORKER_CMD_STREAM: 覆盖所有 server -> worker 命令 subject（cmd.run, cmd.control, cmd.interaction, cmd.skill）。
//
//	保留 72h，每 subject 最多 200 条消息。
//
// SESSION_RUN_STREAM: 覆盖所有 worker -> server/UI 运行事件 subject（run.stream, run.state）。
//
//	保留 24h，每 subject 最多 10000 条消息。
func StreamConfigs() map[string]nats.StreamConfig {
	return map[string]nats.StreamConfig{
		StreamNameWorker: {
			Name:              StreamNameWorker,
			Subjects:          []string{WorkerCommandWildcard()},
			Storage:           nats.FileStorage,
			Retention:         nats.LimitsPolicy,
			Discard:           nats.DiscardOld,
			MaxAge:            72 * time.Hour,
			MaxMsgsPerSubject: 200,
		},
		StreamNameSession: {
			Name:              StreamNameSession,
			Subjects:          []string{RunEventWildcard()},
			Storage:           nats.FileStorage,
			Retention:         nats.LimitsPolicy,
			Discard:           nats.DiscardOld,
			MaxAge:            24 * time.Hour,
			MaxMsgsPerSubject: 10000,
		},
	}
}

// StreamNameFromSubject 根据 subject 返回对应的 stream 名称。
func StreamNameFromSubject(subject string) string {
	// worker command subjects: org.*.worker.*.cmd.*
	// session event subjects: org.*.session.*.run.*
	parts := splitSubject(subject)
	if len(parts) < 4 {
		return ""
	}
	switch parts[2] {
	case "worker":
		return StreamNameWorker
	case "session":
		return StreamNameSession
	default:
		return ""
	}
}

func splitSubject(subject string) []string {
	parts := make([]string, 0, 6)
	start := 0
	for i := 0; i < len(subject); i++ {
		if subject[i] == '.' {
			parts = append(parts, subject[start:i])
			start = i + 1
		}
	}
	if start < len(subject) {
		parts = append(parts, subject[start:])
	}
	return parts
}
