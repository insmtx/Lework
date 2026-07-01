package opencode

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
	"regexp"
	"strings"
	"time"

	"github.com/ygpkg/storage-go"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

const (
	maxPlanFileSize      = 256 * 1024
	planSummaryLines     = 20
	planUploadMaxRetries = 3
)

var planQuestionPathPattern = regexp.MustCompile(`^Plan at (.+) is complete\.`)

// publishPlan resolves the plan path (using question fallback), reads the file,
// uploads it to object storage, and returns a PlanPublishedPayload. Returns an
// error if any step fails — including after all upload retries.
func (st *runState) publishPlan(ctx context.Context, questions []events.QuestionItem) (*events.PlanPublishedPayload, error) {
	path, displayPath, err := st.resolvePlanPath(questions)
	if err != nil {
		logs.WarnContextf(ctx, "[plan] publishPlan resolve path failed: session_id=%s err=%v", st.sessionID, err)
		return nil, err
	}
	logs.InfoContextf(ctx, "[plan] publishPlan path resolved: session_id=%s path=%s display_path=%s", st.sessionID, path, displayPath)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	content, err := readPlanFile(path)
	if err != nil {
		logs.WarnContextf(ctx, "[plan] publishPlan read file failed: session_id=%s path=%s err=%v", st.sessionID, path, err)
		return nil, fmt.Errorf("read plan file: %w", err)
	}
	logs.InfoContextf(ctx, "[plan] publishPlan file read: session_id=%s path=%s size=%d", st.sessionID, path, len(content))

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	summaryEnd := planSummaryLines
	if summaryEnd > totalLines {
		summaryEnd = totalLines
	}
	summaryContent := strings.Join(lines[:summaryEnd], "\n")

	fileID := "file_" + snowflake.GenerateIDBase58()
	filename := filepath.Base(path)
	if filename == "" || filename == "." {
		filename = "plan.md"
	}

	serverAddr := strings.TrimSpace(identity.ServerAddr())
	orgID := identity.OrgID()
	sessionID := strings.TrimSpace(st.sessionID)

	if serverAddr == "" || orgID == 0 || sessionID == "" {
		logs.WarnContextf(ctx, "[plan] publishPlan missing identity: server_addr=%q org_id=%d session_id=%q", serverAddr, orgID, sessionID)
		return nil, errors.New("missing identity")
	}

	storageKey := fmt.Sprintf("projects/%d/sess/%s/plans/%s.md", orgID, sessionID, fileID)
	fileSize := int64(len(content))
	hash := sha256.Sum256([]byte(content))
	sha256Hex := hex.EncodeToString(hash[:])
	mimeType := "text/markdown"

	logs.InfoContextf(ctx, "[plan] publishPlan preparing upload: session_id=%s file_id=%s storage_key=%s size=%d sha256=%s",
		sessionID, fileID, storageKey, fileSize, sha256Hex[:16])

	authToken := os.Getenv(leros.EnvAuthToken)
	storageConfig, err := cli.GetStorageConfig(ctx, serverAddr, authToken)
	if err != nil {
		logs.WarnContextf(ctx, "[plan] publishPlan get storage config failed: session_id=%s err=%v", sessionID, err)
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
			logs.WarnContextf(ctx, "[plan] publishPlan build uri failed: session_id=%s err=%v", sessionID, err)
			return nil, fmt.Errorf("build storage uri: %w", err)
		}
	}
	logs.InfoContextf(ctx, "[plan] publishPlan storage config: session_id=%s bucket=%s scheme=%s uri=%s", sessionID, bucket, scheme, storageURI)

	if err := uploadPlanWithRetry(ctx, serverAddr, authToken, bucket, storageKey, []byte(content), mimeType, fileSize, sessionID, fileID); err != nil {
		return nil, err
	}

	directive := fmt.Sprintf(
		":::plan{\"file_id\":\"%s\",\"summary_lines\":%d,\"total_lines\":%d}\n%s\n:::",
		fileID,
		summaryEnd,
		totalLines,
		summaryContent,
	)

	logs.InfoContextf(ctx, "[plan] publishPlan complete: session_id=%s file_id=%s total_lines=%d summary_lines=%d storage_uri=%s",
		sessionID, fileID, totalLines, summaryEnd, storageURI)

	return &events.PlanPublishedPayload{
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
	}, nil
}

// uploadPlanWithRetry attempts to upload the plan file with exponential backoff.
// Returns nil on first success. Backoff is cancelable via ctx.
func uploadPlanWithRetry(ctx context.Context, serverAddr, authToken, bucket, key string, data []byte, mimeType string, fileSize int64, sessionID, fileID string) error {
	for attempt := 0; attempt < planUploadMaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			logs.InfoContextf(ctx, "[plan] publishPlan retry %d/%d: session_id=%s file_id=%s backoff=%v", attempt+1, planUploadMaxRetries, sessionID, fileID, backoff)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		logs.InfoContextf(ctx, "[plan] publishPlan upload attempt %d/%d: session_id=%s file_id=%s", attempt+1, planUploadMaxRetries, sessionID, fileID)
		if err := uploadPlanFile(ctx, serverAddr, authToken, bucket, key, data, mimeType, fileSize); err != nil {
			logs.WarnContextf(ctx, "[plan] publishPlan upload attempt %d failed: session_id=%s file_id=%s err=%v", attempt+1, sessionID, fileID, err)
			continue
		}
		logs.InfoContextf(ctx, "[plan] publishPlan upload success: session_id=%s file_id=%s attempt=%d", sessionID, fileID, attempt+1)
		return nil
	}
	return fmt.Errorf("plan upload failed after %d retries", planUploadMaxRetries)
}

// uploadPlanFile uploads the plan file data to the presigned URL.
func uploadPlanFile(ctx context.Context, serverAddr, authToken, bucket, key string, data []byte, mimeType string, fileSize int64) error {
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

	response, err := http.DefaultClient.Do(request)
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

func (st *runState) resolvePlanPath(questions []events.QuestionItem) (string, string, error) {
	expectedName := ""
	if st.session != nil && st.session.Slug != "" && st.session.Time.Created > 0 {
		expectedName = fmt.Sprintf("%d-%s.md", st.session.Time.Created, st.session.Slug)
	}

	baseDir := st.workDir
	if strings.TrimSpace(baseDir) == "" && st.session != nil && strings.TrimSpace(st.session.Directory) != "" {
		baseDir = st.session.Directory
	}

	questionPath := extractPlanPath(questions)
	if questionPath != "" {
		path := questionPath
		if !filepath.IsAbs(path) {
			if strings.TrimSpace(baseDir) == "" {
				return "", questionPath, errors.New("plan file base directory is unavailable")
			}
			path = filepath.Join(baseDir, path)
		}
		path = filepath.Clean(path)
		if err := validatePlanPath(path, expectedName); err != nil {
			return "", questionPath, err
		}
		return path, questionPath, nil
	}

	if expectedName == "" || strings.TrimSpace(baseDir) == "" {
		return "", "", errors.New("plan file path is unavailable")
	}
	path := filepath.Join(baseDir, ".opencode", "plans", expectedName)
	displayPath := filepath.Join(".opencode", "plans", expectedName)
	return path, displayPath, nil
}

func validatePlanPath(path, expectedName string) error {
	if filepath.Ext(path) != ".md" || filepath.Base(filepath.Dir(path)) != "plans" {
		return errors.New("plan file path is invalid")
	}
	if expectedName != "" && filepath.Base(path) != expectedName {
		return errors.New("plan file does not match the current session")
	}
	return nil
}

func extractPlanPath(questions []events.QuestionItem) string {
	if questions == nil {
		return ""
	}
	for _, question := range questions {
		match := planQuestionPathPattern.FindStringSubmatch(strings.TrimSpace(question.Question))
		if len(match) == 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
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
