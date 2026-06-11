package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	localmemory "github.com/insmtx/Leros/backend/internal/memory/local"
	"github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

type projectService struct {
	db *gorm.DB
}

// fileTreeEntry 文件树 walk 阶段收集的扁平条目
type fileTreeEntry struct {
	absPath string
	isDir   bool
	size    int64
	modTime int64
}

// getWorkerIDByProjectID 根据项目 publicID 获取对应的 worker ID。
// TODO: 实现根据项目查询实际 worker 的逻辑，当前固定返回 1。
func getWorkerIDByProjectID(publicID string) uint {
	// TODO: 从项目-worker 映射表查询
	return 1
}

// NewProjectService 创建项目服务实例
func NewProjectService(db *gorm.DB) contract.ProjectService {
	return &projectService{
		db: db,
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

	if err := db.CreateProject(ctx, s.db, project); err != nil {
		return nil, err
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
		result.Artifacts = append(result.Artifacts, convertToContractArtifact(a))
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
	workerID := getWorkerIDByProjectID(publicID)
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
	// 1. 鉴权
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	// 2. 查项目
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

	// 3. 解析 repo 路径
	workerID := getWorkerIDByProjectID(publicID)
	repoDir, err := workspace.ProjectRepoPath(project.OrgID, workerID, publicID)
	if err != nil {
		return nil, err
	}

	// 4. 确定起始目录
	startDir := repoDir
	parentPath = strings.TrimSpace(parentPath)
	if parentPath != "" {
		cleanPath := filepath.Clean(filepath.FromSlash(parentPath))
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			return nil, errors.New("invalid parent path")
		}
		startDir = filepath.Join(repoDir, cleanPath)
		// 确保起始目录存在
		if info, statErr := os.Stat(startDir); statErr != nil || !info.IsDir() {
			return nil, errors.New("directory not found")
		}
	}

	// 5. 默认 depth=2
	if depth <= 0 {
		depth = 2
	}

	// 6. 构建 IgnoreChecker
	checker, err := workspace.NewIgnoreChecker(repoDir)
	if err != nil {
		return nil, fmt.Errorf("init ignore checker: %w", err)
	}

	// 7. Walk 收集扁平条目（带深度控制）
	var entries []fileTreeEntry

	err = filepath.WalkDir(startDir, func(absPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		// 跳过起始目录自身
		if absPath == startDir {
			return nil
		}

		// 计算相对深度
		relFromStart, _ := filepath.Rel(startDir, absPath)
		currentDepth := strings.Count(relFromStart, string(filepath.Separator)) + 1

		if d.IsDir() {
			if checker.ShouldSkipDir(absPath) {
				return filepath.SkipDir
			}
			if currentDepth > depth {
				return filepath.SkipDir
			}
			dirModTime := int64(0)
			if dirInfo, statErr := d.Info(); statErr == nil {
				dirModTime = dirInfo.ModTime().Unix()
			}
			entries = append(entries, fileTreeEntry{absPath: absPath, isDir: true, modTime: dirModTime})
			return nil
		}

		// 文件：检查是否被忽略
		ignored, ignoreErr := checker.IsIgnored(absPath)
		if ignoreErr != nil || ignored {
			return nil
		}

		if currentDepth > depth {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}

		entries = append(entries, fileTreeEntry{
			absPath: absPath,
			isDir:   false,
			size:    info.Size(),
			modTime: info.ModTime().Unix(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repo: %w", err)
	}

	// 8. 构建树
	return buildFileTree(entries, repoDir, startDir), nil
}

// buildFileTree 将扁平条目构建为递归文件树。
// entries 按路径排序以确保父节点先于子节点处理。
func buildFileTree(entries []fileTreeEntry, repoDir string, startDir string) []*contract.FileTreeNode {
	if len(entries) == 0 {
		return []*contract.FileTreeNode{}
	}

	// 排序：目录优先 → 按深度 → 字典序
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.isDir != b.isDir {
			return a.isDir
		}
		depthA := strings.Count(a.absPath, string(filepath.Separator))
		depthB := strings.Count(b.absPath, string(filepath.Separator))
		if depthA != depthB {
			return depthA < depthB
		}
		return a.absPath < b.absPath
	})

	nodeIndex := make(map[string]*contract.FileTreeNode)
	var roots []*contract.FileTreeNode

	for _, e := range entries {
		rel, _ := filepath.Rel(repoDir, e.absPath)
		slashPath := filepath.ToSlash(rel)

		node := &contract.FileTreeNode{
			Name: filepath.Base(e.absPath),
			Path: slashPath,
			Type: "file",
		}

		if e.isDir {
			node.Type = "directory"
			node.Children = make([]*contract.FileTreeNode, 0)
			node.ModTime = e.modTime
		} else {
			node.Size = e.size
			node.ModTime = e.modTime
			// 通过扩展名检测 MIME 类型
			ext := filepath.Ext(e.absPath)
			if mimeType := mime.TypeByExtension(ext); mimeType != "" {
				node.MimeType = mimeType
			}
		}

		nodeIndex[e.absPath] = node

		parent := filepath.Dir(e.absPath)
		if parent == startDir {
			roots = append(roots, node)
		} else if p, ok := nodeIndex[parent]; ok {
			p.Children = append(p.Children, node)
		}
	}

	if roots == nil {
		roots = []*contract.FileTreeNode{}
	}
	return roots
}

// DownloadProjectFile 下载项目中的文件。
// 返回文件流、MIME 类型、文件大小。
func (s *projectService) DownloadProjectFile(ctx context.Context, publicID string, filePath string) (io.ReadCloser, string, int64, error) {
	// 1. 鉴权
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

	// 2. 查项目
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

	// 3. 解析 repo 路径
	workerID := getWorkerIDByProjectID(publicID)
	repoDir, err := workspace.ProjectRepoPath(project.OrgID, workerID, publicID)
	if err != nil {
		return nil, "", 0, err
	}

	// 4. 安全解析文件路径（防穿越）
	absPath, err := workspace.SafeJoin(repoDir, filepath.FromSlash(filePath))
	if err != nil {
		return nil, "", 0, fmt.Errorf("invalid file path: %w", err)
	}

	// 5. 打开文件
	file, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", 0, errors.New("file not found")
		}
		return nil, "", 0, fmt.Errorf("open file: %w", err)
	}

	// 6. 获取文件信息
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, "", 0, fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		file.Close()
		return nil, "", 0, errors.New("cannot download a directory")
	}

	// 7. 检测 MIME 类型
	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		// 通过内容嗅探
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		if n > 0 {
			mimeType = http.DetectContentType(buf[:n])
		}
		// 回退到文件开头
		file.Seek(0, io.SeekStart)
	}

	return file, mimeType, info.Size(), nil
}

// UploadProjectFile 上传文件到项目 Upload 目录。
// 若存在同名文件，自动追加 _1、_2 等序列号。
func (s *projectService) UploadProjectFile(ctx context.Context, publicID string, reader io.Reader, filename string) (*contract.FileUploadResult, error) {
	// 1. 鉴权
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

	// 2. 查项目
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

	// 3. 解析 repo 路径
	workerID := getWorkerIDByProjectID(publicID)
	repoDir, err := workspace.ProjectRepoPath(project.OrgID, workerID, publicID)
	if err != nil {
		return nil, err
	}

	// 4. 确保 Upload 目录存在
	uploadDir := filepath.Join(repoDir, "Upload")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upload dir: %w", err)
	}

	// 5. 文件名去重：若存在同名文件，追加 _1、_2 ...
	safeFilename := resolveUniqueFilename(uploadDir, filename)

	// 6. 安全路径校验
	absPath, err := workspace.SafeJoin(repoDir, filepath.Join("Upload", safeFilename))
	if err != nil {
		return nil, fmt.Errorf("invalid filename: %w", err)
	}

	// 7. 写入文件
	file, err := os.Create(absPath)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	written, err := io.Copy(file, reader)
	if err != nil {
		// 写入失败时清理已创建的文件
		os.Remove(absPath)
		return nil, fmt.Errorf("write file: %w", err)
	}

	return &contract.FileUploadResult{
		Path:     filepath.ToSlash(filepath.Join("Upload", safeFilename)),
		Filename: safeFilename,
		Size:     written,
	}, nil
}

// resolveUniqueFilename 在目标目录中生成不重复的文件名。
// 若 base.ext 已存在，则尝试 base_1.ext、base_2.ext 直到不重复。
func resolveUniqueFilename(dir, filename string) string {
	ext := filepath.Ext(filename)
	base := filename[:len(filename)-len(ext)]

	candidate := filename
	for i := 1; ; i++ {
		target := filepath.Join(dir, candidate)
		if _, err := os.Stat(target); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
	}
}

func generateProjectPublicID() string {
	return fmt.Sprintf("prj_%s", snowflake.GenerateIDBase58())
}

// ensure project implements contract.ProjectService at compile time
var _ contract.ProjectService = (*projectService)(nil)
