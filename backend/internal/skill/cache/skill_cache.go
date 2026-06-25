// Package cache 提供 Skill 包的 storage-go 缓存能力。
//
// Server 端在 GetSkillDetail 时异步生成标准化 zip，写入 storage-go，
// 并将 URI 回写到 DB 的 leros_skill_marketplace_item.package_storage_path。
// Worker/CLI 安装时优先从该缓存读取，失败回退远程拉取。
package cache

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	"github.com/insmtx/Leros/backend/pkg/utils"
	"github.com/insmtx/Leros/backend/types"
)

// SkillCacheKey 生成统一的 storage-go object key。
// 格式: skills/marketplace/{source}/{skill_id}/{version}/skill/package.zip
func SkillCacheKey(source, skillID, version string) string {
	if version == "" || version == "latest" {
		version = "latest"
	}
	return fmt.Sprintf("skills/marketplace/%s/%s/%s/skill/package.zip",
		url.PathEscape(source),
		url.PathEscape(skillID),
		url.PathEscape(version),
	)
}

// SkillChineseCacheKey 生成 SKILL.zh-CN.md 的 storage-go object key。
// 与 package.zip 同级，格式: skills/marketplace/{source}/{skill_id}/{version}/skill/SKILL.zh-CN.md
func SkillChineseCacheKey(source, skillID, version string) string {
	if version == "" || version == "latest" {
		version = "latest"
	}
	return fmt.Sprintf("skills/marketplace/%s/%s/%s/skill/SKILL.zh-CN.md",
		url.PathEscape(source),
		url.PathEscape(skillID),
		url.PathEscape(version),
	)
}

// ParseChineseCacheKeyFromPackageURI 从 package.zip 的 storage-go URI 推导出同目录下
// SKILL.zh-CN.md 的 object key。例如:
//
//	s3://bucket/skills/marketplace/ClawHub/foo/1.0.0/skill/package.zip
//	→ skills/marketplace/ClawHub/foo/1.0.0/skill/SKILL.zh-CN.md
func ParseChineseCacheKeyFromPackageURI(packageURI string) (string, error) {
	_, _, key, err := storage.ParseURI(packageURI)
	if err != nil {
		return "", fmt.Errorf("parse storage uri: %w", err)
	}
	// 替换末尾的 package.zip 为 SKILL.zh-CN.md
	dir, _ := filepath.Split(key)
	return filepath.ToSlash(dir) + "SKILL.zh-CN.md", nil
}

// ReadChineseDocumentFromStorage 从 storage 读取 SKILL.zh-CN.md 内容并去 frontmatter。
// 文件不存在或读取失败时返回空字符串（调用方据此回退）。
func ReadChineseDocumentFromStorage(ctx context.Context, st storage.Storage, packageURI string) (body string, description string, err error) {
	chineseKey, err := ParseChineseCacheKeyFromPackageURI(packageURI)
	if err != nil {
		return "", "", err
	}

	_, bucket, _, parseErr := storage.ParseURI(packageURI)
	if parseErr != nil {
		return "", "", fmt.Errorf("parse package uri: %w", parseErr)
	}

	result, getErr := st.GetObject(ctx, bucket, chineseKey)
	if getErr != nil {
		return "", "", getErr // caller decides whether to treat as fallback
	}
	defer result.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(result.Body, 1_048_576))
	if readErr != nil {
		return "", "", fmt.Errorf("read SKILL.zh-CN.md: %w", readErr)
	}

	manifest, bodyContent, parseErr := catalog.ParseDocument(raw)
	if parseErr != nil {
		// 无法解析 frontmatter 时仍返回原始内容，调用方决定是否使用
		return string(raw), "", nil
	}

	return bodyContent, manifest.Description, nil
}

// GenerateSkillZip 从 SkillBundle 生成标准 zip 字节。
// zip 根目录包含 SKILL.md，支持文件保持在原相对路径。
func GenerateSkillZip(content []byte, files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// 写入 SKILL.md
	if err := writeZipEntry(zw, "SKILL.md", content); err != nil {
		return nil, fmt.Errorf("write SKILL.md to zip: %w", err)
	}

	// 写入附属文件（仅保留 allowed 子目录下的文件）
	for relPath, data := range files {
		if !isAllowedSubdir(relPath) {
			continue
		}
		if err := writeZipEntry(zw, filepath.ToSlash(relPath), data); err != nil {
			return nil, fmt.Errorf("write %s to zip: %w", relPath, err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// CachePackage 写入缓存：生成 zip → PutObject → 回写 DB。
// 内部错误只记录 warning，不影响调用方。
// 返回写入的 storage URI，写入失败或提前返回时返回空字符串。
func CachePackage(ctx context.Context, st storage.Storage, bucket string,
	db *gorm.DB, source, skillID, version string, bundle *fetch.SkillBundle) string {

	zipBytes, err := GenerateSkillZip(bundle.Content, bundle.Files)
	if err != nil {
		logs.WarnContextf(ctx, "cache package: generate zip failed for %s/%s@%s: %v", source, skillID, version, err)
		return ""
	}

	key := SkillCacheKey(source, skillID, version)
	result, err := st.PutObject(ctx, bucket, key, bytes.NewReader(zipBytes),
		storage.WithContentType("application/zip"),
	)
	if err != nil {
		logs.WarnContextf(ctx, "cache package: put object failed for %s/%s@%s: %v", source, skillID, version, err)
		return ""
	}

	uri := result.Path.URI()
	logs.Infof("cache package: written %s for %s/%s@%s", uri, source, skillID, version)

	// 回写 DB
	if err := upsertPackagePath(ctx, db, source, skillID, version, uri); err != nil {
		logs.WarnContextf(ctx, "cache package: update db path failed for %s/%s@%s: %v", source, skillID, version, err)
	}
	return uri
}

// ChineseWriter 提供写出 SKILL.zh-CN.md 的能力，供 CachePackage 的调用方使用。
// 定义为接口避免 cache 包对 service 层的依赖。
type ChineseWriter func(ctx context.Context, content string) (string, string, error)

// content: 翻译后的完整 SKILL.md（含 frontmatter）
// 返回: body（去 frontmatter）、description（frontmatter 中 description）、error

// CacheChineseDocument 写入 SKILL.zh-CN.md 到 storage，与 package.zip 同级。
// 当 SKILL.md body 的 CJK 比率 >= 0.6 时，直接使用原内容作为中文版。
// 否则通过 translator 调用 LLM 翻译后写入。
// 写库时同步更新 translated_description、author。
// 内部错误只记录 warning。
func CacheChineseDocument(ctx context.Context, st storage.Storage, bucket string,
	db *gorm.DB, source, skillID, version, packageURI string,
	bundle *fetch.SkillBundle, translateFn ChineseWriter) {

	if packageURI == "" || bundle == nil || len(bundle.Content) == 0 {
		return
	}

	// 解析 SKILL.md 获取 body 和 manifest
	manifest, body, parseErr := catalog.ParseDocument(bundle.Content)
	if parseErr != nil {
		logs.WarnContextf(ctx, "cache chinese doc: parse SKILL.md failed for %s/%s@%s: %v", source, skillID, version, parseErr)
		return
	}

	// 决定内容：CJK >= 0.45 用原文，否则调用翻译
	useOriginal := utils.CJKRatioMarkdown(body) >= 0.45

	var chineseContent string
	var zhDescription string

	if useOriginal {
		chineseContent = string(bundle.Content) // 保持完整 frontmatter + body
		zhDescription = manifest.Description
		logs.Infof("cache chinese doc: SKILL.md for %s/%s@%s is already %.0f%% Chinese, using original", source, skillID, version, utils.CJKRatio(body)*100)
	} else if translateFn != nil {
		// 调用翻译
		translated, zhDesc, tErr := translateFn(ctx, string(bundle.Content))
		if tErr != nil {
			logs.WarnContextf(ctx, "cache chinese doc: translate failed for %s/%s@%s: %v", source, skillID, version, tErr)
			return
		}
		chineseContent = translated
		zhDescription = zhDesc
	} else {
		// 无 translator 且原文不是中文 → 跳过
		return
	}

	if chineseContent == "" {
		return
	}

	// 写入 storage
	chineseKey := SkillChineseCacheKey(source, skillID, version)
	_, cErr := st.PutObject(ctx, bucket, chineseKey, strings.NewReader(chineseContent),
		storage.WithContentType("text/markdown; charset=utf-8"),
	)
	if cErr != nil {
		logs.WarnContextf(ctx, "cache chinese doc: put object failed for %s/%s@%s: %v", source, skillID, version, cErr)
		return
	}

	logs.Infof("cache chinese doc: written SKILL.zh-CN.md for %s/%s@%s", source, skillID, version)

	// 回写 DB：更新 translated_description
	if err := upsertChineseMetadata(ctx, db, source, skillID, version, zhDescription); err != nil {
		logs.WarnContextf(ctx, "cache chinese doc: update db metadata for %s/%s@%s: %v", source, skillID, version, err)
	}
}

// upsertPackagePath 回写 package_storage_path 到 DB。
// 先查现有记录，有则更新 path，无则创建新记录。
func upsertPackagePath(ctx context.Context, db *gorm.DB, source, skillID, version string, path string) error {
	item, err := infradb.GetSkillMarketplaceItemBySourceSkillVersion(ctx, db, source, skillID, version)
	if err != nil {
		return err
	}
	if item != nil {
		return infradb.UpdateSkillMarketplacePackagePath(ctx, db, item.ID, path)
	}

	// 没有现成记录，用 BatchUpsert 创建一条
	items := []types.SkillMarketplaceItem{
		{
			SkillID:               skillID,
			Name:                  "",
			TranslatedName:        "",
			Source:                source,
			Description:           "",
			TranslatedDescription: "",
			Author:                "",
			Installs:              0,
			Version:               version,
			Category:              "",
			Tags:                  nil,
			PackageStoragePath:    path,
		},
	}
	return infradb.BatchUpsertSkillMarketplaceItems(ctx, db, items)
}

// upsertChineseMetadata 更新 translated_description 到 DB。
func upsertChineseMetadata(ctx context.Context, db *gorm.DB, source, skillID, version, translatedDescription string) error {
	item, err := infradb.GetSkillMarketplaceItemBySourceSkillVersion(ctx, db, source, skillID, version)
	if err != nil {
		return err
	}
	if item != nil {
		// 仅更新 translated_description 和 description（如果 description 为空）
		updates := map[string]any{
			"translated_description": translatedDescription,
		}
		return db.WithContext(ctx).
			Model(&types.SkillMarketplaceItem{}).
			Where("id = ?", item.ID).
			Updates(updates).Error
	}
	return nil
}

// CacheChineseDocumentWithContent 使用已有的翻译内容写入 SKILL.zh-CN.md。
// 与 CacheChineseDocument 不同，该方法跳过 LLM 翻译，直接使用 preTranslatedContent。
func CacheChineseDocumentWithContent(ctx context.Context, st storage.Storage, bucket string,
	db *gorm.DB, source, skillID, version, packageURI string,
	chineseContent string, zhDescription string) {

	if packageURI == "" || chineseContent == "" {
		return
	}

	// 写入 storage
	chineseKey := SkillChineseCacheKey(source, skillID, version)
	_, cErr := st.PutObject(ctx, bucket, chineseKey, strings.NewReader(chineseContent),
		storage.WithContentType("text/markdown; charset=utf-8"),
	)
	if cErr != nil {
		logs.WarnContextf(ctx, "cache chinese doc: put object failed for %s/%s@%s: %v", source, skillID, version, cErr)
		return
	}

	logs.Infof("cache chinese doc: written SKILL.zh-CN.md for %s/%s@%s", source, skillID, version)

	// 回写 DB：更新 translated_description
	if err := upsertChineseMetadata(ctx, db, source, skillID, version, zhDescription); err != nil {
		logs.WarnContextf(ctx, "cache chinese doc: update db metadata for %s/%s@%s: %v", source, skillID, version, err)
	}
}

// isAllowedSubdir 检查文件路径是否在允许的子目录内。
func isAllowedSubdir(path string) bool {
	topDir, _, _ := strings.Cut(path, "/")
	switch topDir {
	case "assets", "references", "scripts", "templates":
		return true
	}
	return false
}

// writeZipEntry 向 zip writer 写入一个条目。
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadPackageFromStorage 从 storage 读取标准 zip，返回去 frontmatter 的 SKILL.md body 和文件列表。
// uri 是 storage-go URI（如 "s3://bucket/skills/marketplace/..."）。
// 返回: skillMDBody（不含 frontmatter）、files（含 SKILL.md）、rawZip（原始 zip 字节）、error
func ReadPackageFromStorage(ctx context.Context, st storage.Storage, uri string) (skillMDBody string, files []string, rawZip []byte, err error) {
	_, bucket, key, parseErr := storage.ParseURI(uri)
	if parseErr != nil {
		return "", nil, nil, fmt.Errorf("parse storage uri: %w", parseErr)
	}

	result, getErr := st.GetObject(ctx, bucket, key)
	if getErr != nil {
		return "", nil, nil, fmt.Errorf("get object: %w", getErr)
	}
	defer result.Body.Close()

	rawZip, readErr := io.ReadAll(io.LimitReader(result.Body, 100_000_000))
	if readErr != nil {
		return "", nil, nil, fmt.Errorf("read zip: %w", readErr)
	}

	reader, zipErr := zip.NewReader(bytes.NewReader(rawZip), int64(len(rawZip)))
	if zipErr != nil {
		return "", nil, nil, fmt.Errorf("open zip: %w", zipErr)
	}

	var skillMDRaw []byte
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)

		// 收集文件列表
		if name != "SKILL.md" && !isAllowedSubdir(name) {
			continue
		}
		files = append(files, name)

		// 读 SKILL.md
		if strings.EqualFold(filepath.Base(name), "SKILL.md") {
			rc, openErr := f.Open()
			if openErr != nil {
				return "", nil, nil, fmt.Errorf("open SKILL.md in zip: %w", openErr)
			}
			skillMDRaw, readErr = io.ReadAll(io.LimitReader(rc, 1_048_576))
			rc.Close()
			if readErr != nil {
				return "", nil, nil, fmt.Errorf("read SKILL.md: %w", readErr)
			}
		}
	}

	if skillMDRaw == nil {
		return "", nil, nil, fmt.Errorf("SKILL.md not found in cached package")
	}

	// 去 frontmatter
	if _, body, parseErr := catalog.ParseDocument(skillMDRaw); parseErr == nil {
		skillMDBody = body
	} else {
		skillMDBody = string(skillMDRaw)
	}

	return skillMDBody, files, rawZip, nil
}

// ExtractSkillMDFromZip 从 zip 字节中提取完整的 SKILL.md 内容（含 frontmatter）。
// 用于翻译时需要完整原始内容。
func ExtractSkillMDFromZip(rawZip []byte) string {
	reader, err := zip.NewReader(bytes.NewReader(rawZip), int64(len(rawZip)))
	if err != nil {
		return ""
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Base(f.Name), "SKILL.md") {
			rc, openErr := f.Open()
			if openErr != nil {
				return ""
			}
			data, readErr := io.ReadAll(io.LimitReader(rc, 1_048_576))
			rc.Close()
			if readErr != nil {
				return ""
			}
			return string(data)
		}
	}
	return ""
}
