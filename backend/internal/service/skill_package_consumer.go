package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const skillPackageRetryDelay = 5 * time.Second

type permanentSkillPackageError struct {
	err error
}

func (e *permanentSkillPackageError) Error() string { return e.err.Error() }
func (e *permanentSkillPackageError) Unwrap() error { return e.err }

// StartSkillPackageUploadedConsumer consumes independently published Skill packages.
func StartSkillPackageUploadedConsumer(ctx context.Context, database *gorm.DB, bus eventbus.EventBus) {
	topic := messaging.SkillPackageUploadedWildcard()
	logs.InfoContextf(ctx, "starting Skill package uploaded consumer: %s", topic)
	err := bus.SubscribeManualDurable(
		ctx,
		topic,
		messaging.SkillPackageUploadedConsumer(),
		func(message *nats.Msg) {
			handleSkillPackageUploadedMessage(ctx, database, message)
		},
	)
	if err != nil && !errors.Is(err, context.Canceled) {
		logs.ErrorContextf(ctx, "subscribe to %s failed: %v", topic, err)
	}
}

func handleSkillPackageUploadedMessage(ctx context.Context, database *gorm.DB, message *nats.Msg) {
	orgID, err := messaging.OrgIDFromSkillPackageSubject(message.Subject)
	if err != nil {
		logs.WarnContextf(ctx, "terminate invalid Skill package subject: %v", err)
		_ = message.Term()
		return
	}
	var event messaging.SkillPackageUploadedEvent
	if err := json.Unmarshal(message.Data, &event); err != nil {
		logs.WarnContextf(ctx, "terminate invalid Skill package event JSON: %v", err)
		_ = message.Term()
		return
	}
	if err := processSkillPackageUploaded(ctx, database, orgID, event); err != nil {
		var permanent *permanentSkillPackageError
		if errors.As(err, &permanent) {
			logs.WarnContextf(ctx, "terminate invalid Skill package event %q: %v", event.EventID, err)
			_ = message.Term()
			return
		}
		logs.WarnContextf(ctx, "retry Skill package event %q: %v", event.EventID, err)
		_ = message.NakWithDelay(skillPackageRetryDelay)
		return
	}
	if err := message.Ack(); err != nil {
		logs.WarnContextf(ctx, "ack Skill package event %q: %v", event.EventID, err)
	}
}

func processSkillPackageUploaded(
	ctx context.Context,
	database *gorm.DB,
	orgID uint,
	event messaging.SkillPackageUploadedEvent,
) error {
	if database == nil {
		return fmt.Errorf("database is required")
	}
	if err := validateSkillPackageEvent(event); err != nil {
		return permanentSkillError(err)
	}
	actorID := event.ActorUIN
	if actorID == 0 {
		return permanentSkillError(fmt.Errorf("actor_uin is required"))
	}

	rawArchive, err := readUploadedSkillPackage(ctx, event.StorageURI)
	if err != nil {
		return err
	}
	if int64(len(rawArchive)) != event.FileSize {
		return permanentSkillError(fmt.Errorf("Skill package size does not match event"))
	}
	rawDigest := sha256.Sum256(rawArchive)
	if hex.EncodeToString(rawDigest[:]) != strings.ToLower(event.SHA256) {
		return permanentSkillError(fmt.Errorf("Skill package SHA-256 does not match event"))
	}
	prepared, err := prepareSkillPackage(rawArchive)
	if err != nil {
		return permanentSkillError(fmt.Errorf("validate Skill package: %w", err))
	}
	if prepared.SHA256 != strings.ToLower(event.SHA256) {
		return permanentSkillError(fmt.Errorf("standard Skill package SHA-256 does not match event"))
	}
	if strings.TrimSpace(prepared.Manifest.Name) != event.SkillCode {
		return permanentSkillError(fmt.Errorf("Skill manifest name does not match event code"))
	}

	return database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fileUpload, err := infradb.GetFileUploadByStorageURI(ctx, tx, orgID, event.StorageURI)
		if err != nil {
			return fmt.Errorf("find Skill FileUpload by storage URI: %w", err)
		}
		if fileUpload != nil {
			if !strings.EqualFold(fileUpload.Sha256, event.SHA256) {
				return permanentSkillError(fmt.Errorf("storage URI already belongs to different content"))
			}
		} else {
			fileUpload, err = filestore.RecordUpload(ctx, tx, filestore.RecordUploadParams{
				StorageURI: event.StorageURI, Filename: event.Filename, OriginalName: event.Filename,
				MimeType: event.MimeType, OwnerScope: types.OwnerScopeOrganization,
				OrgID: orgID, OwnerID: actorID, FileSize: event.FileSize,
				Sha256: strings.ToLower(event.SHA256), Purpose: filestore.PurposeSkillPackage,
			})
			if err != nil {
				return fmt.Errorf("record Skill FileUpload: %w", err)
			}
		}

		definition, err := json.Marshal(skillDefinition{
			Schema: "skill/v1",
			Artifact: &ArtifactDefinition{
				FileUploadID: fileUpload.PublicID,
				SHA256:       prepared.SHA256,
				SizeBytes:    event.FileSize,
				ContentType:  event.MimeType,
			},
		})
		if err != nil {
			return fmt.Errorf("build Skill definition: %w", err)
		}
		pluginResult, err := (&pluginService{db: tx}).publishSkillRevisionWithScope(
			ctx,
			tx,
			skillPublishRequest{
				OwnerScope:  types.OwnerScopeOrganization,
				OrgID:       orgID,
				ActorID:     actorID,
				Origin:      "org",
				ActorType:   "worker",
				Code:        event.SkillCode,
				Name:        prepared.Manifest.Name,
				Description: prepared.Manifest.Description,
				Definition:  definition,
				Content:     prepared.Content,
			},
		)
		if err != nil {
			if errors.Is(err, contract.ErrPluginForbidden) || errors.Is(err, contract.ErrPluginNotFound) {
				return permanentSkillError(fmt.Errorf("publish organization Skill revision: %w", err))
			}
			return fmt.Errorf("publish organization Skill revision: %w", err)
		}
		if event.ProjectID == 0 {
			return nil
		}
		if err := ensureSkillProjectBinding(ctx, tx, orgID, event.ProjectID, pluginResult.Plugin.ID, actorID); err != nil {
			return err
		}
		return nil
	})
}

func validateSkillPackageEvent(event messaging.SkillPackageUploadedEvent) error {
	if strings.TrimSpace(event.EventID) == "" || event.WorkerID == 0 || strings.TrimSpace(event.RunID) == "" {
		return fmt.Errorf("event_id, worker_id, and run_id are required")
	}
	if _, err := validPublishedSkillCode(event.SkillCode); err != nil {
		return err
	}
	if event.ChangeType != messaging.SkillChangeCreated && event.ChangeType != messaging.SkillChangeUpdated {
		return fmt.Errorf("unsupported Skill change type %q", event.ChangeType)
	}
	if strings.TrimSpace(event.StorageURI) == "" {
		return fmt.Errorf("storage_uri is required")
	}
	bucket, key, err := filestore.ParseStorageURI(event.StorageURI)
	if err != nil || strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
		return fmt.Errorf("storage_uri must be a valid storage URI")
	}
	shaValue := strings.ToLower(strings.TrimSpace(event.SHA256))
	if len(shaValue) != sha256.Size*2 {
		return fmt.Errorf("sha256 must be a 64-character hexadecimal value")
	}
	if _, err := hex.DecodeString(shaValue); err != nil {
		return fmt.Errorf("sha256 must be hexadecimal")
	}
	if event.FileSize <= 0 || event.FileSize > skillarchive.MaxPackageBytes {
		return fmt.Errorf("invalid Skill package file_size")
	}
	if strings.TrimSpace(event.Filename) == "" || event.MimeType != "application/zip" {
		return fmt.Errorf("Skill package filename and application/zip mime_type are required")
	}
	return nil
}

func readUploadedSkillPackage(ctx context.Context, storageURI string) ([]byte, error) {
	reader, err := filestore.OpenFileUpload(ctx, &types.FileUpload{StorageURI: storageURI})
	if err != nil {
		return nil, fmt.Errorf("open uploaded Skill package: %w", err)
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, skillarchive.MaxPackageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read uploaded Skill package: %w", err)
	}
	if int64(len(raw)) > skillarchive.MaxPackageBytes {
		return nil, permanentSkillError(fmt.Errorf("Skill package exceeds size limit"))
	}
	return raw, nil
}

func ensureSkillProjectBinding(
	ctx context.Context,
	database *gorm.DB,
	orgID, projectID, pluginID, actorID uint,
) error {
	var project types.Project
	if err := database.WithContext(ctx).
		Where("id = ? AND org_id = ?", projectID, orgID).
		First(&project).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return permanentSkillError(fmt.Errorf("project does not belong to organization"))
		}
		return fmt.Errorf("find Skill project: %w", err)
	}
	var binding types.ProjectPluginBinding
	err := database.WithContext(ctx).Unscoped().
		Where("project_id = ? AND plugin_id = ?", projectID, pluginID).
		Order("id DESC").
		First(&binding).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		binding = types.ProjectPluginBinding{
			ProjectID: projectID, PluginID: pluginID, Enabled: true, Config: []byte(`{}`),
			CreatedBy: actorID, UpdatedBy: actorID,
		}
		if err := infradb.CreateProjectPluginBinding(ctx, database, &binding); err != nil {
			return fmt.Errorf("create Skill project binding: %w", err)
		}
	case err != nil:
		return fmt.Errorf("find Skill project binding: %w", err)
	default:
		if err := database.WithContext(ctx).Unscoped().
			Model(&types.ProjectPluginBinding{}).
			Where("id = ?", binding.ID).
			Updates(map[string]any{
				"deleted_at": nil,
				"enabled":    true,
				"updated_by": actorID,
			}).Error; err != nil {
			return fmt.Errorf("restore Skill project binding: %w", err)
		}
	}
	return nil
}

func validPublishedSkillCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" || code == "." || code == ".." || strings.ContainsAny(code, `/\`) ||
		strings.HasPrefix(code, ".") || code == "runs" {
		return "", fmt.Errorf("invalid organization Skill code %q", code)
	}
	return code, nil
}

func permanentSkillError(err error) error {
	return &permanentSkillPackageError{err: err}
}
