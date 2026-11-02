package llm

import (
	"context"
	"sync"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const historyOutputLimit = 20000

// truncateIfNeeded 对字符串 s 做字符级截断，确保不超过 historyOutputLimit 个字符。
// 如果截断，会将 truncated 标记设为 true。
func truncateIfNeeded(s string, truncated *bool) string {
	if utf8.RuneCountInString(s) > historyOutputLimit {
		*truncated = true
		runes := []rune(s)
		return string(runes[:historyOutputLimit]) + "..."
	}
	return s
}

// RecorderDb 是基于 gorm 的 Recorder 接口实现。
type RecorderDb struct {
	db          *gorm.DB
	historyChan chan *types.LLMHistory
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewRecorder 创建一个基于 gorm 的 Recorder 实现，并启动异步 consumer goroutine。
func NewRecorder(database *gorm.DB) *RecorderDb {
	r := &RecorderDb{
		db:          database,
		historyChan: make(chan *types.LLMHistory, 5000),
		stopCh:      make(chan struct{}),
	}
	r.wg.Add(1)
	go r.consumeLoop()
	return r
}

// consumeLoop 从 channel 消费 history 记录并写入数据库。
func (r *RecorderDb) consumeLoop() {
	defer r.wg.Done()
	for {
		select {
		case entity := <-r.historyChan:
			if entity == nil {
				continue
			}
			if err := db.CreateLLMHistory(context.Background(), r.db, entity); err != nil {
				logs.Errorf("CreateLLMHistory async write failed: %v", err)
			}
		case <-r.stopCh:
			for {
				select {
				case entity := <-r.historyChan:
					if entity == nil {
						continue
					}
					if err := db.CreateLLMHistory(context.Background(), r.db, entity); err != nil {
						logs.Errorf("CreateLLMHistory drain write failed: %v", err)
					}
				default:
					return
				}
			}
		}
	}
}

// Shutdown 停止 consumer goroutine 并排空 channel 中剩余记录。
func (r *RecorderDb) Shutdown(ctx context.Context) error {
	close(r.stopCh)
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ Recorder = (*RecorderDb)(nil)

// RecordCall 持久化一次 LLM 调用记录。
// 优先通过 channel 异步写入，channel 满时降级同步写入。
func (r *RecorderDb) RecordCall(ctx context.Context, record *CallRecord) error {
	if record == nil {
		return nil
	}
	entity := callRecordToEntity(record)
	select {
	case r.historyChan <- entity:
		return nil
	default:
		return db.CreateLLMHistory(ctx, r.db, entity)
	}
}

// ListCalls 按条件查询 LLM 调用记录列表。
func (r *RecorderDb) ListCalls(ctx context.Context, orgID uint, offset, limit int, modelID *uint, provider, callerType *string, success *bool) ([]*CallRecord, int64, error) {
	entities, total, err := db.ListLLMHistory(ctx, r.db, orgID, offset, limit, modelID, provider, callerType, success)
	if err != nil {
		return nil, 0, err
	}
	records := make([]*CallRecord, 0, len(entities))
	for _, e := range entities {
		records = append(records, callRecordFromEntity(e))
	}
	return records, total, nil
}

// callRecordToEntity 将领域类型 CallRecord 转换为持久化实体 types.LLMHistory。
// 当 r.ID > 0 时保留指定 ID，否则留零值让 gorm 自动分配。
func callRecordToEntity(r *CallRecord) *types.LLMHistory {
	if r == nil {
		return nil
	}
	e := &types.LLMHistory{
		OrgID:           r.OrgID,
		ModelID:         r.ModelID,
		Provider:        r.Provider,
		ModelName:       r.ModelName,
		ModelProvider:   coalesceStr(r.ModelProvider, r.Provider),
		EntryProtocol:   r.EntryProtocol,
		IsStream:        r.IsStream,
		InputTokens:     r.InputTokens,
		OutputTokens:    r.OutputTokens,
		TotalTokens:     r.TotalTokens,
		LatencyMS:       r.LatencyMS,
		PromptTokens:    r.PromptTokens,
		CacheHitTokens:  r.CacheHitTokens,
		CacheMissTokens: r.CacheMissTokens,
		StatusCode:      r.StatusCode,
		Success:         r.Success,
		Status:          coalesceStr(r.Status, boolToStatus(r.Success)),
		Message:         r.Message,
		CallerType:      r.CallerType,
		ReqID:           r.ReqID,
		TraceID:         r.TraceID,
		RetryTimes:      r.RetryTimes,
		InputLen:        r.InputLen,
		OutputLen:       r.OutputLen,
		InputTruncated:  r.InputTruncated,
		OutputTruncated: r.OutputTruncated,
		ClientIP:        r.ClientIP,
		ProjectID:       r.ProjectID,
		SessionID:       r.SessionID,
		MessageID:       r.MessageID,
		AssistantID:     r.AssistantID,
		Uin:             r.Uin,
		StartedAt:       r.StartedAt,
		FinishedAt:      r.FinishedAt,
	}
	if e.InputLen <= 0 {
		e.InputLen = len(r.Input)
	}
	if e.OutputLen <= 0 {
		e.OutputLen = len(r.Output)
	}
	e.Input = truncateIfNeeded(r.Input, &e.InputTruncated)
	e.Output = truncateIfNeeded(r.Output, &e.OutputTruncated)
	if r.ID > 0 {
		e.ID = r.ID
	}
	return e
}

// callRecordFromEntity 将持久化实体 types.LLMHistory 转换为领域类型 CallRecord。
func callRecordFromEntity(e *types.LLMHistory) *CallRecord {
	if e == nil {
		return nil
	}
	return &CallRecord{
		ID:              e.ID,
		OrgID:           e.OrgID,
		ModelID:         e.ModelID,
		Provider:        e.Provider,
		ModelName:       e.ModelName,
		ModelProvider:   e.ModelProvider,
		EntryProtocol:   e.EntryProtocol,
		IsStream:        e.IsStream,
		InputTokens:     e.InputTokens,
		OutputTokens:    e.OutputTokens,
		TotalTokens:     e.TotalTokens,
		LatencyMS:       e.LatencyMS,
		PromptTokens:    e.PromptTokens,
		CacheHitTokens:  e.CacheHitTokens,
		CacheMissTokens: e.CacheMissTokens,
		StatusCode:      e.StatusCode,
		Success:         e.Success,
		Status:          e.Status,
		Message:         e.Message,
		CallerType:      e.CallerType,
		ReqID:           e.ReqID,
		TraceID:         e.TraceID,
		RetryTimes:      e.RetryTimes,
		InputLen:        e.InputLen,
		OutputLen:       e.OutputLen,
		InputTruncated:  e.InputTruncated,
		OutputTruncated: e.OutputTruncated,
		ClientIP:        e.ClientIP,
		ProjectID:       e.ProjectID,
		SessionID:       e.SessionID,
		MessageID:       e.MessageID,
		AssistantID:     e.AssistantID,
		Uin:             e.Uin,
		Input:           e.Input,
		Output:          e.Output,
		StartedAt:       e.StartedAt,
		FinishedAt:      e.FinishedAt,
	}
}

func boolToStatus(success bool) string {
	if success {
		return "success"
	}
	return "error"
}

func coalesceStr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
