package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	catalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/types"
)

type skillMarketplaceService struct {
	db        *gorm.DB
	publisher eventbus.Publisher
}

// NewSkillMarketplaceService 创建 Skill 市场服务。
func NewSkillMarketplaceService(db *gorm.DB, publisher eventbus.Publisher) contract.SkillMarketplaceService {
	return &skillMarketplaceService{db: db, publisher: publisher}
}

func (s *skillMarketplaceService) SearchSkillMarketplace(ctx context.Context, req *contract.SearchSkillMarketplaceRequest) (*contract.SearchSkillMarketplaceResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 80
	}
	if req.Limit > 200 {
		req.Limit = 200
	}

	// 决定查询哪些源
	queryBuiltin, queryExternal := s.resolveSources(req.SourceTypes)

	keyword := strings.TrimSpace(req.Keyword)
	var externalQuery string

	if keyword == "" {
		if req.Category != "" {
			externalQuery = req.Category
		} else {
			externalQuery = "office"
		}
	} else {
		externalQuery = keyword
	}

	var (
		mu       sync.Mutex
		allItems []contract.SkillMarketplaceItemView
		warnings []contract.SkillSourceWarning
		wg       sync.WaitGroup
	)

	// 内置源：优先排在前面
	if queryBuiltin {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := s.searchBuiltin(ctx, req.Keyword, req.Category, req.Limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, contract.SkillSourceWarning{
					SourceType: "Leros",
					Message:    err.Error(),
				})
			} else {
				allItems = append(allItems, items...)
			}
		}()
	}

	// 外部源（skills.sh）
	if queryExternal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metas, err := fetch.NewSkillsShSource().Search(ctx, externalQuery, req.Limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, contract.SkillSourceWarning{
					SourceType: "Skills.sh",
					Message:    err.Error(),
				})
			} else {
				for _, meta := range metas {
					allItems = append(allItems, metaToView(meta))
				}
			}
		}()
	}

	wg.Wait()

	// 首屏聚合：内置源优先，截断至 limit。
	if len(allItems) > req.Limit {
		allItems = allItems[:req.Limit]
	}

	return &contract.SearchSkillMarketplaceResponse{
		Items:    allItems,
		Warnings: warnings,
	}, nil
}

// resolveSources 根据 source_types 决定查询哪些源。
func (s *skillMarketplaceService) resolveSources(sourceTypes []string) (builtin, external bool) {
	if len(sourceTypes) == 0 {
		return true, true
	}
	for _, t := range sourceTypes {
		switch t {
		case "Leros":
			builtin = true
		case "Skills.sh":
			external = true
		}
	}
	return
}

// searchBuiltin 从数据库查询内置 Skill。
func (s *skillMarketplaceService) searchBuiltin(ctx context.Context, keyword, category string, limit int) ([]contract.SkillMarketplaceItemView, error) {
	items, err := infradb.SearchBuiltinSkills(ctx, s.db, keyword, category, limit)
	if err != nil {
		return nil, err
	}

	result := make([]contract.SkillMarketplaceItemView, 0, len(items))
	for _, item := range items {
		result = append(result, builtinItemToView(item))
	}
	return result, nil
}

// skillMarketplaceItemView constructs a SkillMarketplaceItemView from common fields.
func skillMarketplaceItemView(sourceType, skillID, name, description, version, author, category string, tags []string, icon string, installs int64) contract.SkillMarketplaceItemView {
	return contract.SkillMarketplaceItemView{
		SourceType:  sourceType,
		SkillID:     skillID,
		Name:        name,
		Description: description,
		Version:     version,
		Author:      author,
		Category:    category,
		Tags:        tags,
		Icon:        icon,
		Installs:    installs,
	}
}

func builtinItemToView(item types.BuiltinSkillMarketplaceItem) contract.SkillMarketplaceItemView {
	return skillMarketplaceItemView("Leros", item.SkillID, item.Name, item.Description,
		item.Version, item.Author, item.Category, []string(item.Tags), item.Icon, item.Installs)
}

func metaToView(meta fetch.SkillMeta) contract.SkillMarketplaceItemView {
	return skillMarketplaceItemView(meta.Source, meta.SkillID, meta.Name, meta.Description,
		meta.Version, meta.Author, meta.Category, meta.Tags, meta.Icon, meta.Installs)
}

func (s *skillMarketplaceService) DownloadBuiltinSkill(ctx context.Context, skillID string) (*contract.SkillPackageDownload, error) {
	item, err := infradb.GetBuiltinSkillByID(ctx, s.db, skillID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("skill not found")
	}

	serverDir, err := infradb.ResolveSkillsServerDir()
	if err != nil {
		return nil, fmt.Errorf("resolve skills server dir: %w", err)
	}

	skillDir := filepath.Join(serverDir, skillID)
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); os.IsNotExist(err) {
		return nil, fmt.Errorf("skill %q found in DB but SKILL.md missing on disk", skillID)
	}

	pr, pw := io.Pipe()
	go func() {
		_ = pw.CloseWithError(zipSkillDir(ctx, pw, skillDir))
	}()

	return &contract.SkillPackageDownload{
		Reader:   pr,
		FileName: skillID + ".zip",
	}, nil
}

func zipSkillDir(ctx context.Context, w io.Writer, skillDir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	return filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}

		zipPath := filepath.ToSlash(relPath)

		f, err := zw.Create(zipPath)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}

		_, err = io.Copy(f, file)
		file.Close()
		return err
	})
}

func (s *skillMarketplaceService) InstallSkill(ctx context.Context, req *contract.InstallSkillRequest) (*contract.InstallSkillResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	workerID := uint(1)

	topic, err := dm.WorkerSkillSubject(caller.OrgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-install-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action:  "install",
			Source:  strings.TrimSpace(req.Source),
			SkillID: strings.TrimSpace(req.SkillID),
		},
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		return nil, fmt.Errorf("publish skill install: %w", err)
	}

	return &contract.InstallSkillResponse{
		Status:  "accepted",
		Message: fmt.Sprintf("Skill install request queued for org %d, worker %d", caller.OrgID, workerID),
	}, nil
}

func (s *skillMarketplaceService) InstalledSkills(ctx context.Context, req *contract.InstalledSkillsRequest) (*contract.InstalledSkillsResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	workerID := uint(1)

	topic, err := dm.WorkerSkillSubject(caller.OrgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-list-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action: "list",
		},
	}

	reqCtx, cancel := context.WithTimeout(ctx, skillManagementTimeout)
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

	// Convert response data to contract type
	var skills []contract.SkillInstalledItem
	if err := json.Unmarshal(resp.Data, &skills); err != nil {
		return nil, fmt.Errorf("unmarshal skill list items: %w", err)
	}

	return &contract.InstalledSkillsResponse{Skills: skills}, nil
}

func (s *skillMarketplaceService) UninstallSkill(ctx context.Context, req *contract.UninstallSkillRequest) (*contract.UninstallSkillResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	workerID := uint(1)

	topic, err := dm.WorkerSkillSubject(caller.OrgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-uninstall-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action: "uninstall",
			Name:   strings.TrimSpace(req.Name),
		},
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		return nil, fmt.Errorf("publish skill uninstall: %w", err)
	}

	return &contract.UninstallSkillResponse{
		Status:  "accepted",
		Message: fmt.Sprintf("Skill uninstall request queued for org %d, worker %d", caller.OrgID, workerID),
	}, nil
}

func (s *skillMarketplaceService) GetSkillDetail(ctx context.Context, req *contract.SkillDetailRequest) (*contract.SkillDetailResponse, error) {
	source := strings.TrimSpace(req.Source)
	skillID := strings.TrimSpace(req.SkillID)

	switch source {
	case "Leros":
		return s.getLerosSkillDetail(ctx, skillID)
	case "installed":
		return s.getInstalledSkillDetail(ctx, skillID)
	default:
		return nil, fmt.Errorf("unsupported source: %s", source)
	}
}

// getLerosSkillDetail returns the full detail of a built-in marketplace skill.
func (s *skillMarketplaceService) getLerosSkillDetail(ctx context.Context, skillID string) (*contract.SkillDetailResponse, error) {
	item, err := infradb.GetBuiltinSkillByID(ctx, s.db, skillID)
	if err != nil {
		return nil, fmt.Errorf("query builtin skill: %w", err)
	}
	if item == nil {
		return nil, fmt.Errorf("skill %q not found", skillID)
	}

	serverDir, err := infradb.ResolveSkillsServerDir()
	if err != nil {
		return nil, fmt.Errorf("resolve skills server dir: %w", err)
	}

	skillMDPath := filepath.Join(serverDir, skillID, "SKILL.md")
	skillMDRaw, err := os.ReadFile(skillMDPath)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md for %q: %w", skillID, err)
	}
	// Use catalog.ParseDocument to safely strip YAML frontmatter
	// without false-positive matching on body content like "---".
	skillMDContent := string(skillMDRaw)
	if _, body, parseErr := catalog.ParseDocument(skillMDRaw); parseErr == nil {
		skillMDContent = body
	}

	// Collect files from the skill directory (include SKILL.md as the primary file).
	var files []string
	skillDir := filepath.Join(serverDir, skillID)
	files = append(files, "SKILL.md")
	if entries, readErr := os.ReadDir(skillDir); readErr == nil {
		for _, e := range entries {
			if e.IsDir() || e.Name() == "SKILL.md" {
				continue
			}
			files = append(files, e.Name())
		}
	}

	return &contract.SkillDetailResponse{
		SkillID:     item.SkillID,
		Source:      "Leros",
		Name:        item.Name,
		Description: item.Description,
		SkillMD:     skillMDContent,
		Version:     item.Version,
		Author:      item.Author,
		Category:    item.Category,
		Tags:        []string(item.Tags),
		Icon:        item.Icon,
		Installs:    item.Installs,
		Verified:    item.Verified,
		SourceType:  "Leros",
		Files:       files,
	}, nil
}

// getInstalledSkillDetail sends a NATS request to the worker for installed skill detail.
func (s *skillMarketplaceService) getInstalledSkillDetail(ctx context.Context, skillID string) (*contract.SkillDetailResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	workerID := uint(1)

	topic, err := dm.WorkerSkillSubject(caller.OrgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-detail-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action: "detail",
			Name:   skillID,
		},
	}

	reqCtx, cancel := context.WithTimeout(ctx, skillManagementTimeout)
	defer cancel()
	reply, err := s.publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return nil, fmt.Errorf("request skill detail: %w", err)
	}

	var resp protocol.SkillManagementResponse
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal skill detail response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("skill detail failed: %s", resp.Error)
	}

	var detail protocol.SkillDetailData
	if err := json.Unmarshal(resp.Data, &detail); err != nil {
		return nil, fmt.Errorf("unmarshal skill detail items: %w", err)
	}

	return &contract.SkillDetailResponse{
		SkillID:     detail.Name,
		Source:      "installed",
		Name:        detail.Name,
		Description: detail.Description,
		SkillMD:     detail.SkillMD, // already stripped by catalog.Get in handleDetail
		Version:     detail.Version,
		Author:      detail.Source,
		Category:    detail.Category,
		Tags:        detail.Tags,
		Installs:    0,
		Verified:    detail.Trust == "trusted",
		SourceType:  detail.Source,
		Files:       detail.Files,
	}, nil
}

// ImportSkill 从已上传文件导入 Skill，校验内容后发送给 Worker 异步安装。
func (s *skillMarketplaceService) ImportSkill(ctx context.Context, req *contract.ImportSkillRequest) (*contract.ImportSkillResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	fileUploadID := strings.TrimSpace(req.FileUploadID)

	// 1. 查文件记录
	fileUpload, err := infradb.GetFileUploadByPublicID(ctx, s.db, caller.OrgID, fileUploadID)
	if err != nil {
		return nil, fmt.Errorf("lookup file: %w", err)
	}
	if fileUpload == nil {
		return nil, fmt.Errorf("file not found for file_upload_id %q", fileUploadID)
	}

	// 2. 读文件内容
	reader, _, err := filestore.OpenFileByPublicID(ctx, s.db, caller.OrgID, fileUploadID)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer reader.Close()

	fileBytes, err := io.ReadAll(io.LimitReader(reader, 100_000_000))
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// 3. 按文件类型校验
	lowerName := strings.ToLower(fileUpload.OriginalName)
	switch {
	case strings.HasSuffix(lowerName, ".md"):
		if err := validateSkillMDFromBytes(fileBytes); err != nil {
			return nil, fmt.Errorf("invalid SKILL.md: %w", err)
		}
	case strings.HasSuffix(lowerName, ".zip"):
		if err := validateZipSkill(fileBytes); err != nil {
			return nil, fmt.Errorf("invalid zip: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported file type: only .zip and .md are allowed")
	}

	// 4. 获取 Worker 可访问 URL
	publicURL, err := filestore.ResolvePublicURL(ctx, fileUpload.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("resolve public URL: %w", err)
	}

	// 5. 发送 NATS 消息给 Worker
	workerID := uint(1)
	topic, err := dm.WorkerSkillSubject(caller.OrgID, workerID)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := protocol.SkillManagementMessage{
		ID:        fmt.Sprintf("skill-import-%s", uuid.New().String()),
		Type:      protocol.MessageTypeSkillManagement,
		CreatedAt: time.Now(),
		Route: protocol.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		Body: protocol.SkillManagementBody{
			Action:      "import",
			Source:      "url",
			DownloadURL: publicURL,
		},
	}

	if err := s.publisher.Publish(ctx, topic, msg); err != nil {
		return nil, fmt.Errorf("publish skill import: %w", err)
	}

	return &contract.ImportSkillResponse{
		Status:  "accepted",
		Message: fmt.Sprintf("Skill import request queued for org %d, worker %d", caller.OrgID, workerID),
	}, nil
}

const maxSkillMDFileSize = 1_048_576 // 1MB — consistent with consumer extractZipSkill

// skillManagementTimeout is the deadline for NATS request-reply skill operations.
const skillManagementTimeout = 30 * time.Second

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// validateSkillMDFromBytes 解析原始字节为 SKILL.md 并校验必要字段。
func validateSkillMDFromBytes(raw []byte) error {
	manifest, body, err := catalog.ParseDocument(raw)
	if err != nil {
		return fmt.Errorf("parse SKILL.md: %w", err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("frontmatter must include name")
	}
	if len(manifest.Name) > 64 {
		return fmt.Errorf("skill name exceeds 64 characters")
	}
	if !skillNamePattern.MatchString(manifest.Name) {
		return fmt.Errorf("invalid skill name: use lowercase letters, numbers, hyphens, dots, underscores; start with letter or digit")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		return fmt.Errorf("frontmatter must include description")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("SKILL.md must have content after frontmatter")
	}
	return nil
}

// validateZipSkill 校验 zip 文件的安全性和 SKILL.md 合法性。
func validateZipSkill(zipBytes []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	foundSkillMD := false
	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)

		// 路径穿越检查
		if filepath.IsAbs(name) || strings.Contains(name, "../") {
			return fmt.Errorf("invalid zip entry: path traversal detected (%q)", f.Name)
		}
		clean := filepath.Clean(name)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("invalid zip entry: path traversal detected (%q)", f.Name)
		}

		if f.FileInfo().IsDir() {
			continue
		}

		// 查找 SKILL.md（大小写不敏感）
		base := filepath.Base(name)
		if strings.EqualFold(base, "SKILL.md") {
			foundSkillMD = true
			rc, openErr := f.Open()
			if openErr != nil {
				return fmt.Errorf("open zip entry %q: %w", f.Name, openErr)
			}
			skillBytes, readErr := io.ReadAll(io.LimitReader(rc, maxSkillMDFileSize))
			rc.Close()
			if readErr != nil {
				return fmt.Errorf("read zip entry %q: %w", f.Name, readErr)
			}
			if err := validateSkillMDFromBytes(skillBytes); err != nil {
				return fmt.Errorf("SKILL.md in zip is invalid: %w", err)
			}
		}
	}

	if !foundSkillMD {
		return fmt.Errorf("zip does not contain SKILL.md")
	}
	return nil
}

var _ contract.SkillMarketplaceService = (*skillMarketplaceService)(nil)
