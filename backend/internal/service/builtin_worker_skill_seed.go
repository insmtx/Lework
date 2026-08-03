package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	skilllinks "github.com/insmtx/Leros/backend/internal/skill/links"
	"github.com/insmtx/Leros/backend/types"
)

const builtinWorkerOrigin = "builtin_worker"

var builtinWorkerSyncMu sync.Mutex

// SyncBuiltinWorkerSkills publishes worker Skills as system plugins without marketplace entries.
// An invalid Skill is isolated from the rest of the synchronization pass.
func SyncBuiltinWorkerSkills(
	ctx context.Context,
	database *gorm.DB,
	sourceDir string,
) (*BuiltinSkillSyncReport, error) {
	builtinWorkerSyncMu.Lock()
	defer builtinWorkerSyncMu.Unlock()

	if database == nil {
		return nil, fmt.Errorf("database is required")
	}
	resolved, err := skilllinks.ResolveBuiltinSkillsSource(sourceDir, "worker")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, fmt.Errorf("read built-in worker Skill directory: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})

	report := &BuiltinSkillSyncReport{}
	service := &pluginService{db: database}
	syncedCodes := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		report.Scanned++
		syncedCodes[entry.Name()] = true
		operation, err := service.syncBuiltinWorkerSkill(
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
		case "restored":
			report.Restored++
		default:
			report.Unchanged++
		}
	}

	if err := service.archiveStaleWorkerSkills(ctx, syncedCodes); err != nil {
		// Archive failures are logged but do not fail the overall sync.
		// They are recorded as a single synthetic failure entry.
		report.Failures = append(report.Failures, BuiltinSkillSyncFailure{
			Code: "__archive__",
			Err:  fmt.Errorf("archive stale worker skills: %w", err),
		})
	}
	return report, nil
}

func (s *pluginService) syncBuiltinWorkerSkill(
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
		plugin, err := infradb.GetSystemPluginByCode(ctx, tx, "skill", code)
		if err != nil {
			return err
		}

		// Origin conflict guard: refuse to overwrite a server builtin plugin.
		if plugin != nil && plugin.Origin == "builtin" {
			return fmt.Errorf("worker skill code %q conflicts with existing server builtin skill", code)
		}
		if plugin != nil && plugin.Origin != builtinWorkerOrigin {
			return fmt.Errorf("worker skill code %q has unexpected origin %q", code, plugin.Origin)
		}

		wasArchived := plugin != nil && plugin.Status == types.PluginStatusArchived

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

		if currentSHA == artifactSHA {
			if wasArchived {
				// Content unchanged, just reactivate.
				if err := tx.Model(&types.Plugin{}).Where("id = ?", plugin.ID).
					Select("status").
					Updates(types.Plugin{Status: types.PluginStatusActive}).Error; err != nil {
					return err
				}
				operation = "restored"
				return nil
			}
			// Content unchanged and already active — nothing to do.
			operation = "unchanged"
			return nil
		}

		// Content changed or new plugin.
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
		published, err := s.publishSkillRevisionWithScope(ctx, tx, skillPublishRequest{
			OwnerScope:  types.OwnerScopeSystem,
			OrgID:       0,
			ActorID:     0,
			Origin:      builtinWorkerOrigin,
			ActorType:   "system",
			Code:        code,
			Name:        prepared.Manifest.Name,
			Description: prepared.Manifest.Description,
			Definition:  definition,
			Content:     content,
		})
		if err != nil {
			return err
		}
		if published == nil || published.Plugin == nil || published.Revision == nil {
			return fmt.Errorf("system Skill publication is incomplete")
		}
		// publishSkillRevisionWithScope sets status back to active for existing plugins,
		// so an archived plugin with changed content is automatically reactivated.
		operation = published.Operation
		return nil
	})
	return operation, err
}

func (s *pluginService) archiveStaleWorkerSkills(
	ctx context.Context,
	currentCodes map[string]bool,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plugins, err := infradb.ListSystemPluginsByOrigin(ctx, tx, "skill", builtinWorkerOrigin)
		if err != nil {
			return err
		}
		for _, plugin := range plugins {
			if currentCodes[plugin.Code] {
				continue
			}
			if plugin.Status != types.PluginStatusActive {
				continue
			}
			if err := infradb.ArchivePlugin(ctx, tx, plugin.ID); err != nil {
				return err
			}
		}
		return nil
	})
}
