package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"gorm.io/gorm"

	"github.com/ygpkg/storage-go"

	"code.gitea.io/sdk/gitea"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

const (
	createdAtMaxConcurrent = 8
	createdAtMaxPages      = 100
)

type projectService struct {
	db          *gorm.DB
	inferrer    AssistantInferrer
	giteaClient *gitea.Client
	giteaCfg    *config.GiteaConfig
	env         string
}

// fileTreeEntry 文件树 walk 阶段收集的扁平条目
type fileTreeEntry struct {
	absPath string
	isDir   bool
	size    int64
	modTime int64
}

// NewProjectService 创建项目服务实例
func NewProjectService(db *gorm.DB, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string) contract.ProjectService {
	return &projectService{
		db:          db,
		giteaClient: giteaClient,
		giteaCfg:    giteaCfg,
		env:         env,
	}
}

func NewProjectServiceWithInferrer(db *gorm.DB, inferrer AssistantInferrer, giteaClient *gitea.Client, giteaCfg *config.GiteaConfig, env string) contract.ProjectService {
	return &projectService{
		db:          db,
		inferrer:    inferrer,
		giteaClient: giteaClient,
		giteaCfg:    giteaCfg,
		env:         env,
	}
}

func (s *projectService) CreateProject(ctx context.Context, req *contract.CreateProjectRequest) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}

	publicID := generateProjectPublicID()

	project := &types.Project{
		OrgID:       caller.OrgID,
		PublicID:    publicID,
		OwnerID:     caller.Uin,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Objective:   strings.TrimSpace(req.Objective),
		Status:      "active",
	}
	if req.Metadata != nil {
		project.Metadata = types.ObjectMetadata{}
		if tags, ok := req.Metadata["tags"].([]interface{}); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					project.Metadata.Tags = append(project.Metadata.Tags, s)
				}
			}
		}
		if t, ok := req.Metadata["type"].(string); ok {
			project.Metadata.Type = t
		}
		if extra, ok := req.Metadata["extra"].(map[string]interface{}); ok {
			project.Metadata.Extra = extra
		}
	}

	project.GiteaDefaultBranch = "main"

	repoName := s.buildRepoName(caller.OrgID, publicID)
	repoInfo, _, err := s.giteaClient.AdminCreateRepo(s.giteaCfg.DefaultOwner, gitea.CreateRepoOption{
		Name:        repoName,
		Description: strings.TrimSpace(req.Description),
		Private:     true,
		AutoInit:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("create gitea repo: %w", err)
	}
	project.GiteaRepoFullName = repoInfo.FullName
	project.GiteaRepoID = repoInfo.ID

	if err := db.CreateProject(ctx, s.db, project); err != nil {
		return nil, err
	}
	s.initRepoStructure(ctx, repoInfo.FullName)
	return convertToContractProject(project), nil
}

func (s *projectService) GetProject(ctx context.Context, publicID string) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}
	return convertToContractProject(project), nil
}

func (s *projectService) UpdateProject(ctx context.Context, publicID string, req *contract.UpdateProjectRequest) (*contract.Project, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var project *types.Project
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err = db.GetProjectByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}
		if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
			return err
		}

		if req.Name != nil {
			project.Name = strings.TrimSpace(*req.Name)
			if project.Name == "" {
				return errors.New("name cannot be empty")
			}
		}
		if req.Description != nil {
			project.Description = strings.TrimSpace(*req.Description)
		}
		if req.Objective != nil {
			project.Objective = strings.TrimSpace(*req.Objective)
		}
		if req.OwnerID != nil {
			project.OwnerID = *req.OwnerID
		}
		if req.Status != nil {
			project.Status = *req.Status
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
				project.Metadata = newMeta
			}
		}

		return db.UpdateProject(ctx, tx, project)
	}); err != nil {
		return nil, err
	}
	return convertToContractProject(project), nil
}

func (s *projectService) DeleteProject(ctx context.Context, publicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := db.GetProjectByPublicID(ctx, tx, caller.OrgID, publicID)
		if err != nil {
			return err
		}
		if project == nil {
			return errors.New("project not found")
		}
		if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
			return err
		}
		return db.DeleteProject(ctx, tx, project.ID)
	})
}

func (s *projectService) ListProjects(ctx context.Context, req *contract.ListProjectsRequest) (*contract.ProjectList, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("name", *req.Keyword)
	}
	if req.Status != nil && *req.Status != "" {
		opt.AddFilter("status", *req.Status)
	}

	projects, total, err := db.ListProjects(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.Project, 0, len(projects))
	for _, project := range projects {
		items = append(items, *convertToContractProject(project))
	}
	return &contract.ProjectList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func convertToContractProject(project *types.Project) *contract.Project {
	if project == nil {
		return nil
	}

	var metadata map[string]interface{}
	m := make(map[string]interface{})
	if len(project.Metadata.Tags) > 0 {
		m["tags"] = project.Metadata.Tags
	}
	if project.Metadata.Type != "" {
		m["type"] = project.Metadata.Type
	}
	if project.Metadata.Extra != nil && len(project.Metadata.Extra) > 0 {
		m["extra"] = project.Metadata.Extra
	}
	if len(m) > 0 {
		metadata = m
	}

	return &contract.Project{
		PublicID:    project.PublicID,
		Name:        project.Name,
		Description: project.Description,
		Objective:   project.Objective,
		OwnerID:     project.OwnerID,
		Status:      project.Status,
		Metadata:    metadata,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}

func (s *projectService) DetailProject(ctx context.Context, publicID string) (*contract.ProjectDetail, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	result := &contract.ProjectDetail{
		Project:   *convertToContractProject(project),
		Tasks:     make([]contract.ProjectTaskItem, 0),
		Artifacts: make([]contract.Artifact, 0),
		Members:   make([]contract.ProjectMemberItem, 0),
	}

	// 查询项目会话
	prjSession, _ := db.GetProjectSession(ctx, s.db, project.ID)
	if prjSession != nil {
		result.Session = convertToContractSession(prjSession)
	}

	// 查询项目任务
	tasks, err := db.ListTasksByProjectID(ctx, s.db, caller.OrgID, project.ID)
	if err != nil {
		return nil, err
	}

	// 收集任务会话ID，批量查询会话
	taskSessionIDs := make([]uint, 0)
	taskIDs := make([]uint, 0, len(tasks))
	for _, t := range tasks {
		taskIDs = append(taskIDs, t.ID)
		if t.SessionID != nil {
			taskSessionIDs = append(taskSessionIDs, *t.SessionID)
		}
	}

	taskSessions, err := db.GetSessionsByIDs(ctx, s.db, taskSessionIDs)
	if err != nil {
		return nil, err
	}
	sessionMap := make(map[uint]*types.Session, len(taskSessions))
	for _, sess := range taskSessions {
		sessionMap[sess.ID] = sess
	}

	for _, t := range tasks {
		item := contract.ProjectTaskItem{
			Task: *convertToContractTask(t, project.PublicID),
		}
		if t.SessionID != nil {
			if sess, ok := sessionMap[*t.SessionID]; ok {
				item.Session = convertToContractSession(sess)
			}
		}
		result.Tasks = append(result.Tasks, item)
	}

	// 查询项目产物
	artifacts, err := db.ListArtifactsByProjectID(ctx, s.db, caller.OrgID, project.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range artifacts {
		if converted := convertToContractArtifact(a); converted != nil {
			result.Artifacts = append(result.Artifacts, *converted)
		}
	}

	// 查询项目成员
	members, err := db.ListProjectMembers(ctx, s.db, project.ID)
	if err != nil {
		return nil, err
	}

	userIDs := make([]uint, 0)
	assistantIDs := make([]uint, 0)
	for _, m := range members {
		if m.MemberType == types.MemberTypeUser {
			userIDs = append(userIDs, m.MemberID)
		} else if m.MemberType == types.MemberTypeAssistant {
			assistantIDs = append(assistantIDs, m.MemberID)
		}
	}

	users, _ := db.GetUsersByIDs(ctx, s.db, userIDs)
	userMap := make(map[uint]*types.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	assistants, _ := db.GetAssistantsByIDs(ctx, s.db, assistantIDs)
	assistantMap := make(map[uint]*types.DigitalAssistant, len(assistants))
	for _, a := range assistants {
		assistantMap[a.ID] = a
	}

	for _, m := range members {
		item := contract.ProjectMemberItem{
			MemberID:   m.MemberID,
			MemberType: string(m.MemberType),
			MemberRole: string(m.MemberRole),
			JoinedAt:   m.JoinedAt,
		}
		if m.MemberType == types.MemberTypeUser {
			if u, ok := userMap[m.MemberID]; ok {
				item.Name = u.Name
				item.AvatarURL = u.AvatarURL
			}
		} else if m.MemberType == types.MemberTypeAssistant {
			if a, ok := assistantMap[m.MemberID]; ok {
				item.Name = a.Name
				item.AvatarURL = a.Avatar
			}
		}
		result.Members = append(result.Members, item)
	}

	return result, nil
}

func (s *projectService) GetProjectMemory(ctx context.Context, publicID string) (*contract.ProjectMemory, error) {
	// 1. 鉴权
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	// 2. 查项目（org 隔离）
	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	// 3. 拼 repo 路径: {workspaceRoot}/projects/{orgID}/{publicID}/repo/
	workerID, err := resolveProjectWorkerID(ctx, s.db, project.OrgID, project.ID, s.inferrer)
	if err != nil {
		return nil, fmt.Errorf("resolve project worker: %w", err)
	}
	repoDir, err := workspace.ProjectRepoPath(project.OrgID, workerID, publicID)
	if err != nil {
		return nil, err
	}

	// 4. 读取 MEMORY.md
	memoryPath := workspace.ProjectMemoryPath(repoDir)
	entries, err := localmemory.ReadEntries(memoryPath)
	if err != nil {
		// 文件不存在或不可读时返回空列表而非报错
		if os.IsNotExist(err) {
			return &contract.ProjectMemory{
				Entries: []string{},
				Total:   0,
			}, nil
		}
		return nil, fmt.Errorf("read project memory: %w", err)
	}

	if entries == nil {
		entries = []string{}
	}

	return &contract.ProjectMemory{
		Entries: entries,
		Total:   len(entries),
	}, nil
}

func (s *projectService) GetProjectFileTree(ctx context.Context, publicID string, parentPath string, depth int) ([]*contract.FileTreeNode, error) {
	_ = depth // depth is ignored with recursive tree API
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	if strings.TrimSpace(project.GiteaRepoFullName) == "" {
		return nil, errors.New("project not linked to gitea repo")
	}

	parts := strings.SplitN(project.GiteaRepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid gitea repo full name")
	}
	owner, repo := parts[0], parts[1]

	treeResp, _, err := s.giteaClient.GetTrees(owner, repo, gitea.ListTreeOptions{
		Ref:       project.GiteaDefaultBranch,
		Recursive: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list repo tree: %w", err)
	}

	filtered := make([]gitea.GitEntry, 0, len(treeResp.Entries))
	for _, e := range treeResp.Entries {
		if isPathAllowed(e.Path) {
			filtered = append(filtered, e)
		}
	}

	filePaths := make([]string, 0, len(filtered))
	for _, e := range filtered {
		if e.Type != "tree" {
			filePaths = append(filePaths, e.Path)
		}
	}
	createdAtMap := s.lookupFileCreatedAt(ctx, owner, repo, project.GiteaDefaultBranch, filePaths)

	allRoots := buildFileTree(filtered, createdAtMap)
	roots := filterByParentPaths(allRoots, strings.Trim(parentPath, "/"))
	if roots == nil {
		return nil, errors.New("directory not found")
	}
	return roots, nil
}

// DownloadProjectFile 下载项目中的文件。
// 返回文件流、MIME 类型、文件大小。
func (s *projectService) DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, "", 0, errors.New("public_id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, "", 0, errors.New("file path is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, "", 0, err
	}
	if project == nil {
		return nil, "", 0, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, "", 0, err
	}

	if strings.TrimSpace(project.GiteaRepoFullName) == "" {
		return nil, "", 0, errors.New("project not linked to gitea repo")
	}

	parts := strings.SplitN(project.GiteaRepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, "", 0, errors.New("invalid gitea repo full name")
	}
	owner, repo := parts[0], parts[1]

	if !isPathAllowed(filePath) {
		return nil, "", 0, errors.New("file access denied")
	}

	data, _, err := s.giteaClient.GetFile(owner, repo, project.GiteaDefaultBranch, filePath)
	if err != nil {
		return nil, "", 0, fmt.Errorf("get gitea file: %w", err)
	}

	reader := io.NopCloser(bytes.NewReader(data))

	mimeType := mime.TypeByExtension(filepath.Ext(filePath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return reader, mimeType, 0, nil
}

// PreviewProjectFile 通过代理 Gitea raw endpoint 预览项目文件。
// 返回文件流、Content-Type、Content-Length。
func (s *projectService) PreviewProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, "", 0, errors.New("public_id is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, "", 0, errors.New("file path is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, "", 0, err
	}
	if project == nil {
		return nil, "", 0, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, "", 0, err
	}

	if strings.TrimSpace(project.GiteaRepoFullName) == "" {
		return nil, "", 0, errors.New("project not linked to gitea repo")
	}

	parts := strings.SplitN(project.GiteaRepoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, "", 0, errors.New("invalid gitea repo full name")
	}
	owner, repo := parts[0], parts[1]

	if !isPathAllowed(filePath) {
		return nil, "", 0, errors.New("file access denied")
	}

	if s.giteaCfg == nil {
		return nil, "", 0, errors.New("gitea not configured")
	}

	branch := project.GiteaDefaultBranch
	if branch == "" {
		branch = "main"
	}

	rawURL := fmt.Sprintf("%s/%s/%s/raw/branch/%s/%s",
		strings.TrimRight(s.giteaCfg.Endpoint, "/"),
		owner, repo, branch, filePath)

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, "", 0, fmt.Errorf("create gitea raw request: %w", err)
	}
	req.Header.Set("Authorization", "token "+s.giteaCfg.AdminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("fetch gitea raw file: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, "", 0, fmt.Errorf("gitea raw file returned %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return resp.Body, contentType, resp.ContentLength, nil
}

// UploadProjectFile 上传文件到 storage。
func (s *projectService) UploadProjectFile(ctx context.Context, publicID string, reader io.Reader, filename string) (*contract.FileUploadResult, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return nil, errors.New("filename is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, errors.New("project not found")
	}
	if err := verifyUserPermission(project.OwnerID, caller.Uin); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	hash := sha256.Sum256(data)
	sha256Hex := hex.EncodeToString(hash[:])

	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	if mimeType == "" {
		mimeType = http.DetectContentType(data[:min(len(data), 512)])
	}

	ext := ""
	if idx := strings.LastIndex(filename, "."); idx >= 0 {
		ext = filename[idx:]
	}
	storeFilename := fmt.Sprintf("%s%s", snowflake.GenerateIDBase58(), ext)
	key := fmt.Sprintf("projects/%s/%d/%s/%s", publicID, caller.OrgID, sha256Hex[:8], storeFilename)

	st := filestore.GetStorage()
	bucket := filestore.DefaultBucket()

	result, err := st.PutObject(ctx, bucket, key, bytes.NewReader(data),
		storage.WithContentType(mimeType),
	)
	if err != nil {
		return nil, fmt.Errorf("upload file: %w", err)
	}

	return &contract.FileUploadResult{
		Path:     result.Path.URI(),
		Filename: filename,
		Size:     int64(len(data)),
		URL:      result.Path.PublicURL(),
	}, nil
}

// AddFile 将已上传的文件关联到项目。
func (s *projectService) AddFile(ctx context.Context, publicID string, filePublicID string) error {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}
	if strings.TrimSpace(filePublicID) == "" {
		return errors.New("file_public_id is required")
	}

	project, err := db.GetProjectByPublicID(ctx, s.db, caller.OrgID, publicID)
	if err != nil {
		return err
	}
	if project == nil {
		return errors.New("project not found")
	}

	file, err := db.GetFileUploadByPublicID(ctx, s.db, caller.OrgID, filePublicID)
	if err != nil {
		return err
	}
	if file == nil {
		return errors.New("file not found")
	}

	if file.Metadata.Extra == nil {
		file.Metadata.Extra = make(map[string]interface{})
	}
	file.Metadata.Extra["project_public_id"] = publicID
	if err := db.UpdateFileUpload(ctx, s.db, file); err != nil {
		return fmt.Errorf("update file upload metadata: %w", err)
	}

	return nil
}

func generateProjectPublicID() string {
	return fmt.Sprintf("prj_%s", snowflake.GenerateIDBase58())
}

func (s *projectService) buildRepoName(orgID uint, projectPublicID string) string {
	return fmt.Sprintf("%s-%d-%s", s.env, orgID, projectPublicID)
}

func (s *projectService) initRepoStructure(ctx context.Context, fullName string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return
	}
	owner, repo := parts[0], parts[1]

	emptyContent := base64.StdEncoding.EncodeToString([]byte(""))

	initFiles := []struct {
		path    string
		content string
		msg     string
	}{
		{".leros/memory/.gitkeep", emptyContent, "chore: init .leros/memory/"},
		{"artifacts/.gitkeep", emptyContent, "chore: init artifacts/"},
		{"README.md", base64.StdEncoding.EncodeToString([]byte("# " + repo + "\n")), "chore: init README"},
	}

	for _, f := range initFiles {
		if _, _, err := s.giteaClient.CreateFile(owner, repo, f.path, gitea.CreateFileOptions{
			FileOptions: gitea.FileOptions{
				Message: f.msg,
			},
			Content: f.content,
		}); err != nil {
			logs.WarnContextf(ctx, "[project] init gitea file %s failed: %v", f.path, err)
		}
	}
}

var visibleFolders = []string{"artifacts/", "uploads/"}

var ignoredFiles = map[string]bool{".gitkeep": true}

func isPathAllowed(filePath string) bool {
	name := filepath.Base(filePath)
	if ignoredFiles[name] {
		return false
	}
	for _, prefix := range visibleFolders {
		if strings.HasPrefix(filePath, prefix) {
			return true
		}
	}
	return false
}

// lookupFileCreatedAt 通过并发分页拉取 commit 记录，构造文件路径到首次 commit Unix 时间戳的映射。
func (s *projectService) lookupFileCreatedAt(ctx context.Context, owner, repo, ref string, paths []string) map[string]int64 {
	if len(paths) == 0 {
		return map[string]int64{}
	}

	var (
		mu      sync.Mutex
		needed  = make(map[string]struct{}, len(paths))
		result  = make(map[string]int64, len(paths))
		stopped atomic.Bool
	)
	for _, p := range paths {
		needed[p] = struct{}{}
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(createdAtMaxConcurrent)

	for page := 1; page <= createdAtMaxPages; page++ {
		mu.Lock()
		covered := len(result) == len(needed)
		mu.Unlock()
		if covered || stopped.Load() {
			break
		}
		page := page
		g.Go(func() error {
			if gCtx.Err() != nil {
				return nil
			}
			commits, _, err := s.giteaClient.ListRepoCommits(owner, repo, gitea.ListCommitOptions{
				ListOptions: gitea.ListOptions{Page: page, PageSize: 100},
				SHA:         ref,
				Files:       true,
			})
			if err != nil || len(commits) == 0 {
				stopped.Store(true)
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			for _, c := range commits {
				t := c.Created.Unix()
				if t == 0 && c.RepoCommit != nil && c.RepoCommit.Committer != nil {
					if pt, err := time.Parse(time.RFC3339, c.RepoCommit.Committer.Date); err == nil {
						t = pt.Unix()
					}
				}
				if t == 0 {
					continue
				}
				for _, f := range c.Files {
					if _, ok := needed[f.Filename]; !ok {
						continue
					}
					if _, exists := result[f.Filename]; !exists {
						result[f.Filename] = t
					}
				}
			}
			return nil
		})
	}

	g.Wait()
	return result
}

func buildFileTree(entries []gitea.GitEntry, createdAtMap map[string]int64) []*contract.FileTreeNode {
	nodeMap := make(map[string]*contract.FileTreeNode)
	var roots []*contract.FileTreeNode

	for _, entry := range entries {
		parts := strings.Split(entry.Path, "/")
		name := parts[len(parts)-1]

		var parentPath string
		if len(parts) > 1 {
			parentPath = strings.Join(parts[:len(parts)-1], "/")
		}

		node := &contract.FileTreeNode{
			Name: name,
			Path: entry.Path,
		}
		if entry.Type == "tree" {
			node.Type = "directory"
			node.Children = make([]*contract.FileTreeNode, 0)
		} else {
			node.Type = "file"
			node.Size = entry.Size
			if mt := mimeTypeByExt(name); mt != "" {
				node.MimeType = mt
			}
			if createdAtMap != nil {
				node.CreatedAt = createdAtMap[entry.Path]
			}
		}

		nodeMap[entry.Path] = node

		if parentPath == "" {
			roots = append(roots, node)
		} else if parent, ok := nodeMap[parentPath]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			ancestors := strings.Split(entry.Path, "/")
			for i := 1; i < len(ancestors); i++ {
				prefix := strings.Join(ancestors[:i], "/")
				if _, exists := nodeMap[prefix]; exists {
					continue
				}
				dirName := ancestors[i-1]
				dirNode := &contract.FileTreeNode{
					Name:     dirName,
					Path:     prefix,
					Type:     "directory",
					Children: make([]*contract.FileTreeNode, 0),
				}
				nodeMap[prefix] = dirNode

				var grandParent string
				if i > 1 {
					grandParent = strings.Join(ancestors[:i-1], "/")
				}
				if grandParent == "" {
					roots = append(roots, dirNode)
				} else if gp, ok := nodeMap[grandParent]; ok {
					gp.Children = append(gp.Children, dirNode)
				}
			}
			if parent, ok := nodeMap[parentPath]; ok {
				parent.Children = append(parent.Children, node)
			}
		}
	}
	return roots
}

func filterByParentPaths(roots []*contract.FileTreeNode, parentPath string) []*contract.FileTreeNode {
	parentPath = strings.Trim(parentPath, "/")
	if parentPath == "" {
		return roots
	}
	parts := strings.Split(parentPath, "/")
	current := roots
	for i, part := range parts {
		found := false
		for _, node := range current {
			if node.Name == part {
				if i == len(parts)-1 {
					return []*contract.FileTreeNode{node}
				}
				if node.Type == "directory" {
					current = node.Children
					found = true
					break
				}
			}
		}
		if !found {
			return nil
		}
	}
	return nil
}

func mimeTypeByExt(filename string) string {
	ext := filepath.Ext(filename)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		return mimeType
	}
	return ""
}

// ensure project implements contract.ProjectService at compile time
var _ contract.ProjectService = (*projectService)(nil)
