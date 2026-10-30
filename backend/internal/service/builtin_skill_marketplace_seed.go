package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	skilllinks "github.com/insmtx/Leros/backend/internal/skill/links"
	"github.com/insmtx/Leros/backend/types"
)

const (
	builtinMarketplaceSourceType = "builtin"
	builtinMarketplaceAuthor     = "Lework"
)

var builtinMarketplaceSyncMu sync.Mutex

// BuiltinSkillSyncFailure records one isolated built-in Skill synchronization error.
type BuiltinSkillSyncFailure struct {
	Code string
	Err  error
}

// BuiltinSkillSyncReport summarizes one startup synchronization pass.
type BuiltinSkillSyncReport struct {
	Scanned   int
	Created   int
	Updated   int
	Unchanged int
	Restored  int
	Failures  []BuiltinSkillSyncFailure
}

// SyncBuiltinServerSkillMarketplace publishes server Skills into the official system catalogue.
// An invalid Skill is isolated from the rest of the synchronization pass.
func SyncBuiltinServerSkillMarketplace(
	ctx context.Context,
	database *gorm.DB,
	sourceDir string,
) (*BuiltinSkillSyncReport, error) {
	builtinMarketplaceSyncMu.Lock()
	defer builtinMarketplaceSyncMu.Unlock()

	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	resolved, err := skilllinks.ResolveBuiltinSkillsSource(sourceDir, "server")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("read built-in server Skill directory: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})

	report := &BuiltinSkillSyncReport{}
	service := &pluginService{db: database}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		report.Scanned++
		operation, err := service.syncBuiltinSkill(
			ctx, filepath.Join(resolved, entry.Name()), entry.Name(),
		)
		if err != nil {
			report.Failures = append(report.Failures, BuiltinSkillSyncFailure{
				Code: entry.Name(),
				Err:  err,
			})
			continue
		}
		switch operation {
		case "created":
			report.Created++
		case "updated":
			report.Updated++
		default:
			report.Unchanged++
		}
	}
	return report, nil
}

func (s *pluginService) syncBuiltinSkill(
	ctx context.Context,
	skillDir string,
	directoryCode string,
) (string, error) {
	prepared, err := packageBuiltinSkillDirectory(skillDir)
	if err != nil {
		return "", err
	}
	code := prepared.Manifest.Name
	if code != directoryCode {
		return "", fmt.Errorf("SKILL.md name %q must match directory %q", code, directoryCode)
	}
	archive, artifactSHA, content := prepared.Archive, prepared.SHA256, prepared.Content

	operation := "unchanged"
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := infradb.GetPluginMarketplaceItemBySource(
			ctx, tx, builtinMarketplaceSourceType, code,
		)
		if err != nil {
			return err
		}
		var plugin *types.Plugin
		if item != nil && item.PluginID > 0 {
			plugin, err = infradb.GetPluginByID(ctx, tx, item.PluginID)
		} else {
			plugin, err = infradb.GetSystemPluginByCode(ctx, tx, "skill", code)
		}
		if err != nil {
			return err
		}

		// Refuse to overwrite a worker builtin plugin with the same code.
		if plugin != nil && plugin.Origin == builtinWorkerOrigin {
			return fmt.Errorf("server skill code %q conflicts with existing worker builtin skill", code)
		}

		var currentRevision *types.PluginRevision
		if plugin != nil {
			currentRevision, err = infradb.GetCurrentPluginRevision(ctx, tx, plugin)
			if err != nil {
				return err
			}
		}
		currentSHA := ""
		if currentRevision != nil {
			artifact, artifactErr := ArtifactFromDefinition("skill", currentRevision.Definition)
			if artifactErr == nil && artifact != nil {
				currentSHA, _ = normalizedPluginSHA256(artifact.SHA256)
			}
		}

		var published *skillPublishResult
		if currentSHA == artifactSHA {
			published = &skillPublishResult{
				Plugin: plugin, Revision: currentRevision, Operation: "unchanged",
			}
		} else {
			file, err := storeSystemSkillArtifact(ctx, tx, code, archive, artifactSHA)
			if err != nil {
				return fmt.Errorf("store system Skill artifact: %w", err)
			}
			definition, err := json.Marshal(skillDefinition{
				Schema: "skill/v1",
				Artifact: &ArtifactDefinition{
					FileUploadID: file.PublicID, SHA256: artifactSHA,
					SizeBytes: file.FileSize, ContentType: "application/zip",
				},
			})
			if err != nil {
				return err
			}
			published, err = s.publishSkillRevisionWithScope(ctx, tx, skillPublishRequest{
				OwnerScope: types.OwnerScopeSystem, OrgID: 0, ActorID: 0,
				Origin: "builtin", ActorType: "system", Code: code, Name: code,
				Description: prepared.Manifest.Description,
				Definition:  definition, Content: content,
			})
			if err != nil {
				return err
			}
		}
		if published == nil || published.Plugin == nil || published.Revision == nil {
			return fmt.Errorf("system Skill publication is incomplete")
		}

		now := time.Now()
		if item == nil {
			item = &types.PluginMarketplaceItem{
				PublicID: "mkt_" + uuid.NewString(), SourceType: builtinMarketplaceSourceType,
				SourceRef: code, Status: "published", PublishedAt: now,
			}
			operation = "created"
		} else if published.Operation != "unchanged" {
			item.PublishedAt = now
			operation = "updated"
		}
		item.PluginID = published.Plugin.ID
		item.Kind, item.Code = "skill", code
		item.Name, item.Description = code, prepared.Manifest.Description
		item.Author = builtinMarketplaceAuthor
		item.Category = prepared.Manifest.Metadata.Category
		item.Tags = append(types.PluginStringList(nil), prepared.Manifest.Metadata.Tags...)
		item.Verified = true
		item.Status = "published"
		if item.ID == 0 {
			inserted, err := infradb.CreatePluginMarketplaceItemIfAbsent(ctx, tx, item)
			if err != nil {
				return err
			}
			if inserted {
				return nil
			}
			item, err = infradb.GetPluginMarketplaceItemBySource(
				ctx, tx, builtinMarketplaceSourceType, code,
			)
			if err != nil {
				return err
			}
			if item == nil {
				return fmt.Errorf("reload concurrently created marketplace item")
			}
			operation = "unchanged"
			if published.Operation != "unchanged" {
				operation = "updated"
			}
			item.PluginID = published.Plugin.ID
			item.Kind, item.Code = "skill", code
			item.Name, item.Description = code, prepared.Manifest.Description
			item.Author = builtinMarketplaceAuthor
			item.Category = prepared.Manifest.Metadata.Category
			item.Tags = append(types.PluginStringList(nil), prepared.Manifest.Metadata.Tags...)
			item.Verified = true
			item.Status = "published"
		}
		return infradb.UpdatePluginMarketplaceItem(ctx, tx, item)
	})
	return operation, err
}

func storeSystemSkillArtifact(
	ctx context.Context,
	database *gorm.DB,
	code string,
	archive []byte,
	artifactSHA string,
) (*types.FileUpload, error) {
	existing, err := infradb.GetSystemFileUploadBySHA256(ctx, database, artifactSHA)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if !strings.EqualFold(existing.Status, "active") ||
			existing.MimeType != "application/zip" ||
			existing.FileSize != int64(len(archive)) {
			return nil, fmt.Errorf("content-addressed system artifact is not reusable")
		}
		return existing, nil
	}
	file, err := filestore.Upload(ctx, database, filestore.UploadParams{
		Data: archive, Filename: code + "-" + artifactSHA[:12] + ".zip",
		OriginalName: code + ".zip", MimeType: "application/zip",
		OwnerScope: types.OwnerScopeSystem, OrgID: 0, OwnerID: 0,
		ObjectKey: "plugins/system/artifacts/" + artifactSHA + ".zip",
		Purpose:   filestore.PurposeArtifact,
	})
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, infradb.ErrSystemArtifactAlreadyExists) {
		return nil, err
	}
	existing, lookupErr := infradb.GetSystemFileUploadBySHA256(
		ctx, database, artifactSHA,
	)
	if lookupErr != nil {
		return nil, lookupErr
	}
	if existing == nil {
		return nil, err
	}
	return existing, nil
}

func packageBuiltinSkillDirectory(skillDir string) (*preparedSkillPackage, error) {
	files := make(map[string][]byte)
	var skillDocument []byte
	err := filepath.WalkDir(skillDir, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(skillDir, filePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %q is not allowed", relative)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular file %q is not allowed", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > skillarchive.MaxPackageBytes {
			return fmt.Errorf("file %q exceeds size limit", relative)
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == skillEntrypointPath {
			skillDocument = content
		} else {
			files[relative] = content
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if skillDocument == nil {
		return nil, fmt.Errorf("root SKILL.md is required")
	}
	archive, err := skillcache.GenerateSkillZip(skillDocument, files)
	if err != nil {
		return nil, err
	}
	if int64(len(archive)) > skillarchive.MaxPackageBytes {
		return nil, fmt.Errorf("Skill package exceeds size limit")
	}
	return prepareSkillPackage(archive)
}
