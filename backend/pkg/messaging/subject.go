package messaging

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// ---- Subject 构建 ----

// WorkerCommandSubject 构建 server -> worker 命令 subject。
//
// 格式：org.<org_id>.worker.<worker_id>.cmd.<lane>
func WorkerCommandSubject(orgID, workerID uint, lane Lane) (string, error) {
	if orgID == 0 {
		return "", fmt.Errorf("org_id is required")
	}
	if workerID == 0 {
		return "", fmt.Errorf("worker_id is required")
	}
	if lane == "" {
		return "", fmt.Errorf("lane is required")
	}
	return fmt.Sprintf("org.%d.worker.%d.%s", orgID, workerID, lane), nil
}

// WorkerCommandWildcard 返回匹配所有 worker command 的 wildcard subject。
//
// 格式：org.*.worker.*.cmd.>
func WorkerCommandWildcard() string {
	return "org.*.worker.*.cmd.>"
}

// RunEventSubject 构建 worker -> server 运行事件 subject。
//
// 格式：org.<org_id>.session.<session_id>.run.<lane>
func RunEventSubject(orgID uint, sessionID string, lane RunEventLane) (string, error) {
	if orgID == 0 {
		return "", fmt.Errorf("org_id is required")
	}
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if lane == "" {
		return "", fmt.Errorf("lane is required")
	}
	return fmt.Sprintf("org.%d.session.%s.%s", orgID, sessionID, string(lane)), nil
}

// RunEventWildcard 返回匹配所有 run event 的 wildcard subject。
func RunEventWildcard() string {
	return "org.*.session.*.run.>"
}

// RunEventStateWildcard 返回匹配所有 state lane 事件的 wildcard subject。
func RunEventStateWildcard() string {
	return "org.*.session.*.run.state"
}

// RunEventStreamWildcard 返回匹配所有 stream lane 事件的 wildcard subject。
func RunEventStreamWildcard() string {
	return "org.*.session.*.run.stream"
}

// ---- Consumer 名称 ----

// WorkerRunConsumer 返回 cmd.run lane 的持久化消费者名称。
// 用于 SubscribeManualDurable，worker 重启后 NATS 从断点续投。
func WorkerRunConsumer() string { return "worker-run-consumer" }

// WorkerControlConsumer 返回 cmd.control lane 的持久化消费者名称。
func WorkerControlConsumer() string { return "worker-control-consumer" }

// WorkerInteractionConsumer 返回 cmd.interaction lane 的持久化消费者名称。
func WorkerInteractionConsumer() string { return "worker-interaction-consumer" }

// WorkerSkillConsumer 返回 cmd.skill lane 的持久化消费者名称。
func WorkerSkillConsumer() string { return "worker-skill-consumer" }

// WorkerLaneConsumer 返回按 org/worker/lane 隔离的 worker 持久化消费者名称。
func WorkerLaneConsumer(orgID, workerID uint, lane Lane) string {
	switch lane {
	case LaneRun:
		return fmt.Sprintf("worker-o%d-w%d-run-consumer", orgID, workerID)
	case LaneControl:
		return fmt.Sprintf("worker-o%d-w%d-control-consumer", orgID, workerID)
	case LaneInteraction:
		return fmt.Sprintf("worker-o%d-w%d-interaction-consumer", orgID, workerID)
	case LaneSkill:
		return fmt.Sprintf("worker-o%d-w%d-skill-consumer", orgID, workerID)
	default:
		return fmt.Sprintf("worker-o%d-w%d-%s-consumer", orgID, workerID, lane)
	}
}

// SessionRunStateConsumer 返回 session run state projector 的持久化消费者名称。
// 用于消费 run.state 事件，投影更新 session 的当前运行状态。
func SessionRunStateConsumer() string { return "session-run-state-projector" }

// ---- Stream 配置 ----

const (
	StreamNameWorker  = "WORKER_CMD_STREAM"
	StreamNameSession = "SESSION_RUN_STREAM"
)

// StreamConfigs 返回所有预配置的 JetStream stream 配置。
//
// WORKER_CMD_STREAM: server -> worker 方向，覆盖所有 worker command subject
// （cmd.run、cmd.control、cmd.interaction、cmd.skill）。
//
//	保留 72h，每 subject 最多 10000 条。使用 DiscardOld，
//	积压时丢弃最旧消息以确保新命令始终可写入。
//
// SESSION_RUN_STREAM: worker -> server/UI 方向，覆盖所有 run event subject
// （run.stream、run.state）。
//
//	保留 24h，每 subject 最多 10000 条。
func StreamConfigs() map[string]nats.StreamConfig {
	return map[string]nats.StreamConfig{
		StreamNameWorker: {
			Name:              StreamNameWorker,
			Subjects:          []string{WorkerCommandWildcard()},
			Storage:           nats.FileStorage,
			Retention:         nats.LimitsPolicy,
			Discard:           nats.DiscardOld,
			MaxAge:            72 * time.Hour,
			MaxMsgsPerSubject: 10000,
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

// StreamNameFromSubject 根据 subject 的路径结构判断它属于哪个 stream。
// worker command subjects:  org.<id>.worker.<id>.cmd.*  → WORKER_CMD_STREAM
// session event subjects:  org.<id>.session.<id>.run.*  → SESSION_RUN_STREAM
func StreamNameFromSubject(subject string) string {
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
