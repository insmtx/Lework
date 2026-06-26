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
	"time"

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

	if s.giteaClient != nil && s.giteaCfg != nil && s.giteaCfg.Enabled {
		repoName := s.buildRepoName(caller.OrgID, publicID)
		repoInfo, _, err := s.giteaClient.CreateRepo(gitea.CreateRepoOption{
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
	}

	if err := db.CreateProject(ctx, s.db, project); err != nil {
		return nil, err
	}
	if project.GiteaRepoFullName != "" {
		s.initRepoStructure(ctx, project.GiteaRepoFullName)
	}
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
	_ = depth
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

	files, err := db.ListProjectFiles(ctx, s.db, caller.OrgID, project.PublicID, "")
	if err != nil {
		return nil, fmt.Errorf("list project files: %w", err)
	}

	return buildFileTreeFromProjectFiles(files, parentPath), nil
}

// DownloadProjectFile 通过 project_file 表和 filestore 下载/预览项目文件。
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

	if !isPathAllowed(filePath) {
		return nil, "", 0, errors.New("file access denied")
	}

	fileName := filepath.Base(filePath)
	files, err := db.ListProjectFiles(ctx, s.db, caller.OrgID, project.PublicID, "")
	if err != nil {
		return nil, "", 0, fmt.Errorf("list project files: %w", err)
	}

	var target *types.ProjectFile
	for i := range files {
		if files[i].OriginalName == fileName || files[i].Filename == fileName {
			target = &files[i]
			break
		}
	}
	if target == nil {
		return nil, "", 0, fmt.Errorf("file %q not found in project files", fileName)
	}

	objectKey, err := storageKeyFromFilestoreURI(target.StoragePath)
	if err != nil {
		return nil, "", 0, fmt.Errorf("parse storage path: %w", err)
	}

	st := filestore.GetStorage()
	obj, err := st.GetObject(ctx, filestore.DefaultBucket(), objectKey)
	if err != nil {
		return nil, "", 0, fmt.Errorf("read file from storage: %w", err)
	}

	return obj.Body, target.MimeType, target.FileSize, nil
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

	filePublicID := fmt.Sprintf("file_%s", snowflake.GenerateIDBase58())
	pf := &types.ProjectFile{
		PublicID:        filePublicID,
		OrgID:           caller.OrgID,
		ProjectID:       project.ID,
		ProjectPublicID: project.PublicID,
		Filename:        storeFilename,
		OriginalName:    filename,
		MimeType:        mimeType,
		FileSize:        int64(len(data)),
		StoragePath:     result.Path.URI(),
		Sha256:          sha256Hex,
		Source:          "user_upload",
	}
	if err := db.CreateProjectFile(ctx, s.db, pf); err != nil {
		return nil, fmt.Errorf("create project file record: %w", err)
	}

	return &contract.FileUploadResult{
		PublicID: filePublicID,
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

// PresignArtifactUpload 为 Worker 产物文件生成预签名上传 URL
func (s *projectService) PresignArtifactUpload(ctx context.Context, req *contract.PresignArtifactUploadRequest) (*contract.PresignArtifactUploadResponse, error) {
	bucket := strings.TrimSpace(req.Bucket)
	if bucket == "" {
		return nil, errors.New("bucket is required")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return nil, errors.New("key is required")
	}
	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		return nil, errors.New("filename is required")
	}

	defaultBucket := filestore.DefaultBucket()
	if bucket != defaultBucket {
		return nil, fmt.Errorf("bucket %q is not allowed, only %q is supported", bucket, defaultBucket)
	}

	uploadURL, expiresAt, err := filestore.PresignUpload(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("generate presigned upload url: %w", err)
	}

	return &contract.PresignArtifactUploadResponse{
		UploadURL: uploadURL,
		ExpiresAt: expiresAt.Format(time.RFC3339),
	}, nil
}

func (s *projectService) GetStorageConfig(_ context.Context) (*contract.StorageConfigResponse, error) {
	scheme := "s3"
	if filestore.IsLocal() {
		scheme = "file"
	}
	return &contract.StorageConfigResponse{
		Scheme: scheme,
		Bucket: filestore.DefaultBucket(),
	}, nil
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

// lookupFileCreatedAt 已移除，创建时间现在直接使用 ProjectFile.CreatedAt。
// 此文件中的一切 Gitea API 调用仅用于 Gitea 启用时的仓库初始化和 commit 记录查询。

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

// buildFileTreeFromProjectFiles 将扁平的 ProjectFile 列表转换为 FileTreeNode 树结构
func buildFileTreeFromProjectFiles(files []types.ProjectFile, parentPath string) []*contract.FileTreeNode {
	var roots []*contract.FileTreeNode

	for _, pf := range files {
		sourcePrefix := "uploads/"
		if pf.Source == "worker_artifact" {
			sourcePrefix = "artifacts/"
		}
		fullPath := sourcePrefix + pf.OriginalName
		fileName := pf.OriginalName

		node := &contract.FileTreeNode{
			Name:      fileName,
			Path:      fullPath,
			Type:      "file",
			Size:      pf.FileSize,
			MimeType:  pf.MimeType,
			CreatedAt: pf.CreatedAt.Unix(),
			PublicID:  pf.PublicID,
		}
		roots = append(roots, node)
	}

	return filterByParentPaths(roots, strings.Trim(parentPath, "/"))
}

func storageKeyFromFilestoreURI(uri string) (string, error) {
	_, _, key, err := storage.ParseURI(uri)
	if err != nil {
		return "", fmt.Errorf("parse storage uri: %w", err)
	}
	return key, nil
}

// ensure project implements contract.ProjectService at compile time
var _ contract.ProjectService = (*projectService)(nil)
