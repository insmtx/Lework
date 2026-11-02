package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/insmtx/Lework/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

const usageMessageMaxBytes = 256 * 1024

const usageTruncationMarker = "…[truncated]"

const (
	usageInputPreviewMaxBytes   = 20 * 1024
	usageOutputPreviewMaxBytes  = 20 * 1024
	usageMessagePreviewMaxBytes = 1 * 1024
)

// RecordPublisher 是对外发布 CallRecord 的最小抽象。
type RecordPublisher interface {
	PublishUsage(ctx context.Context, record *CallRecord) error
}

// UsageSubscriber 是订阅 usage 事件的抽象，server 端用 eventbus.Subscriber 实现。
type UsageSubscriber interface {
	Subscribe(ctx context.Context, topic string, consumer string, handler func(data []byte)) error
}

// RecorderNATS 通过 NATS JetStream 将 LLM 调用记录发布到 server 端。
type RecorderNATS struct {
	pub   RecordPublisher
	orgID uint
}

var _ Recorder = (*RecorderNATS)(nil)

// NewRecorderNATS 创建一个基于 NATS 的 Recorder 实现。
func NewRecorderNATS(pub RecordPublisher, orgID uint) *RecorderNATS {
	return &RecorderNATS{pub: pub, orgID: orgID}
}

// RecordCall 将调用记录发布到 LLM_USAGE_STREAM。
func (r *RecorderNATS) RecordCall(ctx context.Context, record *CallRecord) error {
	if r.pub == nil {
		return nil
	}
	return r.pub.PublishUsage(ctx, record)
}

// ListCalls 不支持，NATS 端不负责查询。
func (r *RecorderNATS) ListCalls(ctx context.Context, orgID uint, offset, limit int,
	modelID *uint, provider, callerType *string, success *bool) ([]*CallRecord, int64, error) {
	return nil, 0, nil
}

// Shutdown 空实现，NATS recorder 不管理本地 goroutine。
func (r *RecorderNATS) Shutdown(ctx context.Context) error {
	return nil
}

// LLMUsagePublisher 将 eventbus.Publisher 适配为 RecordPublisher。
type LLMUsagePublisher struct {
	publishFn func(ctx context.Context, subject string, event any) error
}

// NewLLMUsagePublisher 创建适配器。
func NewLLMUsagePublisher(publishFn func(ctx context.Context, subject string, event any) error) *LLMUsagePublisher {
	return &LLMUsagePublisher{publishFn: publishFn}
}

// PublishUsage 将 CallRecord 发布到 org.<org_id>.usage.llm。
func (p *LLMUsagePublisher) PublishUsage(ctx context.Context, record *CallRecord) error {
	if record == nil {
		return fmt.Errorf("cannot publish nil LLM usage record")
	}
	subject, err := messaging.LLMUsageSubject(record.OrgID)
	if err != nil {
		return err
	}
	publication, size, err := boundedUsageRecord(record)
	if err != nil {
		return err
	}
	if p.publishFn == nil {
		return fmt.Errorf("LLM usage publisher is unavailable")
	}
	if err := p.publishFn(ctx, subject, publication); err != nil {
		return fmt.Errorf("publish LLM usage payload_bytes=%d: %w", size, err)
	}
	return nil
}

func boundedUsageRecord(record *CallRecord) (*CallRecord, int, error) {
	publication := *record
	if publication.InputLen < 0 || publication.OutputLen < 0 {
		return nil, 0, fmt.Errorf("LLM usage record has a negative original content length")
	}
	if publication.InputLen == 0 {
		publication.InputLen = len(record.Input)
	}
	if publication.OutputLen == 0 {
		publication.OutputLen = len(record.Output)
	}
	publication.Input = strings.ToValidUTF8(publication.Input, "�")
	publication.Output = strings.ToValidUTF8(publication.Output, "�")
	publication.Message = strings.ToValidUTF8(publication.Message, "�")
	publication.InputTruncated = publication.InputTruncated || publication.Input != record.Input
	publication.OutputTruncated = publication.OutputTruncated || publication.Output != record.Output
	if publication.Message != record.Message {
		publication.Message += usageTruncationMarker
	}
	publication.Input = truncateUsagePreview(publication.Input, usageInputPreviewMaxBytes,
		&publication.InputTruncated)
	publication.Output = truncateUsagePreview(publication.Output, usageOutputPreviewMaxBytes,
		&publication.OutputTruncated)
	if len(publication.Message) > usageMessagePreviewMaxBytes {
		publication.Message = truncateUTF8(publication.Message, usageMessagePreviewMaxBytes)
	}

	for {
		body, err := json.Marshal(&publication)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal LLM usage record: %w", err)
		}
		if len(body) <= usageMessageMaxBytes {
			return &publication, len(body), nil
		}
		if !shrinkUsageContent(&publication) {
			return nil, len(body), fmt.Errorf("LLM usage metadata exceeds %d-byte budget (serialized=%d)",
				usageMessageMaxBytes, len(body))
		}
	}
}

func truncateUsagePreview(value string, maxBytes int, truncated *bool) string {
	if len(value) <= maxBytes {
		return value
	}
	*truncated = true
	return truncateUTF8(value, maxBytes)
}

func shrinkUsageContent(record *CallRecord) bool {
	field := ""
	longest := 0
	for _, candidate := range []struct{ name, value string }{
		{"input", record.Input}, {"output", record.Output}, {"message", record.Message},
	} {
		name, value := candidate.name, candidate.value
		if len(value) > longest {
			field, longest = name, len(value)
		}
	}
	if longest == 0 {
		return false
	}
	var target *string
	switch field {
	case "input":
		target = &record.Input
	case "output":
		target = &record.Output
	case "message":
		target = &record.Message
	}
	*target = truncateUTF8(*target, longest/2)
	if field == "input" {
		record.InputTruncated = true
	} else if field == "output" {
		record.OutputTruncated = true
	}
	return true
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	marker := usageTruncationMarker
	if maxBytes <= len(marker) {
		return utf8Prefix(value, maxBytes)
	}
	return utf8Prefix(value, maxBytes-len(marker)) + marker
}

func utf8Prefix(value string, maxBytes int) string {
	if maxBytes >= len(value) {
		return value
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

// StartUsageConsumer 在 server 端订阅 LLM usage 事件并写入数据库。
func StartUsageConsumer(ctx context.Context, recorder Recorder, sub UsageSubscriber) {
	const consumer = "llm-usage-consumer"

	for {
		select {
		case <-ctx.Done():
			logs.InfoContextf(ctx, "llm usage consumer stopped")
			return
		default:
		}

		finished := make(chan struct{}, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logs.ErrorContextf(ctx, "llm usage consumer panicked: %v", r)
				}
				finished <- struct{}{}
			}()
			topic := messaging.LLMUsageWildcard()
			err := sub.Subscribe(ctx, topic, consumer, func(data []byte) {
				var record CallRecord
				if err := json.Unmarshal(data, &record); err != nil {
					logs.ErrorContextf(ctx, "llm usage consumer: unmarshal failed: %v", err)
					return
				}
				if err := recorder.RecordCall(ctx, &record); err != nil {
					logs.ErrorContextf(ctx, "llm usage consumer: RecordCall failed: %v", err)
				}
			})
			if err != nil {
				logs.ErrorContextf(ctx, "llm usage consumer: subscribe failed: %v", err)
			}
		}()

		<-finished

		if ctx.Err() != nil {
			return
		}
		logs.WarnContextf(ctx, "llm usage consumer exited, restarting...")
	}
}
