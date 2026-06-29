package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	catalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/insmtx/Leros/backend/types"
)

type skillMarketplaceService struct {
	db         *gorm.DB
	publisher  eventbus.Publisher
	inferrer   AssistantInferrer
	translator SkillDescriptionTranslator
	st         storage.Storage
	bucket     string
}

// NewSkillMarketplaceService 创建 Skill 市场服务。
func NewSkillMarketplaceService(db *gorm.DB, publisher eventbus.Publisher, st storage.Storage, bucket string) contract.SkillMarketplaceService {
	return &skillMarketplaceService{db: db, publisher: publisher, st: st, bucket: bucket}
}

func NewSkillMarketplaceServiceWithInferrer(db *gorm.DB, publisher eventbus.Publisher, inferrer AssistantInferrer, st storage.Storage, bucket string) contract.SkillMarketplaceService {
	return &skillMarketplaceService{db: db, publisher: publisher, inferrer: inferrer, st: st, bucket: bucket}
}

func NewSkillMarketplaceServiceWithTranslator(db *gorm.DB, publisher eventbus.Publisher, inferrer AssistantInferrer, translator SkillDescriptionTranslator, st storage.Storage, bucket string) contract.SkillMarketplaceService {
	return &skillMarketplaceService{db: db, publisher: publisher, inferrer: inferrer, translator: translator, st: st, bucket: bucket}
}

func (s *skillMarketplaceService) SearchSkillMarketplace(ctx context.Context, req *contract.SearchSkillMarketplaceRequest) (*contract.SearchSkillMarketplaceResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 30
	}
	if req.Limit > 200 {
		req.Limit = 200
	}

	// 决定查询哪些源
	queryBuiltin, queryExternal := s.resolveSources(req.SourceTypes)

	keyword := strings.TrimSpace(req.Keyword)

	if keyword == "" {
		keyword = req.Category
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
			items, err := s.searchBuiltin(ctx, keyword, req.Category, req.Limit)
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

	// 外部源（ClawHub）
	if queryExternal {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metas, err := fetch.NewClawHubSource().Search(ctx, keyword, req.Limit)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				warnings = append(warnings, contract.SkillSourceWarning{
					SourceType: "ClawHub",
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

	if queryExternal && len(allItems) < req.Limit {
		fallbackItems, err := s.searchCachedClawHubFallback(ctx, keyword, req.Category, req.Limit, allItems)
		if err != nil {
			warnings = append(warnings, contract.SkillSourceWarning{
				SourceType: "ClawHub",
				Message:    "cache fallback: " + err.Error(),
			})
		} else {
			allItems = append(allItems, fallbackItems...)
		}
	}

	// 缓存查找 + 中文描述替换（best-effort）
	s.resolveCacheAndTranslation(ctx, allItems)

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
		case "ClawHub":
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

func (s *skillMarketplaceService) searchCachedClawHubFallback(ctx context.Context, keyword, category string, limit int, existing []contract.SkillMarketplaceItemView) ([]contract.SkillMarketplaceItemView, error) {
	remaining := limit - len(existing)
	if remaining <= 0 {
		return nil, nil
	}

	cachedItems, err := infradb.ListCachedSkillMarketplaceItems(ctx, s.db, "ClawHub", keyword, category, limit)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(existing)+len(cachedItems))
	for _, item := range existing {
		seen[marketplaceItemDedupeKey(item.SourceType, item.SkillID, item.Version)] = struct{}{}
	}

	result := make([]contract.SkillMarketplaceItemView, 0, remaining)
	for _, item := range cachedItems {
		view := cachedMarketplaceItemToView(item)
		key := marketplaceItemDedupeKey(view.SourceType, view.SkillID, view.Version)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, view)
		if len(result) >= remaining {
			break
		}
	}
	return result, nil
}

func marketplaceItemDedupeKey(sourceType, skillID, version string) string {
	return sourceType + "|" + skillID + "|" + version
}

// skillMarketplaceItemView constructs a SkillMarketplaceItemView from common fields.
func skillMarketplaceItemView(sourceType, skillID, name, description, version, author, category string, tags []string, icon string, installs int64) contract.SkillMarketplaceItemView {
	return contract.SkillMarketplaceItemView{
		SourceType:  sourceType,
		SkillID:     skillID,
		Name:        name,
		DisplayName: "",
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

func cachedMarketplaceItemToView(item types.SkillMarketplaceItem) contract.SkillMarketplaceItemView {
	view := skillMarketplaceItemView(item.Source, item.SkillID, item.Name, item.Description,
		item.Version, item.Author, item.Category, []string(item.Tags), "", item.Installs)
	if strings.TrimSpace(item.TranslatedName) != "" {
		view.DisplayName = item.TranslatedName
	}
	if strings.TrimSpace(item.TranslatedDescription) != "" {
		view.Description = item.TranslatedDescription
	}
	return view
}

// resolveCacheAndTranslation 从缓存表查找中文展示文案，未命中的进行翻译后写库。
func (s *skillMarketplaceService) resolveCacheAndTranslation(ctx context.Context, items []contract.SkillMarketplaceItemView) {
	if len(items) == 0 {
		return
	}

	keys := make([]infradb.CacheKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, infradb.CacheKey{
			Source:  item.SourceType,
			SkillID: item.SkillID,
			Version: item.Version,
		})
	}

	cacheMap, err := infradb.BatchGetSkillMarketplaceItems(ctx, s.db, keys)
	if err != nil {
		logs.WarnContextf(ctx, "resolve cache: batch get failed: %v", err)
		return
	}

	var (
		alreadyChinese []types.SkillMarketplaceItem // 中文文案直接写库
		needTranslate  []TranslateItem              // 需要模型生成中文文案
	)

	for idx := range items {
		item := &items[idx]
		key := fmt.Sprintf("%s|%s|%s", item.SourceType, item.SkillID, item.Version)
		cached, hit := cacheMap[key]

		if hit && cached.TranslatedName != "" {
			item.DisplayName = cached.TranslatedName
		}
		if hit && cached.TranslatedDescription != "" {
			item.Description = cached.TranslatedDescription
		}

		if item.Description == "" {
			continue
		}

		hasChineseDescription := utils.CJKRatio(item.Description) >= cjkTranslationThreshold
		hasDisplayName := strings.TrimSpace(item.DisplayName) != ""
		if hasChineseDescription && hasDisplayName {
			alreadyChinese = append(alreadyChinese, itemToCacheItem(item, TranslatedSkillText{
				DisplayName: item.DisplayName,
				Description: item.Description,
			}))
			continue
		}

		needTranslate = append(needTranslate, TranslateItem{
			SkillID:     translateItemKey(item),
			Name:        item.Name,
			Description: item.Description,
		})
	}

	// 写入已中文条目
	if len(alreadyChinese) > 0 {
		if err := infradb.BatchUpsertSkillMarketplaceItems(ctx, s.db, alreadyChinese); err != nil {
			logs.WarnContextf(ctx, "resolve cache: upsert already-chinese items: %v", err)
		}
	}

	// 翻译并写入
	if s.translator != nil && len(needTranslate) > 0 {
		translationMap, err := s.translator.Translate(ctx, needTranslate)
		if err != nil {
			logs.WarnContextf(ctx, "resolve cache: translate failed: %v", err)
			return
		}
		if len(translationMap) == 0 {
			return
		}

		// 先组装 upsert 条目（此时 Description 还是原文）
		upsertItems := make([]types.SkillMarketplaceItem, 0, len(translationMap))
		for idx := range items {
			item := items[idx]
			if translated, ok := translationMap[translateItemKey(&item)]; ok {
				upsertItems = append(upsertItems, itemToCacheItem(&item, translated))
			}
		}
		if len(upsertItems) > 0 {
			if err := infradb.BatchUpsertSkillMarketplaceItems(ctx, s.db, upsertItems); err != nil {
				logs.WarnContextf(ctx, "resolve cache: upsert translated items: %v", err)
			}
		}

		// 再替换返回给前端的描述
		for idx := range items {
			item := &items[idx]
			if translated, ok := translationMap[translateItemKey(item)]; ok {
				if translated.DisplayName != "" {
					item.DisplayName = translated.DisplayName
				}
				if translated.Description != "" {
					item.Description = translated.Description
				}
			}
		}
	}
}

// itemToCacheItem 将 SkillMarketplaceItemView 转为缓存表记录。
func itemToCacheItem(item *contract.SkillMarketplaceItemView, translated TranslatedSkillText) types.SkillMarketplaceItem {
	tags := types.SkillStringList{}
	if item.Tags != nil {
		tags = types.SkillStringList(item.Tags)
	}
	displayName := translated.DisplayName
	if displayName == "" {
		displayName = item.DisplayName
	}
	translatedDescription := translated.Description
	if translatedDescription == "" && utils.CJKRatio(item.Description) >= cjkTranslationThreshold {
		translatedDescription = item.Description
	}
	return types.SkillMarketplaceItem{
		SkillID:               item.SkillID,
		Name:                  item.Name,
		TranslatedName:        displayName,
		Source:                item.SourceType,
		Description:           item.Description,
		TranslatedDescription: translatedDescription,
		Author:                item.Author,
		Installs:              0,
		Version:               item.Version,
		Category:              item.Category,
		Tags:                  tags,
	}
}

func translateItemKey(item *contract.SkillMarketplaceItemView) string {
	return fmt.Sprintf("%s|%s|%s", item.SourceType, item.SkillID, item.Version)
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

// DownloadSkillPackage 从 storage-go 缓存中下载 Skill 包。
// 只查 DB 的 package_storage_path，不触发远程拉取。
// 未命中时返回错误，调用方可回退到远程拉取。
func (s *skillMarketplaceService) DownloadSkillPackage(ctx context.Context, req *contract.DownloadSkillRequest) (*contract.SkillPackageDownload, error) {
	if req == nil || strings.TrimSpace(req.SkillID) == "" {
		return nil, fmt.Errorf("skill_id is required")
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "Leros"
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "latest"
	}
	skillID := strings.TrimSpace(req.SkillID)

	// 查 DB 缓存记录
	item, err := infradb.GetSkillMarketplaceItemBySourceSkillVersion(ctx, s.db, source, skillID, version)
	if err != nil {
		return nil, fmt.Errorf("query cache record: %w", err)
	}
	if item == nil || item.PackageStoragePath == "" {
		return nil, fmt.Errorf("cached package not found for %s/%s@%s", source, skillID, version)
	}

	// 从 storage-go 读取
	_, bucket, key, err := storage.ParseURI(item.PackageStoragePath)
	if err != nil {
		return nil, fmt.Errorf("parse storage uri: %w", err)
	}
	result, err := s.st.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("get object from storage: %w", err)
	}

	return &contract.SkillPackageDownload{
		Reader:   result.Body,
		FileName: fmt.Sprintf("%s-%s-%s.zip", source, skillID, version),
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

	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}

	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-install-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action:  "install",
			Source:  strings.TrimSpace(req.Source),
			SkillID: strings.TrimSpace(req.SkillID),
			Version: strings.TrimSpace(req.Version),
		},
		"",
	)

	resp, err := s.requestSkillManagement(ctx, topic, msg, "skill install")
	if err != nil {
		return nil, err
	}

	installSource := normalizeMarketplaceSource(req.Source)
	installVersion := strings.TrimSpace(req.Version)
	if installVersion == "" {
		installVersion = "latest"
	}
	if installSource != "" && s.db != nil {
		updated, err := infradb.IncrementSkillMarketplaceInstalls(ctx, s.db, installSource, strings.TrimSpace(req.SkillID), installVersion)
		if err != nil {
			return nil, fmt.Errorf("increment skill installs: %w", err)
		}
		if !updated {
			logs.WarnContextf(ctx, "skip increment installs: marketplace item not found for %s/%s@%s", installSource, strings.TrimSpace(req.SkillID), installVersion)
		}
	}

	message := resp.Message
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("Skill installed for org %d, worker %d", caller.OrgID, workerID)
	}
	return &contract.InstallSkillResponse{
		Status:  "accepted",
		Message: message,
	}, nil
}

func (s *skillMarketplaceService) requestSkillManagement(ctx context.Context, topic string, msg messaging.WorkerCommand, operation string) (*messaging.WorkerCommandResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, skillManagementTimeout)
	defer cancel()
	reply, err := s.publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", operation, err)
	}

	var resp messaging.WorkerCommandResult
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal %s response: %w", operation, err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s failed: %s", operation, resp.Error)
	}
	return &resp, nil
}

func (s *skillMarketplaceService) InstalledSkills(ctx context.Context, req *contract.InstalledSkillsRequest) (*contract.InstalledSkillsResponse, error) {
	skills, err := s.listInstalledSkills(ctx)
	if err != nil {
		return nil, err
	}
	return &contract.InstalledSkillsResponse{Skills: skills}, nil
}

func (s *skillMarketplaceService) listInstalledSkills(ctx context.Context) ([]contract.SkillInstalledItem, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if s.publisher == nil {
		return nil, fmt.Errorf("skill management publisher is nil")
	}

	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}

	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-list-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action: "list",
		},
		"",
	)

	reqCtx, cancel := context.WithTimeout(ctx, skillManagementTimeout)
	defer cancel()
	reply, err := s.publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return nil, fmt.Errorf("request skill list: %w", err)
	}

	var resp messaging.WorkerCommandResult
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal skill list response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("skill list failed: %s", resp.Error)
	}

	// Convert response data to contract type
	var skills []contract.SkillInstalledItem
	rawData, _ := json.Marshal(resp.Data)
	if err := json.Unmarshal(rawData, &skills); err != nil {
		return nil, fmt.Errorf("unmarshal skill list items: %w", err)
	}
	s.enrichInstalledSystemSkills(ctx, skills)

	return skills, nil
}

func (s *skillMarketplaceService) UninstallSkill(ctx context.Context, req *contract.UninstallSkillRequest) (*contract.UninstallSkillResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}

	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-uninstall-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action: "uninstall",
			Name:   strings.TrimSpace(req.Name),
		},
		"",
	)

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
	version := strings.TrimSpace(req.Version)

	// installed 走 NATS worker 路径，不变
	if strings.EqualFold(source, "installed") {
		return s.getInstalledSkillDetail(ctx, skillID)
	}

	// marketplace 路径（Leros / ClawHub）
	normalizedSource := normalizeMarketplaceSource(source)
	if normalizedSource == "" {
		return nil, fmt.Errorf("unsupported source: %s", source)
	}
	if version == "" {
		version = "latest"
	}

	resp, err := s.getMarketplaceSkillDetail(ctx, normalizedSource, skillID, version)
	if err != nil {
		return nil, err
	}
	s.annotateMarketplaceInstalled(ctx, resp)
	return resp, nil
}

func (s *skillMarketplaceService) annotateMarketplaceInstalled(ctx context.Context, detail *contract.SkillDetailResponse) {
	if detail == nil {
		return
	}
	if strings.EqualFold(detail.Source, "installed") {
		detail.Installed = true
		return
	}
	skills, err := s.listInstalledSkills(ctx)
	if err != nil {
		logs.WarnContextf(ctx, "query installed skills for detail %s/%s failed: %v", detail.Source, detail.SkillID, err)
		return
	}
	detail.Installed = skillDetailIsInstalled(detail, skills)
}

func skillDetailIsInstalled(detail *contract.SkillDetailResponse, skills []contract.SkillInstalledItem) bool {
	if detail == nil {
		return false
	}
	candidates := map[string]struct{}{}
	if name := strings.TrimSpace(detail.Name); name != "" {
		candidates[strings.ToLower(name)] = struct{}{}
	}
	if skillID := strings.TrimSpace(detail.SkillID); skillID != "" {
		candidates[strings.ToLower(skillID)] = struct{}{}
	}
	if len(candidates) == 0 {
		return false
	}
	for _, skill := range skills {
		name := strings.ToLower(strings.TrimSpace(skill.Name))
		if _, ok := candidates[name]; ok {
			return true
		}
	}
	return false
}

// getMarketplaceSkillDetail 统一处理 marketplace skill 详情查询。
// 先查 leros_skill_marketplace_item 缓存表，有缓存从 storage 读，无缓存回填。
func (s *skillMarketplaceService) getMarketplaceSkillDetail(ctx context.Context, source, skillID, version string) (*contract.SkillDetailResponse, error) {
	// 1. 尝试查缓存表
	item, cacheErr := infradb.GetSkillMarketplaceItemBySourceSkillVersion(ctx, s.db, source, skillID, version)
	if cacheErr != nil {
		logs.WarnContextf(ctx, "query marketplace cache for %s/%s@%s: %v", source, skillID, version, cacheErr)
	}

	// 2. 有缓存路径 → 从 storage 读
	if item != nil && item.PackageStoragePath != "" && s.st != nil {
		resp, err := s.readDetailFromCache(ctx, item, source)
		if err == nil {
			return resp, nil
		}
		logs.WarnContextf(ctx, "read cached package for %s/%s@%s failed: %v, fallback to refill", source, skillID, version, err)
	}

	// 3. 无缓存或读取失败 → 回填
	return s.refillMarketplaceSkillDetail(ctx, source, skillID, version, item)
}

// readDetailFromCache 从 storage 读取 detail 并组装响应。
// 优先读取 SKILL.zh-CN.md（缓存同目录），未命中时回退读取 package.zip 内 SKILL.md。
// 元数据来自表记录 item，文件列表来自 zip。
func (s *skillMarketplaceService) readDetailFromCache(ctx context.Context, item *types.SkillMarketplaceItem, source string) (*contract.SkillDetailResponse, error) {
	description := item.Description
	if item.TranslatedDescription != "" {
		description = item.TranslatedDescription
	}

	// 始终从 zip 读取以获取完整文件列表。SKILL.zh-CN.md 仅用于覆盖正文内容。
	zipBody, files, rawZip, err := skillcache.ReadPackageFromStorage(ctx, s.st, item.PackageStoragePath)
	if err != nil {
		return nil, err
	}
	skillMDBody := zipBody

	// 1. 优先使用 SKILL.zh-CN.md 覆盖正文和描述
	hasChineseDoc := false
	if item.PackageStoragePath != "" {
		zhBody, zhDesc, zhErr := skillcache.ReadChineseDocumentFromStorage(ctx, s.st, item.PackageStoragePath)
		if zhErr == nil && zhBody != "" {
			skillMDBody = zhBody
			if zhDesc != "" {
				description = zhDesc
			}
			hasChineseDoc = true
		} else {
			logs.WarnContextf(ctx, "read SKILL.zh-CN.md for %s/%s@%s: %v", source, item.SkillID, item.Version, zhErr)
		}
	}

	// 2. 正文非中文时，同步翻译（但已有 SKILL.zh-CN.md 时跳过，避免二次翻译）
	if !hasChineseDoc && (skillMDBody == "" || utils.CJKRatioMarkdown(skillMDBody) < cjkTranslationThreshold) {
		if skillMDBody == "" {
			skillMDBody = zipBody
		}
		if s.translator != nil {
			// 从 rawZip 中提取完整 SKILL.md（含 frontmatter）用于翻译
			fullSkillMD := skillcache.ExtractSkillMDFromZip(rawZip)
			if fullSkillMD != "" {
				if translatedBody, zhDesc, tErr := s.translateBodyAndCache(ctx, item, fullSkillMD); tErr == nil {
					skillMDBody = translatedBody
					if zhDesc != "" {
						description = zhDesc
					}
				}
			}
		}
	}

	return &contract.SkillDetailResponse{
		SkillID:     item.SkillID,
		Source:      source,
		Name:        item.Name,
		DisplayName: item.TranslatedName,
		Description: description,
		SkillMD:     skillMDBody,
		Version:     item.Version,
		Author:      item.Author,
		Category:    item.Category,
		Tags:        item.Tags,
		Icon:        "",
		Installs:    item.Installs,
		Verified:    source == "Leros",
		SourceType:  source,
		Files:       files,
	}, nil
}

// refillMarketplaceSkillDetail 回填：远程/本地拉取后写缓存并返回。
// switch 只做数据获取，Eager 翻译和异步写缓存在 switch 后统一执行。
func (s *skillMarketplaceService) refillMarketplaceSkillDetail(ctx context.Context, source, skillID, version string, existingItem *types.SkillMarketplaceItem) (*contract.SkillDetailResponse, error) {
	var resp *contract.SkillDetailResponse
	var bundle *fetch.SkillBundle

	switch source {
	case "Leros":
		r, b, err := s.getLerosSkillDetailWithBundle(ctx, skillID)
		if err != nil {
			return nil, err
		}
		resp, bundle = r, b

	case "ClawHub":
		detail, b, err := s.getClawHubSkillDetailWithBundle(ctx, skillID, version)
		if err != nil {
			return nil, err
		}
		bundle = b
		resp = &contract.SkillDetailResponse{
			SkillID:     detail.SkillID,
			Source:      "ClawHub",
			Name:        detail.Name,
			Description: detail.Description,
			SkillMD:     detail.SkillMD,
			Version:     detail.Version,
			Author:      detail.Author,
			Category:    detail.Category,
			Tags:        detail.Tags,
			Icon:        "",
			Installs:    0,
			Verified:    false,
			SourceType:  "ClawHub",
			Files:       detail.Files,
		}

	default:
		return nil, fmt.Errorf("unsupported source: %s", source)
	}

	// 统一 version fallback
	if resp.Version == "" {
		resp.Version = "latest"
	}
	if existingItem != nil && existingItem.TranslatedName != "" {
		resp.DisplayName = existingItem.TranslatedName
	}

	// 统一 eager 翻译：有 translator、原文非中文时同步翻译
	needEagerTranslate := s.translator != nil && bundle != nil &&
		len(bundle.Content) > 0 &&
		utils.CJKRatioMarkdown(string(bundle.Content)) < cjkTranslationThreshold

	var translatedContent string // eager translate 的完整翻译结果，用于异步缓存写入

	if needEagerTranslate {
		translationMap, tErr := s.translator.TranslateDocument(ctx, []TranslateDocumentItem{
			{SkillID: skillID, Content: string(bundle.Content)},
		})
		if tErr == nil {
			if translated, ok := translationMap[skillID]; ok && translated != "" {
				if manifest, body, pErr := catalog.ParseDocument([]byte(translated)); pErr == nil {
					resp.SkillMD = body
					if manifest.Description != "" {
						resp.Description = manifest.Description
					}
					translatedContent = translated // 保存完整翻译结果，供异步缓存复用
				}
			}
		} else {
			logs.WarnContextf(ctx, "refill: eager translate SKILL.md for %s/%s@%s: %v", source, skillID, resp.Version, tErr)
		}
	}

	// Description fallback：eager 翻译未命中时，用已有的 translated_description
	if existingItem != nil && existingItem.TranslatedDescription != "" &&
		(resp.Description == "" || !needEagerTranslate) {
		resp.Description = existingItem.TranslatedDescription
	}

	// 统一异步写缓存
	if s.st != nil && s.db != nil && bundle != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logs.WarnContextf(ctx, "cache write panic for %s/%s@%s: %v", source, skillID, resp.Version, r)
				}
			}()
			// 缓存写入完成后清理 TempDir（由 clawhub.GetDetail 创建的临时目录）
			if bundle.TempDir != "" {
				defer os.RemoveAll(bundle.TempDir)
			}
			uri := skillcache.CachePackage(ctx, s.st, s.bucket, s.db, source, skillID, resp.Version, bundle)
			if uri != "" {
				if translatedContent != "" {
					// 使用 eager translate 的结果，避免重复 LLM 调用
					s.cacheChineseDocumentWithTranslated(ctx, source, skillID, resp.Version, uri, translatedContent)
				} else {
					s.cacheChineseDocument(ctx, source, skillID, resp.Version, uri, bundle)
				}
			}
		}()
	}

	return resp, nil
}

// getLerosSkillDetailWithBundle 从本地文件读取 Leros 内置 skill 详情，同时返回 bundle 用于写缓存。
func (s *skillMarketplaceService) getLerosSkillDetailWithBundle(ctx context.Context, skillID string) (*contract.SkillDetailResponse, *fetch.SkillBundle, error) {
	item, err := infradb.GetBuiltinSkillByID(ctx, s.db, skillID)
	if err != nil {
		return nil, nil, fmt.Errorf("query builtin skill: %w", err)
	}
	if item == nil {
		return nil, nil, fmt.Errorf("skill %q not found", skillID)
	}

	serverDir, err := infradb.ResolveSkillsServerDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve skills server dir: %w", err)
	}

	skillMDPath := filepath.Join(serverDir, skillID, "SKILL.md")
	skillMDRaw, err := os.ReadFile(skillMDPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read SKILL.md for %q: %w", skillID, err)
	}

	// 去 frontmatter
	skillMDContent := string(skillMDRaw)
	if _, body, parseErr := catalog.ParseDocument(skillMDRaw); parseErr == nil {
		skillMDContent = body
	}

	// 收集文件列表
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

	// 构建 bundle 用于写缓存
	bundle := buildBundleFromLocalSkill(skillDir, skillID, item)

	resp := &contract.SkillDetailResponse{
		SkillID:     item.SkillID,
		Source:      "Leros",
		Name:        item.Name,
		DisplayName: "",
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
	}
	return resp, bundle, nil
}

// getClawHubSkillDetailWithBundle 从 ClawHub 远程获取 skill 详情，同时返回 bundle 用于写缓存。
func (s *skillMarketplaceService) getClawHubSkillDetailWithBundle(ctx context.Context, skillID, version string) (*fetch.SkillDetail, *fetch.SkillBundle, error) {
	clawhub := fetch.NewClawHubSource()

	// GetDetail 通过 GET /api/v1/skills/{slug} 获取 Author 等元数据，
	// 内部只下载一次 zip（FetchVersion），已同时返回 bundle，无需再次下载。
	detail, bundle, err := clawhub.GetDetail(ctx, skillID, version)
	if err != nil {
		return nil, nil, fmt.Errorf("clawhub skill detail: %w", err)
	}

	return detail, bundle, nil
}

// getInstalledSkillDetail sends a NATS request to the worker for installed skill detail.
func (s *skillMarketplaceService) getInstalledSkillDetail(ctx context.Context, skillID string) (*contract.SkillDetailResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}

	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-detail-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action: "detail",
			Name:   skillID,
		},
		"",
	)

	reqCtx, cancel := context.WithTimeout(ctx, skillManagementTimeout)
	defer cancel()
	reply, err := s.publisher.Request(reqCtx, topic, msg)
	if err != nil {
		return nil, fmt.Errorf("request skill detail: %w", err)
	}

	var resp messaging.WorkerCommandResult
	if err := json.Unmarshal(reply.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal skill detail response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("skill detail failed: %s", resp.Error)
	}

	rawDetail, _ := json.Marshal(resp.Data)
	var detail messaging.SkillDetailData
	if err := json.Unmarshal(rawDetail, &detail); err != nil {
		return nil, fmt.Errorf("unmarshal skill detail items: %w", err)
	}

	detailResp := &contract.SkillDetailResponse{
		SkillID:     detail.Name,
		Source:      "installed",
		Name:        detail.Name,
		DisplayName: "",
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
	}
	s.enrichInstalledSystemSkillDetail(ctx, detailResp)
	return detailResp, nil
}

func (s *skillMarketplaceService) enrichInstalledSystemSkills(ctx context.Context, skills []contract.SkillInstalledItem) {
	if len(skills) == 0 || s.db == nil {
		return
	}

	names := make([]string, 0, len(skills))
	for _, item := range skills {
		name := strings.TrimSpace(item.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return
	}

	builtinByID, err := s.builtinItemsBySkillID(ctx, names)
	if err != nil {
		logs.WarnContextf(ctx, "enrich installed system skills: query builtin skills: %v", err)
		return
	}
	if len(builtinByID) == 0 {
		return
	}

	views := make([]contract.SkillMarketplaceItemView, 0, len(builtinByID))
	for _, name := range names {
		if item, ok := builtinByID[name]; ok {
			views = append(views, builtinItemToView(item))
		}
	}
	s.resolveCacheAndTranslation(ctx, views)

	viewByID := make(map[string]contract.SkillMarketplaceItemView, len(views))
	for _, view := range views {
		viewByID[view.SkillID] = view
	}

	for idx := range skills {
		view, ok := viewByID[skills[idx].Name]
		if !ok {
			continue
		}
		skills[idx].DisplayName = view.DisplayName
		if view.Description != "" {
			skills[idx].Description = view.Description
		}
		if skills[idx].Category == "" {
			skills[idx].Category = view.Category
		}
	}
}

func (s *skillMarketplaceService) enrichInstalledSystemSkillDetail(ctx context.Context, detail *contract.SkillDetailResponse) {
	if detail == nil || s.db == nil {
		return
	}

	builtinByID, err := s.builtinItemsBySkillID(ctx, []string{detail.Name})
	if err != nil {
		logs.WarnContextf(ctx, "enrich installed system skill detail: query builtin skill: %v", err)
		return
	}
	item, ok := builtinByID[detail.Name]
	if !ok {
		return
	}

	views := []contract.SkillMarketplaceItemView{builtinItemToView(item)}
	s.resolveCacheAndTranslation(ctx, views)
	if len(views) == 0 {
		return
	}
	detail.DisplayName = views[0].DisplayName
	if views[0].Description != "" {
		detail.Description = views[0].Description
	}
	if detail.Category == "" {
		detail.Category = views[0].Category
	}
}

func (s *skillMarketplaceService) builtinItemsBySkillID(ctx context.Context, skillIDs []string) (map[string]types.BuiltinSkillMarketplaceItem, error) {
	result := make(map[string]types.BuiltinSkillMarketplaceItem)
	if len(skillIDs) == 0 {
		return result, nil
	}

	unique := make([]string, 0, len(skillIDs))
	seen := make(map[string]bool, len(skillIDs))
	for _, id := range skillIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return result, nil
	}

	var items []types.BuiltinSkillMarketplaceItem
	if err := s.db.WithContext(ctx).
		Where("skill_id IN ? AND status = ?", unique, "active").
		Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.SkillID] = item
	}
	return result, nil
}

// ImportSkill 从已上传文件导入 Skill，校验内容后发送给 Worker 并等待完成。
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
			logs.WarnContextf(ctx, "skill import validation failed: invalid SKILL.md: %v", err)
			return nil, errors.New(localizeSkillImportError(fmt.Errorf("invalid SKILL.md: %w", err)))
		}
	case strings.HasSuffix(lowerName, ".zip"):
		if err := validateZipSkill(fileBytes); err != nil {
			logs.WarnContextf(ctx, "skill import validation failed: invalid zip: %v", err)
			return nil, errors.New(localizeSkillImportError(fmt.Errorf("invalid zip: %w", err)))
		}
	default:
		return nil, errors.New(localizeSkillImportError(fmt.Errorf("unsupported file type: only .zip and .md are allowed")))
	}

	// 4. 获取 Worker 可访问 URL
	publicURL, err := filestore.ResolvePublicURL(ctx, fileUpload.StorageURI)
	if err != nil {
		return nil, fmt.Errorf("resolve public URL: %w", err)
	}

	// 5. 发送 NATS 消息给 Worker
	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}
	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-import-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action:      "import",
			Source:      "url",
			DownloadURL: publicURL,
		},
		"",
	)

	resp, err := s.requestSkillManagement(ctx, topic, msg, "skill import")
	if err != nil {
		logs.WarnContextf(ctx, "skill import worker request failed: %v", err)
		return nil, errors.New(localizeSkillImportError(err))
	}
	message := resp.Message
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("Skill imported for org %d, worker %d", caller.OrgID, workerID)
	}

	return &contract.ImportSkillResponse{
		Status:  "imported",
		Message: message,
	}, nil
}

// ImportSkillFromGitHub imports a GitHub skill on the default worker and waits for completion.
func (s *skillMarketplaceService) ImportSkillFromGitHub(ctx context.Context, req *contract.ImportSkillFromGitHubRequest) (*contract.ImportSkillResponse, error) {
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}

	skillID, version, err := parseGitHubSkillImportURL(req.GitHubURL)
	if err != nil {
		return nil, errors.New(localizeSkillImportError(err))
	}

	_, workerID, err := resolveDefaultRuntimeWorker(ctx, s.db, caller.OrgID, s.inferrer)
	if err != nil {
		return nil, err
	}
	topic, err := messaging.WorkerCommandSubject(caller.OrgID, workerID, messaging.LaneSkill)
	if err != nil {
		return nil, fmt.Errorf("build skill topic: %w", err)
	}

	msg := messaging.NewSkillCommand(
		fmt.Sprintf("skill-import-github-%s", uuid.New().String()),
		messaging.RouteContext{
			OrgID:    caller.OrgID,
			WorkerID: workerID,
		},
		messaging.SkillCommandPayload{
			Action:  "import",
			Source:  "github",
			SkillID: skillID,
			Version: version,
		},
		"",
	)

	resp, err := s.requestSkillManagement(ctx, topic, msg, "GitHub skill import")
	if err != nil {
		logs.WarnContextf(ctx, "GitHub skill import worker request failed: %v", err)
		return nil, errors.New(localizeSkillImportError(err))
	}
	message := resp.Message
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("GitHub skill imported for org %d, worker %d", caller.OrgID, workerID)
	}

	return &contract.ImportSkillResponse{
		Status:  "imported",
		Message: message,
	}, nil
}

func parseGitHubSkillImportURL(raw string) (skillID string, version string, err error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", "", fmt.Errorf("github_url is required")
	}

	if !strings.Contains(input, "://") {
		parts := splitCleanPath(input)
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid GitHub skill path %q: expected owner/repo/path", raw)
		}
		if len(parts) == 2 {
			skillID, err := normalizeGitHubSkillIdentifier(parts[0], parts[1], ".")
			return skillID, "", err
		}
		skillID, err := normalizeGitHubSkillIdentifier(parts[0], parts[1], strings.Join(parts[2:], "/"))
		return skillID, "", err
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return "", "", fmt.Errorf("invalid GitHub URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	parts := splitCleanPath(parsed.Path)

	switch host {
	case "github.com", "www.github.com":
		return parseGitHubWebPath(parts, raw)
	case "raw.githubusercontent.com":
		return parseGitHubRawPath(parts, raw)
	default:
		return "", "", fmt.Errorf("unsupported GitHub URL host %q", parsed.Hostname())
	}
}

func parseGitHubWebPath(parts []string, raw string) (string, string, error) {
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL %q: expected owner/repo path", raw)
	}
	owner, repo := parts[0], parts[1]
	if len(parts) < 3 {
		skillID, err := normalizeGitHubSkillIdentifier(owner, repo, ".")
		return skillID, "", err
	}

	switch parts[2] {
	case "tree":
		if len(parts) == 4 {
			ref := parts[3]
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, ".")
			return skillID, ref, err
		}
		if len(parts) >= 5 {
			ref := parts[3]
			skillPath := strings.Join(parts[4:], "/")
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
			return skillID, ref, err
		}
	case "blob":
		if len(parts) >= 5 {
			ref := parts[3]
			skillFilePath := strings.Join(parts[4:], "/")
			skillPath, err := skillDirFromSkillMDPath(skillFilePath)
			if err != nil {
				return "", "", err
			}
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
			return skillID, ref, err
		}
	}
	return "", "", fmt.Errorf("unsupported GitHub URL %q: use a repository root, a tree link to a skill directory, or a blob link to SKILL.md", raw)
}

func parseGitHubRawPath(parts []string, raw string) (string, string, error) {
	if len(parts) < 4 {
		return "", "", fmt.Errorf("invalid raw GitHub URL %q: expected owner/repo/ref/path/SKILL.md", raw)
	}
	owner, repo, ref := parts[0], parts[1], parts[2]
	skillFilePath := strings.Join(parts[3:], "/")
	skillPath, err := skillDirFromSkillMDPath(skillFilePath)
	if err != nil {
		return "", "", err
	}
	skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
	return skillID, ref, err
}

func normalizeGitHubSkillIdentifier(owner, repo, skillPath string) (string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	skillPath = strings.Trim(strings.TrimSpace(skillPath), "/")
	if owner == "" || repo == "" || skillPath == "" {
		return "", fmt.Errorf("invalid GitHub skill identifier: owner, repo, and skill path are required")
	}
	if strings.EqualFold(path.Base(skillPath), "SKILL.md") {
		dir, err := skillDirFromSkillMDPath(skillPath)
		if err != nil {
			return "", err
		}
		skillPath = dir
	}
	return owner + "/" + repo + "/" + skillPath, nil
}

func skillDirFromSkillMDPath(skillFilePath string) (string, error) {
	clean := strings.Trim(path.Clean(strings.TrimSpace(skillFilePath)), "/")
	if clean == "." || clean == "" || !strings.EqualFold(path.Base(clean), "SKILL.md") {
		return "", fmt.Errorf("GitHub blob/raw link must point to SKILL.md")
	}
	dir := path.Dir(clean)
	if dir == "." || dir == "" {
		return ".", nil
	}
	return dir, nil
}

func splitCleanPath(rawPath string) []string {
	cleaned := strings.Trim(rawPath, "/")
	if cleaned == "" {
		return nil
	}
	rawParts := strings.Split(cleaned, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

const (
	maxSkillMDFileSize      = 1_048_576 // 1MB — consistent with consumer extractZipSkill
	skillManagementTimeout  = 30 * time.Second
	cjkTranslationThreshold = 0.35 // CJK 字符占比阈值：达到此比例视为已中文，不再翻译
)

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

func localizeSkillImportError(err error) string {
	if err == nil {
		return "技能导入失败，请稍后重试"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "技能导入处理中超时，请稍后查看是否已安装，或重试导入"
	}

	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "open zip"):
		return "技能包文件损坏，请重新导出或重新下载后再试"
	case strings.Contains(lower, "zip does not contain skill.md") ||
		strings.Contains(lower, "skill.md not found"):
		return "技能包中未找到 SKILL.md，请确认上传的是技能目录或技能压缩包"
	case strings.Contains(lower, "multiple skill.md"):
		return "仓库中包含多个 SKILL.md，请使用具体技能目录 tree 链接或 SKILL.md blob/raw 链接"
	case strings.Contains(lower, "unsupported github url") ||
		strings.Contains(lower, "invalid github skill path") ||
		strings.Contains(lower, "invalid github url") ||
		strings.Contains(lower, "unsupported github url host"):
		return "GitHub 链接不支持，请使用技能目录 tree 链接或 SKILL.md blob/raw 链接"
	case strings.Contains(lower, "github blob/raw link must point to skill.md"):
		return "GitHub 链接必须指向 SKILL.md 文件"
	case strings.Contains(lower, "fetch github skill") ||
		strings.Contains(lower, "download github skill"):
		return "GitHub 技能下载失败，请检查链接或网络后重试"
	case strings.Contains(lower, "parse skill.md") ||
		strings.Contains(lower, "invalid skill.md") ||
		strings.Contains(lower, "skill.md in zip is invalid") ||
		strings.Contains(lower, "frontmatter") ||
		strings.Contains(lower, "skill name") ||
		strings.Contains(lower, "skill.md must have content"):
		return "SKILL.md 格式错误：" + localizeSkillDocumentDetail(msg)
	case strings.Contains(lower, "install skill"):
		return "技能安装失败：" + cleanWorkerImportDetail(msg)
	case strings.Contains(lower, "request skill import") ||
		strings.Contains(lower, "request github skill import"):
		return "技能导入请求失败：" + cleanWorkerImportDetail(msg)
	case strings.Contains(lower, "unsupported file type"):
		return "仅支持导入 .zip 或 .md 格式的技能文件"
	default:
		return "技能导入失败：" + cleanWorkerImportDetail(msg)
	}
}

func localizeSkillDocumentDetail(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "frontmatter must include name"):
		return "frontmatter 必须包含 name"
	case strings.Contains(lower, "frontmatter must include description"):
		return "frontmatter 必须包含 description"
	case strings.Contains(lower, "skill name exceeds"):
		return "技能名称不能超过 64 个字符"
	case strings.Contains(lower, "invalid skill name"):
		return "技能名称只能使用小写字母、数字、连字符、点和下划线，并且必须以字母或数字开头"
	case strings.Contains(lower, "skill.md must have content"):
		return "frontmatter 后必须包含技能说明正文"
	case strings.Contains(lower, "parse skill.md"):
		return "无法解析 frontmatter 或 Markdown 内容"
	default:
		return cleanWorkerImportDetail(msg)
	}
}

func cleanWorkerImportDetail(msg string) string {
	detail := strings.TrimSpace(msg)
	prefixes := []string{
		"GitHub skill import failed:",
		"skill import failed:",
		"fetch GitHub skill:",
		"extract zip skill:",
		"parse SKILL.md:",
		"install skill:",
		"request GitHub skill import:",
		"request skill import:",
		"invalid SKILL.md:",
		"invalid zip:",
	}
	for _, prefix := range prefixes {
		detail = strings.TrimSpace(strings.TrimPrefix(detail, prefix))
	}
	if detail == "" {
		return "未知错误"
	}
	return detail
}

// buildBundleFromLocalSkill 从本地 skill 目录构建 *fetch.SkillBundle。
// 用于 Leros 内置 skill 写入缓存。返回 nil 表示构建失败（已记录 warning）。
func buildBundleFromLocalSkill(skillDir, skillID string, _ *types.BuiltinSkillMarketplaceItem) *fetch.SkillBundle {
	content, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		logs.Warnf("build bundle for %s: read SKILL.md: %v", skillID, err)
		return nil
	}

	files := make(map[string][]byte)
	allowedSubdirs := map[string]bool{"assets": true, "references": true, "scripts": true, "templates": true}
	filepath.Walk(skillDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(skillDir, path)
		if relErr != nil {
			return nil
		}
		if rel == "SKILL.md" {
			return nil
		}
		topDir, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
		if !allowedSubdirs[topDir] {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr == nil && len(data) <= 1_048_576 {
			files[filepath.ToSlash(rel)] = data
		}
		return nil
	})

	return &fetch.SkillBundle{
		Content: content,
		Files:   files,
	}
}

// normalizeMarketplaceSource 统一 source 名称做缓存表查询。
func normalizeMarketplaceSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "leros":
		return "Leros"
	case "clawhub":
		return "ClawHub"
	default:
		return ""
	}
}

// translateBodyAndCache 同步翻译 SKILL.md 内容，更新返回值，并异步写入 SKILL.zh-CN.md。
// fullSkillMD 为完整 SKILL.md 内容（含 frontmatter）。
// 返回翻译后的 body（不含 frontmatter）和 description。
func (s *skillMarketplaceService) translateBodyAndCache(ctx context.Context, item *types.SkillMarketplaceItem, fullSkillMD string) (body string, description string, err error) {
	translationMap, tErr := s.translator.TranslateDocument(ctx, []TranslateDocumentItem{
		{SkillID: item.SkillID, Content: fullSkillMD},
	})
	if tErr != nil {
		logs.WarnContextf(ctx, "translateBodyAndCache: TranslateDocument for %s/%s@%s: %v", item.Source, item.SkillID, item.Version, tErr)
		return "", "", tErr
	}

	translated, ok := translationMap[item.SkillID]
	if !ok || translated == "" {
		return "", "", fmt.Errorf("translate returned empty result for %s", item.SkillID)
	}

	manifest, bodyContent, pErr := catalog.ParseDocument([]byte(translated))
	if pErr != nil {
		logs.WarnContextf(ctx, "translateBodyAndCache: ParseDocument for %s/%s@%s: %v", item.Source, item.SkillID, item.Version, pErr)
		return "", "", pErr
	}

	// 异步写入 SKILL.zh-CN.md，直接使用本次翻译结果，不重复调用 LLM
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logs.WarnContextf(ctx, "translateBodyAndCache: write SKILL.zh-CN.md panic for %s/%s@%s: %v", item.Source, item.SkillID, item.Version, r)
			}
		}()
		s.cacheChineseDocumentWithContent(ctx, item, translated, manifest.Description)
	}()

	return bodyContent, manifest.Description, nil
}

// cacheChineseDocumentWithContent 使用已有的翻译内容写入 SKILL.zh-CN.md。
// 与 cacheChineseDocument 不同，该方法直接使用 preTranslatedContent，不调用 LLM。
func (s *skillMarketplaceService) cacheChineseDocumentWithContent(ctx context.Context, item *types.SkillMarketplaceItem, chineseContent string, zhDescription string) {
	if s.st == nil || s.db == nil || item.PackageStoragePath == "" || chineseContent == "" {
		return
	}
	skillcache.CacheChineseDocumentWithContent(ctx, s.st, s.bucket, s.db, item.Source, item.SkillID, item.Version, item.PackageStoragePath, chineseContent, zhDescription)
}

// cacheChineseDocumentWithTranslated 使用 eager translate 的翻译结果写入 SKILL.zh-CN.md。
// 与 cacheChineseDocument 不同，该方法直接使用传入的完整翻译内容，不调用 LLM。
func (s *skillMarketplaceService) cacheChineseDocumentWithTranslated(ctx context.Context, source, skillID, version, packageURI string, translatedContent string) {
	if s.st == nil || s.db == nil || packageURI == "" || translatedContent == "" {
		return
	}

	// 从翻译内容中提取 description
	zhDescription := ""
	if manifest, _, pErr := catalog.ParseDocument([]byte(translatedContent)); pErr == nil {
		zhDescription = manifest.Description
	}

	// 写入 storage
	chineseKey := skillcache.SkillChineseCacheKey(source, skillID, version)
	_, cErr := s.st.PutObject(ctx, s.bucket, chineseKey, strings.NewReader(translatedContent),
		storage.WithContentType("text/markdown; charset=utf-8"),
	)
	if cErr != nil {
		logs.WarnContextf(ctx, "cache chinese doc: put object failed for %s/%s@%s: %v", source, skillID, version, cErr)
		return
	}

	logs.Infof("cache chinese doc: written SKILL.zh-CN.md for %s/%s@%s", source, skillID, version)

	// 回写 DB
	if err := infradb.UpdateSkillMarketplaceTranslatedDescription(ctx, s.db, source, skillID, version, zhDescription); err != nil {
		logs.WarnContextf(ctx, "cache chinese doc: update db metadata for %s/%s@%s: %v", source, skillID, version, err)
	}
}

// cacheChineseDocument 将 SKILL.zh-CN.md 写入 storage。
// 内部错误只记 warning，不影响主流程。
func (s *skillMarketplaceService) cacheChineseDocument(ctx context.Context, source, skillID, version, packageURI string, bundle *fetch.SkillBundle) {
	if s.st == nil || s.db == nil || bundle == nil || packageURI == "" {
		return
	}

	// 构建 ChineseWriter
	translateFn := func(ctx context.Context, content string) (string, string, error) {
		if s.translator == nil {
			return "", "", fmt.Errorf("translator not available")
		}
		result, tErr := s.translator.TranslateDocument(ctx, []TranslateDocumentItem{
			{SkillID: skillID, Content: content},
		})
		if tErr != nil {
			return "", "", tErr
		}
		translated, ok := result[skillID]
		if !ok || translated == "" {
			return "", "", fmt.Errorf("translate returned empty result")
		}
		// 提取 translated description
		if manifest, _, pErr := catalog.ParseDocument([]byte(translated)); pErr == nil {
			return translated, manifest.Description, nil
		}
		return translated, "", nil
	}

	skillcache.CacheChineseDocument(ctx, s.st, s.bucket, s.db, source, skillID, version, packageURI, bundle, translateFn)
}

var _ contract.SkillMarketplaceService = (*skillMarketplaceService)(nil)
