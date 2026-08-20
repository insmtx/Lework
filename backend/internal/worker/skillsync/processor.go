package skillsync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ygpkg/storage-go"

	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/internal/worker/skillstate"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

const skillPackageMimeType = "application/zip"

// Publisher is the narrow message capability required by Skill synchronization.
type Publisher interface {
	Publish(context.Context, string, any) error
}

type storageClient interface {
	Config(context.Context) (*cli.StorageConfig, error)
	PresignUpload(context.Context, string, string) (string, error)
}

type serverStorageClient struct {
	serverAddr string
	authToken  string
}

func (c serverStorageClient) Config(ctx context.Context) (*cli.StorageConfig, error) {
	return cli.GetStorageConfig(ctx, c.serverAddr, c.authToken)
}

func (c serverStorageClient) PresignUpload(ctx context.Context, bucket, key string) (string, error) {
	return cli.GetPresignUploadURL(ctx, c.serverAddr, c.authToken, bucket, key)
}

// Processor publishes successful-run Skill changes without affecting Run status.
type Processor struct {
	orgID      uint
	workerID   uint
	repository *Repository
	publisher  Publisher
	storage    storageClient
	httpClient *http.Client
}

// RunContext carries publication identifiers and the task Skill view to import.
type RunContext struct {
	RunID               string
	ProjectID           uint
	ActorUIN            uint
	PublishChanges      bool
	LocalOnlySkillCodes []string
	TaskSkillDir        string
}

// NewProcessor creates a worker Skill post-run processor.
func NewProcessor(
	orgID, workerID uint,
	serverAddr, authToken string,
	repository *Repository,
	publisher Publisher,
) (*Processor, error) {
	if orgID == 0 || workerID == 0 {
		return nil, fmt.Errorf("organization and worker are required for Skill synchronization")
	}
	if repository == nil {
		return nil, fmt.Errorf("Skill repository is required")
	}
	if publisher == nil {
		return nil, fmt.Errorf("Skill event publisher is required")
	}
	serverAddr = strings.TrimSpace(serverAddr)
	if serverAddr == "" {
		return nil, fmt.Errorf("server address is required for Skill synchronization")
	}
	return &Processor{
		orgID: orgID, workerID: workerID, repository: repository, publisher: publisher,
		storage:    serverStorageClient{serverAddr: serverAddr, authToken: strings.TrimSpace(authToken)},
		httpClient: http.DefaultClient,
	}, nil
}

// Process restores local-only Skills and publishes eligible successful-Run changes.
func (p *Processor) Process(ctx context.Context, run RunContext) error {
	if p == nil || p.repository == nil || strings.TrimSpace(run.RunID) == "" {
		return fmt.Errorf("Skill post-run context is required")
	}
	lock := p.repository.repositoryLock()
	lock.Lock()
	defer lock.Unlock()
	defer func() {
		p.repository.importTaskSkillDirs(ctx, run.TaskSkillDir)
		if err := p.repository.restoreAll(context.WithoutCancel(ctx)); err != nil {
			logs.WarnContextf(ctx, "restore Worker Skill repository after Run failed: %v", err)
		}
	}()
	if err := p.repository.ensure(ctx); err != nil {
		return err
	}
	p.repository.importTaskSkillDirs(ctx, run.TaskSkillDir)
	changes, deleted, err := p.repository.changes(ctx)
	if err != nil {
		return err
	}
	localOnly := make(map[string]struct{}, len(run.LocalOnlySkillCodes))
	for _, code := range run.LocalOnlySkillCodes {
		code, validateErr := validSkillCode(code)
		if validateErr == nil {
			localOnly[code] = struct{}{}
		}
	}
	manifest, manifestErr := p.repository.committedInstallManifest(ctx)
	if manifestErr == nil {
		for code, record := range manifest.Records {
			if record.SyncPolicy == skillstate.SyncPolicyLocalOnly {
				localOnly[code] = struct{}{}
			}
		}
		if len(manifest.Warnings) > 0 {
			manifestErr = fmt.Errorf("committed Skill manifest is invalid: %v", manifest.Warnings)
		}
	}
	for _, code := range deleted {
		_, isLocalOnly := localOnly[code]
		if !isLocalOnly && !run.PublishChanges {
			continue
		}
		if err := p.repository.restore(ctx, code); err != nil {
			logs.WarnContextf(ctx, "restore deleted Skill %q: %v", code, err)
		}
	}
	publishable := make([]Change, 0, len(changes))
	for _, change := range changes {
		if _, isLocalOnly := localOnly[change.Code]; isLocalOnly {
			if err := p.repository.restore(ctx, change.Code); err != nil {
				logs.WarnContextf(ctx, "restore local-only Skill %q: %v", change.Code, err)
			}
			continue
		}
		if run.PublishChanges {
			publishable = append(publishable, change)
		}
	}
	if manifestErr != nil {
		return fmt.Errorf("read committed Skill manifest before publication: %w", manifestErr)
	}
	if len(publishable) == 0 {
		return nil
	}
	storageConfig, err := p.storage.Config(ctx)
	if err != nil {
		return fmt.Errorf("get Skill storage config: %w", err)
	}
	if storageConfig == nil || strings.TrimSpace(storageConfig.Bucket) == "" {
		return fmt.Errorf("Skill storage bucket is required")
	}
	for _, change := range publishable {
		if err := p.publishChange(ctx, run, change, storageConfig); err != nil {
			logs.WarnContextf(ctx, "publish Skill %q change: %v", change.Code, err)
		}
	}
	return nil
}

func (p *Processor) publishChange(
	ctx context.Context,
	run RunContext,
	change Change,
	storageConfig *cli.StorageConfig,
) error {
	archive, err := buildPackage(p.repository.Root(), change.Code)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(archive)
	sha256Hex := hex.EncodeToString(digest[:])
	key := fmt.Sprintf("plugins/%d/skills/%s.zip", p.orgID, sha256Hex)
	scheme := strings.TrimSpace(storageConfig.Scheme)
	if scheme == "" {
		scheme = "s3"
	}
	storageURI, err := storage.BuildURI(scheme, storageConfig.Bucket, key)
	if err != nil {
		return fmt.Errorf("build Skill storage URI: %w", err)
	}
	uploadURL, err := p.storage.PresignUpload(ctx, storageConfig.Bucket, key)
	if err != nil {
		return fmt.Errorf("get Skill presigned upload URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("create Skill upload request: %w", err)
	}
	request.Header.Set("Content-Type", skillPackageMimeType)
	request.ContentLength = int64(len(archive))
	response, err := p.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("upload Skill package: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("upload Skill package returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	event := messaging.SkillPackageUploadedEvent{
		EventID:    skillEventID(p.workerID, run.RunID, change.Code, sha256Hex),
		WorkerID:   p.workerID,
		RunID:      run.RunID,
		ProjectID:  run.ProjectID,
		ActorUIN:   run.ActorUIN,
		SkillCode:  change.Code,
		ChangeType: change.Type,
		StorageURI: storageURI,
		SHA256:     sha256Hex,
		FileSize:   int64(len(archive)),
		Filename:   filepath.Base(change.Code) + ".zip",
		MimeType:   skillPackageMimeType,
	}
	subject, err := messaging.SkillPackageUploadedSubject(p.orgID)
	if err != nil {
		return err
	}
	if err := p.publisher.Publish(ctx, subject, event); err != nil {
		return fmt.Errorf("publish Skill package event: %w", err)
	}
	if err := p.repository.restore(ctx, change.Code); err != nil {
		return fmt.Errorf("restore published Skill: %w", err)
	}
	return nil
}

func skillEventID(workerID uint, runID, code, sha256Hex string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", workerID, runID, code, sha256Hex)))
	return "skill_evt_" + hex.EncodeToString(digest[:16])
}
