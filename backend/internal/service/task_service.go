package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

type taskService struct {
	db *gorm.DB
}

func NewTaskService(db *gorm.DB) contract.TaskService {
	return &taskService{
		db: db,
	}
}

func (s *taskService) CreateTask(ctx context.Context, req *contract.CreateTaskRequest) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Title) == "" {
		return nil, errors.New("title is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	publicID := generateTaskPublicID()

	taskType := string(types.TaskTypeGeneral)
	if req.TaskType != nil && *req.TaskType != "" {
		taskType = *req.TaskType
	}

	task := &types.Task{
		OrgID:       caller.OrgID,
		PublicID:    publicID,
		OwnerID:     caller.Uin,
		ProjectID:   project.ID,
		TaskType:    types.TaskType(taskType),
		AssigneeID:  req.AssigneeID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Status:      string(types.TaskStatusCreated),
		Deadline:    req.Deadline,
	}
	if req.Metadata != nil {
		task.Metadata = types.ObjectMetadata{}
		if tags, ok := req.Metadata["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					task.Metadata.Tags = append(task.Metadata.Tags, s)
				}
			}
		}
		if t, ok := req.Metadata["type"].(string); ok {
			task.Metadata.Type = t
		}
		if extra, ok := req.Metadata["extra"].(map[string]interface{}); ok {
			task.Metadata.Extra = extra
		}
	}

	if err := db.CreateTask(ctx, s.db, task); err != nil {
		return nil, err
	}
	return convertToContractTask(task, project.PublicID), nil
}

func (s *taskService) GetTask(ctx context.Context, publicID string) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	task, err := db.GetTaskByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("task not found")
	}
	if err := verifyUserPermission(task.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	projectPublicID, err := s.resolveProjectPublicID(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	return convertToContractTask(task, projectPublicID), nil
}

func (s *taskService) UpdateTask(ctx context.Context, publicID string, req *contract.UpdateTaskRequest) (*contract.Task, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var task *types.Task
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err = db.GetTaskByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if task == nil {
			return errors.New("task not found")
		}
		if err := verifyUserPermission(task.OwnerID, caller.Uin); err != nil {
			return err
		}

		if req.Title != nil {
			task.Title = strings.TrimSpace(*req.Title)
			if task.Title == "" {
				return errors.New("title cannot be empty")
			}
		}
		if req.Description != nil {
			task.Description = strings.TrimSpace(*req.Description)
		}
		if req.TaskType != nil {
			task.TaskType = types.TaskType(*req.TaskType)
		}
		if req.AssigneeID != nil {
			task.AssigneeID = req.AssigneeID
		}
		if req.Status != nil {
			task.Status = *req.Status
		}
		if req.Deadline != nil {
			task.Deadline = req.Deadline
		}
		if req.ProjectID != nil {
			project, dbErr := db.GetProjectByPublicID(ctx, tx, caller.OrgID, *req.ProjectID)
			if dbErr != nil {
				return dbErr
			}
			if project == nil {
				return errors.New("project not found")
			}
			if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
				return err
			}
			task.ProjectID = project.ID
		}
		if req.Metadata != nil {
			if *req.Metadata != nil {
				newMeta := types.ObjectMetadata{}
				if tags, ok := (*req.Metadata)["tags"].([]interface{}); ok {
					for _, t := range tags {
						if s, ok := t.(string); ok {
							newMeta.Tags = append(newMeta.Tags, s)
						}
					}
				}
				if t, ok := (*req.Metadata)["type"].(string); ok {
					newMeta.Type = t
				}
				if extra, ok := (*req.Metadata)["extra"].(map[string]interface{}); ok {
					newMeta.Extra = extra
				}
				task.Metadata = newMeta
			}
		}

		return db.UpdateTask(ctx, tx, task)
	}); err != nil {
		return nil, err
	}

	projectPublicID, err := s.resolveProjectPublicID(ctx, task.ProjectID)
	if err != nil {
		return nil, err
	}
	return convertToContractTask(task, projectPublicID), nil
}

func (s *taskService) DeleteTask(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := db.GetTaskByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if task == nil {
			return errors.New("task not found")
		}
		if err := verifyUserPermission(task.OwnerID, caller.Uin); err != nil {
			return err
		}
		return db.DeleteTask(ctx, tx, task.ID)
	})
}

func (s *taskService) ListTasks(ctx context.Context, req *contract.ListTasksRequest) (*contract.TaskList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}
	if req.ProjectID != nil && *req.ProjectID != "" {
		project, dbErr := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, *req.ProjectID)
		if dbErr != nil {
			return nil, dbErr
		}
		if project == nil {
			return nil, errors.New("project not found")
		}
		if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
			return nil, err
		}
		opt.AddExactFilter("project_id", fmt.Sprintf("%d", project.ID))
	}
	if req.TaskType != nil && *req.TaskType != "" {
		opt.AddFilter("task_type", *req.TaskType)
	}
	if req.AssigneeID != nil {
		opt.AddExactFilter("assignee_id", fmt.Sprintf("%d", *req.AssigneeID))
	}

	tasks, total, err := db.ListTasks(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	projectIDs := make([]uint, 0, len(tasks))
	projectIDSet := make(map[uint]struct{})
	for _, t := range tasks {
		if _, ok := projectIDSet[t.ProjectID]; !ok {
			projectIDSet[t.ProjectID] = struct{}{}
			projectIDs = append(projectIDs, t.ProjectID)
		}
	}
	projectPublicIDMap, err := s.resolveProjectPublicIDs(ctx, projectIDs)
	if err != nil {
		return nil, err
	}

	items := make([]contract.Task, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, *convertToContractTask(task, projectPublicIDMap[task.ProjectID]))
	}
	return &contract.TaskList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func convertToContractTask(task *types.Task, projectPublicID string) *contract.Task {
	if task == nil {
		return nil
	}

	var metadata map[string]interface{}
	m := make(map[string]interface{})
	if len(task.Metadata.Tags) > 0 {
		m["tags"] = task.Metadata.Tags
	}
	if task.Metadata.Type != "" {
		m["type"] = task.Metadata.Type
	}
	if task.Metadata.Extra != nil && len(task.Metadata.Extra) > 0 {
		m["extra"] = task.Metadata.Extra
	}
	if len(m) > 0 {
		metadata = m
	}

	return &contract.Task{
		PublicID:    task.PublicID,
		OrgID:       task.OrgID,
		OwnerID:     task.OwnerID,
		ProjectID:   projectPublicID,
		SessionID:   task.SessionID,
		TaskType:    string(task.TaskType),
		AssigneeID:  task.AssigneeID,
		Title:       task.Title,
		Description: task.Description,
		Status:      task.Status,
		Deadline:    task.Deadline,
		Metadata:    metadata,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
	}
}

func generateTaskPublicID() string {
	return fmt.Sprintf("task_%s", snowflake.GenerateIDBase58())
}

func (s *taskService) resolveProjectPublicID(ctx context.Context, projectID uint) (string, error) {
	projectIDs := []uint{projectID}
	publicIDs, err := s.resolveProjectPublicIDs(ctx, projectIDs)
	if err != nil {
		return "", err
	}
	return publicIDs[projectID], nil
}

func (s *taskService) resolveProjectPublicIDs(ctx context.Context, projectIDs []uint) (map[uint]string, error) {
	result := make(map[uint]string)
	if len(projectIDs) == 0 {
		return result, nil
	}
	projects, err := db.GetProjectsByIDs(ctx, s.db, projectIDs)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		result[p.ID] = p.PublicID
	}
	return result, nil
}

var _ contract.TaskService = (*taskService)(nil)
