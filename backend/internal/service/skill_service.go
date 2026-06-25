package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
)

const defaultRecentSkillLimit = 10

type skillService struct {
	db        *gorm.DB
	publisher mq.Publisher
	inferrer  AssistantInferrer
}

// NewSkillService creates a new SkillService.
func NewSkillService(db *gorm.DB, publisher mq.Publisher, inferrer AssistantInferrer) contract.SkillService {
	return &skillService{db: db, publisher: publisher, inferrer: inferrer}
}

func (s *skillService) ListRecentUsedSkills(ctx context.Context, orgID, uin uint, limit int) ([]contract.SkillInstalledItem, error) {
	if limit <= 0 {
		limit = defaultRecentSkillLimit
	}

	keys, err := infradb.GetDistinctSkillCodes(ctx, s.db, orgID, uin, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get distinct skill codes: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	recentCodes := make(map[string]struct{}, len(keys))
	orderedCodes := make([]string, 0, len(keys))
	for _, key := range keys {
		code := key
		if idx := strings.Index(key, ":"); idx != -1 {
			code = key[idx+1:]
		}
		if _, ok := recentCodes[code]; !ok {
			recentCodes[code] = struct{}{}
			orderedCodes = append(orderedCodes, code)
		}
	}

	installedList, err := s.fetchInstalledSkills(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch installed skills: %w", err)
	}

	installedMap := make(map[string]*contract.SkillInstalledItem, len(installedList))
	for i := range installedList {
		installedMap[installedList[i].Name] = &installedList[i]
	}

	result := make([]contract.SkillInstalledItem, 0, len(orderedCodes))
	for _, code := range orderedCodes {
		if sk, ok := installedMap[code]; ok {
			result = append(result, *sk)
		}
	}

	return result, nil
}

func (s *skillService) fetchInstalledSkills(ctx context.Context, orgID uint) ([]contract.SkillInstalledItem, error) {
	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, orgID, s.inferrer)
	if err != nil {
		return nil, err
	}

	topic, err := dm.WorkerSkillSubject(orgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-list-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    orgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action: "list",
		},
	}

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reply, err := s.publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return nil, fmt.Errorf("request skill list: %w", err)
	}

	var resp protocol.SkillManagementResponse
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal skill list response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("skill list failed: %s", resp.Error)
	}

	var items []protocol.SkillListItem
	if err := json.Unmarshal(resp.Data, &items); err != nil {
		return nil, fmt.Errorf("unmarshal skill list items: %w", err)
	}

	result := make([]contract.SkillInstalledItem, 0, len(items))
	for _, item := range items {
		result = append(result, contract.SkillInstalledItem{
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
			Source:      item.Source,
			Trust:       item.Trust,
		})
	}

	return result, nil
}
