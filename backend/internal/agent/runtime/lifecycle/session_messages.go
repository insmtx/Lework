package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"gorm.io/gorm"
)

var ErrNoPendingSessionMessages = errors.New("no pending session messages")

// SessionMessageProvider 将持久化的会话消息注入到 Agent 运行上下文中。
type SessionMessageProvider interface {
	// Prepare 构建历史上下文、占位本轮待处理用户消息，并填充运行输入。
	Prepare(ctx context.Context, req *agent.RequestContext) error
	// CompleteClaimed 将本轮已占位的用户消息标记为已处理。
	CompleteClaimed(ctx context.Context, req *agent.RequestContext) error
}

type dbSessionMessageProvider struct {
	db           *gorm.DB
	contextLimit int
}

// NewDBSessionMessageProvider 创建基于数据库的会话消息上下文提供器。
func NewDBSessionMessageProvider(db *gorm.DB, contextLimit int) SessionMessageProvider {
	if db == nil {
		return nil
	}
	if contextLimit <= 0 {
		contextLimit = 20
	}
	return &dbSessionMessageProvider{db: db, contextLimit: contextLimit}
}

func (p *dbSessionMessageProvider) Prepare(ctx context.Context, req *agent.RequestContext) error {
	if p == nil || p.db == nil || req == nil {
		return nil
	}
	if req.Input.Type != agent.InputTypeMessage {
		return nil
	}
	publicID := strings.TrimSpace(req.Conversation.ID)
	if publicID == "" {
		return nil
	}

	session, err := infradb.GetSessionByPublicID(ctx, p.db, publicID)
	if err != nil || session == nil {
		return fmt.Errorf("find session by public id %s: %w", publicID, err)
	}

	// 历史上下文和本轮输入都以数据库中的 session_id 为准，避免依赖 MQ 中的单条消息内容。
	recentMessages, err := infradb.GetRecentSessionMessages(ctx, p.db, session.ID, p.contextLimit)
	if err != nil {
		return fmt.Errorf("load session context messages: %w", err)
	}
	contextMessages := filterContextMessages(recentMessages)
	claimed, err := infradb.ClaimSessionMessagesByStatus(
		ctx,
		p.db,
		session.ID,
		string(types.MessageRoleUser),
		string(types.MessageStatusPending),
		string(types.MessageStatusProcessing),
	)
	if err != nil {
		return fmt.Errorf("claim pending session messages: %w", err)
	}
	// 如果没有待处理消息且上游也没有显式输入，本次 worker task 只是空触发，应跳过运行。
	if len(claimed) == 0 && strings.TrimSpace(req.Input.Text) == "" && len(req.Input.Messages) == 0 {
		return ErrNoPendingSessionMessages
	}

	req.Conversation.Messages = messagesToInput(contextMessages)
	if len(claimed) > 0 {
		req.Input.Messages = messagesToInput(claimed)
		req.Input.Text = inputTextFromMessages(req.Input.Messages)
		setClaimedMessageIDs(req, claimed)
	}
	return nil
}

func (p *dbSessionMessageProvider) CompleteClaimed(ctx context.Context, req *agent.RequestContext) error {
	if p == nil || p.db == nil || req == nil {
		return nil
	}
	ids := claimedMessageIDs(req)
	if len(ids) == 0 {
		return nil
	}
	return infradb.UpdateMessagesStatus(ctx, p.db, ids, string(types.MessageStatusCompleted))
}

// filterContextMessages 筛选可进入历史上下文的终态消息。
func filterContextMessages(messages []*types.SessionMessage) []*types.SessionMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]*types.SessionMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		switch message.Status {
		case string(types.MessageStatusCompleted), string(types.MessageStatusFailed), string(types.MessageStatusCancelled):
			result = append(result, message)
		}
	}
	return result
}

// messagesToInput 将数据库消息转换为 Agent 标准输入消息。
func messagesToInput(messages []*types.SessionMessage) []agent.InputMessage {
	if len(messages) == 0 {
		return nil
	}
	result := make([]agent.InputMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil || strings.TrimSpace(message.Content) == "" {
			continue
		}
		result = append(result, agent.InputMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return result
}

// inputTextFromMessages 将多条本轮输入按行合并为兼容现有 runtime 的文本输入。
func inputTextFromMessages(messages []agent.InputMessage) string {
	lines := make([]string, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		lines = append(lines, content)
	}
	return strings.Join(lines, "\n")
}

// setClaimedMessageIDs 在运行元数据中记录本轮占位的消息 ID，便于运行结束后更新状态。
func setClaimedMessageIDs(req *agent.RequestContext, messages []*types.SessionMessage) {
	if req.Metadata == nil {
		req.Metadata = map[string]any{}
	}
	ids := make([]uint, 0, len(messages))
	for _, message := range messages {
		if message != nil {
			ids = append(ids, message.ID)
		}
	}
	// 存入 Extra 子属性中
	if req.Metadata["extra"] == nil {
		req.Metadata["extra"] = map[string]interface{}{}
	}
	req.Metadata["extra"].(map[string]interface{})["claimed_message_ids"] = ids
}

// claimedMessageIDs 从运行元数据中恢复本轮占位的消息 ID。
func claimedMessageIDs(req *agent.RequestContext) []uint {
	if req == nil || len(req.Metadata) == 0 {
		return nil
	}
	extra, ok := req.Metadata["extra"].(map[string]interface{})
	if !ok {
		return nil
	}
	value, ok := extra["claimed_message_ids"]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []uint:
		return append([]uint(nil), typed...)
	case []any:
		ids := make([]uint, 0, len(typed))
		for _, item := range typed {
			switch id := item.(type) {
			case uint:
				ids = append(ids, id)
			case int:
				if id > 0 {
					ids = append(ids, uint(id))
				}
			case float64:
				if id > 0 {
					ids = append(ids, uint(id))
				}
			}
		}
		return ids
	default:
		return nil
	}
}
