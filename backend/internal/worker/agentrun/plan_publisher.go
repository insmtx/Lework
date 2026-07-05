package agentrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"
)

const (
	planSummaryLines     = 20
	planUploadMaxRetries = 3
	maxPlanFileSize      = 256 * 1024
)

// PlanPublishError is a business-stage error during plan publishing.
// AgentRun records it after Runtime execution returns so it is not mistaken
// for a Runtime adapter failure.
type PlanPublishError struct {
	Phase string
	Err   error
}

func (e *PlanPublishError) Error() string {
	return fmt.Sprintf("plan publish %s: %v", e.Phase, e.Err)
}

func (e *PlanPublishError) Unwrap() error {
	return e.Err
}

// PlanPublisherConfig holds injected dependencies for plan publishing.
type PlanPublisherConfig struct {
	ServerAddr string
	OrgID      uint
	AuthToken  string
	HTTPClient *http.Client
}

// planPublisher implements PlanPublisher.
type planPublisher struct {
	cfg PlanPublisherConfig
}

// NewPlanPublisher creates a PlanPublisher with injected dependencies.
func NewPlanPublisher(cfg PlanPublisherConfig) PlanPublisher {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return &planPublisher{cfg: cfg}
}

// Publish reads the plan file at the path in the event, uploads it, and returns a plan.published event.
func (p *planPublisher) Publish(ctx context.Context, event agent.NodeEvent) (*messaging.RunEventBody, error) {
	if p == nil {
		return nil, nil
	}

	payload, ok := event.Payload.(*agent.PlanReadyPayload)
	if !ok || payload == nil {
		return nil, nil
	}
	if payload.Path == "" {
		return nil, &PlanPublishError{Phase: "validate", Err: errors.New("plan.ready missing path")}
	}

	content, err := readPlanFile(payload.Path)
	if err != nil {
		return nil, &PlanPublishError{Phase: "read", Err: fmt.Errorf("read plan file %s: %w", payload.Path, err)}
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	summaryEnd := planSummaryLines
	if summaryEnd > totalLines {
		summaryEnd = totalLines
	}
	summaryContent := strings.Join(lines[:summaryEnd], "\n")

	fileID := "file_" + snowflake.GenerateIDBase58()
	filename := filepath.Base(payload.Path)
	if filename == "" || filename == "." {
		filename = "plan.md"
	}
	displayPath := payload.DisplayPath
	if displayPath == "" {
		displayPath = payload.Path
	}

	sessionID := payload.ProviderSessionID

	if p.cfg.ServerAddr == "" || p.cfg.OrgID == 0 {
		return nil, &PlanPublishError{Phase: "validate", Err: fmt.Errorf("missing identity: server_addr=%q org_id=%d", p.cfg.ServerAddr, p.cfg.OrgID)}
	}

	storageKey := fmt.Sprintf("projects/%d/sess/%s/plans/%s.md", p.cfg.OrgID, sessionID, fileID)
	fileSize := int64(len(content))
	hash := sha256.Sum256([]byte(content))
	sha256Hex := hex.EncodeToString(hash[:])
	mimeType := "text/markdown"

	logs.InfoContextf(ctx, "[plan] publisher preparing upload: session_id=%s file_id=%s storage_key=%s size=%d sha256=%s",
		sessionID, fileID, storageKey, fileSize, sha256Hex[:16])

	storageConfig, err := cli.GetStorageConfig(ctx, p.cfg.ServerAddr, p.cfg.AuthToken)
	if err != nil {
		logs.WarnContextf(ctx, "[plan] publisher get storage config failed: session_id=%s err=%v", sessionID, err)
		storageConfig = nil
	}

	bucket := ""
	scheme := "s3"
	if storageConfig != nil {
		bucket = storageConfig.Bucket
		scheme = storageConfig.Scheme
	}

	storageURI := ""
	if bucket != "" {
		storageURI, err = storage.BuildURI(scheme, bucket, storageKey)
		if err != nil {
			return nil, &PlanPublishError{Phase: "uri", Err: fmt.Errorf("build storage uri: %w", err)}
		}
	}

	if err := uploadPlanWithRetry(ctx, p.cfg.ServerAddr, p.cfg.AuthToken, bucket, storageKey, []byte(content), mimeType, fileSize, sessionID, fileID, p.cfg.HTTPClient); err != nil {
		return nil, &PlanPublishError{Phase: "upload", Err: err}
	}

	directive := fmt.Sprintf(
		":::plan{\"file_id\":\"%s\",\"summary_lines\":%d,\"total_lines\":%d}\n%s\n:::",
		fileID,
		summaryEnd,
		totalLines,
		summaryContent,
	)

	logs.InfoContextf(ctx, "[plan] publisher complete: session_id=%s file_id=%s total_lines=%d summary_lines=%d storage_uri=%s",
		sessionID, fileID, totalLines, summaryEnd, storageURI)

	published := &messaging.PlanPublishedPayload{
		FileID:       fileID,
		Directive:    directive,
		SummaryLines: summaryEnd,
		TotalLines:   totalLines,
		StorageKey:   storageKey,
		StorageURI:   storageURI,
		Filename:     filename,
		OriginalName: displayPath,
		MimeType:     mimeType,
		FileSize:     fileSize,
		Sha256:       sha256Hex,
	}
	return &messaging.RunEventBody{
		Event: messaging.RunEventPlanPublished,
		Payload: messaging.RunEventPayload{
			PlanPublished: published,
		},
	}, nil
}

func readPlanFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxPlanFileSize+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxPlanFileSize {
		return "", errors.New("plan file exceeds the size limit")
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", errors.New("plan file is empty")
	}
	return string(content), nil
}

func uploadPlanWithRetry(ctx context.Context, serverAddr, authToken, bucket, key string, data []byte, mimeType string, fileSize int64, sessionID, fileID string, httpClient *http.Client) error {
	for attempt := 0; attempt < planUploadMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			logs.InfoContextf(ctx, "[plan] publisher retry %d/%d: session_id=%s file_id=%s backoff=%v", attempt+1, planUploadMaxRetries, sessionID, fileID, backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		logs.InfoContextf(ctx, "[plan] publisher upload attempt %d/%d: session_id=%s file_id=%s", attempt+1, planUploadMaxRetries, sessionID, fileID)
		if err := uploadPlanFile(ctx, serverAddr, authToken, bucket, key, data, mimeType, fileSize, httpClient); err != nil {
			logs.WarnContextf(ctx, "[plan] publisher upload attempt %d failed: session_id=%s file_id=%s err=%v", attempt+1, sessionID, fileID, err)
			continue
		}
		logs.InfoContextf(ctx, "[plan] publisher upload success: session_id=%s file_id=%s attempt=%d", sessionID, fileID, attempt+1)
		return nil
	}
	return fmt.Errorf("plan upload failed after %d retries", planUploadMaxRetries)
}

func uploadPlanFile(ctx context.Context, serverAddr, authToken, bucket, key string, data []byte, mimeType string, fileSize int64, httpClient *http.Client) error {
	uploadURL, err := cli.GetPresignUploadURL(ctx, serverAddr, authToken, bucket, key)
	if err != nil {
		return fmt.Errorf("get presign upload url: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create plan upload request: %w", err)
	}
	request.Header.Set("Content-Type", mimeType)
	request.ContentLength = fileSize

	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("upload plan file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf(
			"upload plan file returned %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}
	return nil
}
