// Package skillmgmt provides a NATS consumer that handles unified skill management
// requests (install, list, uninstall) by shelling out to the leros CLI.
package skillmgmt

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/insmtx/Leros/backend/engines"
	eventbus "github.com/insmtx/Leros/backend/internal/infra/mq"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/internal/worker/protocol"
	"github.com/insmtx/Leros/backend/pkg/dm"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

const consumerName = "worker-skill-mgmt"

// httpClient is a shared HTTP client with a reasonable timeout for skill file downloads.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// Config holds the configuration for a skill management consumer.
type Config struct {
	OrgID    uint
	WorkerID uint
}

// Consumer subscribes to skill management requests and dispatches by action.
type Consumer struct {
	cfg        Config
	subscriber eventbus.Subscriber
	conn       *nats.Conn
}

// New creates a new skill management consumer.
// The conn parameter is required for publishing reply messages via core NATS.
func New(cfg Config, subscriber eventbus.Subscriber, conn *nats.Conn) (*Consumer, error) {
	if cfg.OrgID == 0 {
		return nil, fmt.Errorf("org_id is required")
	}
	if cfg.WorkerID == 0 {
		return nil, fmt.Errorf("worker_id is required")
	}
	if subscriber == nil {
		return nil, fmt.Errorf("subscriber is required")
	}
	if conn == nil {
		return nil, fmt.Errorf("NATS connection is required")
	}
	return &Consumer{cfg: cfg, subscriber: subscriber, conn: conn}, nil
}

// Topic returns the NATS subject for this consumer.
func (c *Consumer) Topic() string {
	topic, err := dm.WorkerSkillSubject(c.cfg.OrgID, c.cfg.WorkerID)
	if err != nil {
		logs.Errorf("Failed to build skill management topic: %v", err)
		return ""
	}
	return topic
}

// Start subscribes to the skill management topic and processes incoming requests.
func (c *Consumer) Start(ctx context.Context) error {
	topic := c.Topic()
	logs.InfoContextf(ctx, "Starting skill management subscription: %s", topic)
	return c.subscriber.Subscribe(ctx, topic, consumerName, func(msg *nats.Msg) {
		if err := c.handle(ctx, msg); err != nil {
			logs.ErrorContextf(ctx, "Failed to handle skill management request: %v", err)
		}
	})
}

func (c *Consumer) handle(ctx context.Context, msg *nats.Msg) error {
	var req protocol.SkillManagementMessage
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		return c.replyError("", "unmarshal request", err)
	}

	action := strings.TrimSpace(req.Body.Action)
	logs.InfoContextf(ctx,
		"Received skill management request: action=%s msg_id=%s org_id=%d worker_id=%d reply_to=%s",
		action, req.ID, req.Route.OrgID, req.Route.WorkerID, req.Body.ReplyTo,
	)

	switch action {
	case "install":
		return c.handleInstall(ctx, req)
	case "list":
		return c.handleList(ctx, req)
	case "uninstall":
		return c.handleUninstall(ctx, req)
	case "detail":
		return c.handleDetail(ctx, req)
	case "import":
		return c.handleImport(ctx, req)
	default:
		return c.replyError(req.Body.ReplyTo, fmt.Sprintf("unknown action: %s", action), nil)
	}
}

func (c *Consumer) handleInstall(ctx context.Context, req protocol.SkillManagementMessage) error {
	skillID := strings.TrimSpace(req.Body.SkillID)
	if skillID == "" {
		return c.replyError(req.Body.ReplyTo, "skill_id is empty", nil)
	}

	source := strings.TrimSpace(req.Body.Source)
	version := strings.TrimSpace(req.Body.Version)

	// 优先尝试从 server download 缓存
	serverAddr := identity.ServerAddr()
	if serverAddr != "" && source != "" {
		downloaded, err := c.tryDownloadFromServer(ctx, skillID, source, version)
		if err == nil && downloaded != nil {
			logs.InfoContextf(ctx, "Cache HIT for %s/%s@%s from server %s", source, skillID, version, serverAddr)
			return c.installFromZip(ctx, downloaded, req.Body.ReplyTo, source, skillID)
		}
		if err != nil {
			logs.InfoContextf(ctx, "Cache MISS for %s/%s@%s: %v, falling back to remote fetch", source, skillID, version, err)
		} else {
			logs.InfoContextf(ctx, "Cache MISS for %s/%s@%s, falling back to remote fetch", source, skillID, version)
		}
	}

	// 回退到 CLI 远程拉取
	lerosBin, err := os.Executable()
	if err != nil {
		return c.replyError(req.Body.ReplyTo, "find leros binary", err)
	}

	args := []string{"skill", "install", skillID, "--force", "--yes"}
	if source != "" {
		args = append(args, "--source", source)
	}
	if version != "" {
		args = append(args, "--version", version)
	}

	cmd := exec.CommandContext(ctx, lerosBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logs.InfoContextf(ctx, "Running: %s %s", lerosBin, strings.Join(args, " "))
	if err := cmd.Run(); err != nil {
		logs.ErrorContextf(ctx, "leros skill install failed for %q: %v", skillID, err)
		return c.replyError(req.Body.ReplyTo, fmt.Sprintf("install skill %q", skillID), err)
	}

	logs.InfoContextf(ctx, "leros skill install succeeded for %q", skillID)
	if req.Body.ReplyTo != "" {
		return c.replySuccess(req.Body.ReplyTo, "install", fmt.Sprintf("skill %q installed", skillID))
	}
	return nil
}

// tryDownloadFromServer 尝试从 server download 接口获取缓存 zip。
// 成功返回 zip 字节，失败返回 error（调用方据此回退远程拉取）。
func (c *Consumer) tryDownloadFromServer(ctx context.Context, skillID, source, version string) ([]byte, error) {
	serverAddr := identity.ServerAddr()
	if serverAddr == "" {
		return nil, fmt.Errorf("server addr not configured")
	}

	baseURL := fmt.Sprintf("http://%s/v1/skill-marketplace/skills/%s/download", serverAddr, skillID)
	reqURL := fmt.Sprintf("%s?source=%s&version=%s", baseURL, url.QueryEscape(source), url.QueryEscape(version))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (404)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 100_000_000))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// installFromZip 将 zip 字节解压并安装 skill，同步外部 symlink。
func (c *Consumer) installFromZip(ctx context.Context, zipBytes []byte, replyTo, source, skillID string) error {
	skillContent, supportingFiles, err := extractZipSkill(zipBytes)
	if err != nil {
		return c.replyError(replyTo, "extract zip skill", err)
	}
	return c.installSkillContent(ctx, skillContent, supportingFiles, replyTo, "install", source, skillID)
}

func (c *Consumer) installSkillContent(ctx context.Context, skillContent []byte, supportingFiles map[string][]byte, replyTo, action, source, skillID string) error {
	manifest, _, err := catalog.ParseDocument(skillContent)
	if err != nil {
		return c.replyError(replyTo, "parse SKILL.md", err)
	}
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return c.replyError(replyTo, "SKILL.md has no name", nil)
	}

	skillsDir, err := leros.SkillsDir()
	if err != nil {
		return c.replyError(replyTo, "resolve skills dir", err)
	}
	store, err := skillstore.NewSkillStore(skillsDir)
	if err != nil {
		return c.replyError(replyTo, "create skill store", err)
	}

	files := make(map[string]string, len(supportingFiles))
	for relPath, data := range supportingFiles {
		files[relPath] = string(data)
	}

	result, err := store.Install(ctx, skillstore.InstallRequest{
		Name:    name,
		Content: string(skillContent),
		Files:   files,
		Force:   true,
		Source:  source,
		SkillID: skillID,
	})
	if err != nil {
		return c.replyError(replyTo, "install skill", err)
	}
	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unknown install error"
		}
		return c.replyError(replyTo, fmt.Sprintf("install skill: %s", errMsg), nil)
	}

	// 同步外部 skill symlink
	knownCLISkillDirs := []string{
		"~/.claude/skills",
		"~/.agents/skills",
	}
	if err := engines.EnsureExternalSkillLink(name, knownCLISkillDirs); err != nil {
		logs.WarnContextf(ctx, "sync external links for %q: %v", name, err)
	}

	logs.InfoContextf(ctx, "Skill %s succeeded for %q", action, name)
	if replyTo != "" {
		resultVerb := "installed"
		if action == "import" {
			resultVerb = "imported"
		}
		return c.replySuccess(replyTo, action, fmt.Sprintf("skill %q %s", name, resultVerb))
	}
	return nil
}

func (c *Consumer) handleList(ctx context.Context, req protocol.SkillManagementMessage) error {
	lerosBin, err := os.Executable()
	if err != nil {
		return c.replyError(req.Body.ReplyTo, "find leros binary", err)
	}

	cmd := exec.CommandContext(ctx, lerosBin, "skill", "list", "--json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		errDetail := stderr.String()
		if errDetail == "" {
			if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
				errDetail = string(exitErr.Stderr)
			}
		}
		if errDetail != "" {
			errDetail = strings.TrimSpace(errDetail)
			logs.ErrorContextf(ctx, "leros skill list failed: %v, stderr: %s", err, errDetail)
			return c.replyError(req.Body.ReplyTo, fmt.Sprintf("list skills: %s", errDetail), err)
		}
		logs.ErrorContextf(ctx, "leros skill list failed: %v", err)
		return c.replyError(req.Body.ReplyTo, "list skills", err)
	}

	var items []protocol.SkillListItem
	if err := json.Unmarshal(output, &items); err != nil {
		logs.ErrorContextf(ctx, "Failed to unmarshal skill list output: %v, raw=%s", err, string(output))
		return c.replyError(req.Body.ReplyTo, "unmarshal list output", err)
	}

	resp := protocol.SkillManagementResponse{
		Success: true,
		Action:  "list",
	}
	if data, err := json.Marshal(items); err != nil {
		return c.replyError(req.Body.ReplyTo, "marshal list data", err)
	} else {
		resp.Data = data
	}
	return c.publishReply(req.Body.ReplyTo, resp)
}

func (c *Consumer) handleUninstall(ctx context.Context, req protocol.SkillManagementMessage) error {
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return c.replyError(req.Body.ReplyTo, "name is empty", nil)
	}

	lerosBin, err := os.Executable()
	if err != nil {
		return c.replyError(req.Body.ReplyTo, "find leros binary", err)
	}

	cmd := exec.CommandContext(ctx, lerosBin, "skill", "uninstall", name, "--yes")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logs.InfoContextf(ctx, "Running: %s skill uninstall %s --yes", lerosBin, name)
	if err := cmd.Run(); err != nil {
		logs.ErrorContextf(ctx, "leros skill uninstall failed for %q: %v", name, err)
		return c.replyError(req.Body.ReplyTo, fmt.Sprintf("uninstall skill %q", name), err)
	}

	logs.InfoContextf(ctx, "leros skill uninstall succeeded for %q", name)
	if req.Body.ReplyTo != "" {
		return c.replySuccess(req.Body.ReplyTo, "uninstall", fmt.Sprintf("skill %q uninstalled", name))
	}
	return nil
}

func (c *Consumer) handleDetail(ctx context.Context, req protocol.SkillManagementMessage) error {
	name := strings.TrimSpace(req.Body.Name)
	if name == "" {
		return c.replyError(req.Body.ReplyTo, "name is empty", nil)
	}

	entry, err := catalog.Get(name)
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to get skill detail for %q: %v", name, err)
		return c.replyError(req.Body.ReplyTo, fmt.Sprintf("get skill detail %q", name), err)
	}

	// catalog.ListFiles excludes SKILL.md by design; always include it as the primary file.
	supportingFiles, _ := catalog.ListFiles(name, 0)
	files := append([]string{"SKILL.md"}, supportingFiles...)

	data := protocol.SkillDetailData{
		Name:        entry.Manifest.Name,
		Description: entry.Manifest.Description,
		Category:    entry.Manifest.Metadata.Category,
		Source:      entry.Summary().Source,
		Trust:       entry.Summary().Trust,
		Version:     entry.Manifest.Version,
		SkillMD:     entry.Body,
		Tags:        entry.Manifest.Metadata.Tags,
		Files:       files,
	}

	resp := protocol.SkillManagementResponse{
		Success: true,
		Action:  "detail",
	}
	if data, err := json.Marshal(data); err != nil {
		return c.replyError(req.Body.ReplyTo, "marshal detail data", err)
	} else {
		resp.Data = data
	}
	return c.publishReply(req.Body.ReplyTo, resp)
}

// handleImport downloads a skill file from a URL (local path or HTTP), extracts
// SKILL.md and supporting files, then installs into the skills directory by name.
func (c *Consumer) handleImport(ctx context.Context, req protocol.SkillManagementMessage) error {
	if strings.EqualFold(strings.TrimSpace(req.Body.Source), "github") {
		skillID := strings.TrimSpace(req.Body.SkillID)
		if skillID == "" {
			return c.replyError(req.Body.ReplyTo, "skill_id is empty", nil)
		}
		bundle, err := fetch.NewGitHubSource().FetchVersion(ctx, skillID, strings.TrimSpace(req.Body.Version))
		if err != nil {
			return c.replyError(req.Body.ReplyTo, "fetch GitHub skill", err)
		}
		if bundle.TempDir != "" {
			defer os.RemoveAll(bundle.TempDir)
		}
		return c.installSkillContent(ctx, bundle.Content, bundle.Files, req.Body.ReplyTo, "import", req.Body.Source, req.Body.SkillID)
	}

	// Prefer the dedicated DownloadURL field; fall back to SkillID for
	// backward compatibility with older server versions.
	sourceURL := strings.TrimSpace(req.Body.DownloadURL)
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(req.Body.SkillID)
	}
	if sourceURL == "" {
		return c.replyError(req.Body.ReplyTo, "download_url (or skill_id) is empty", nil)
	}

	// 1. Download from URL (supports local paths and HTTP)
	fileBytes, contentType, err := downloadFromURL(ctx, sourceURL)
	if err != nil {
		return c.replyError(req.Body.ReplyTo, "download skill file", err)
	}

	// 2. Extract SKILL.md content and supporting files
	var skillContent []byte
	var supportingFiles map[string][]byte

	if isZipContent(fileBytes, contentType) {
		sc, sf, err := extractZipSkill(fileBytes)
		if err != nil {
			return c.replyError(req.Body.ReplyTo, "extract zip skill", err)
		}
		skillContent = sc
		supportingFiles = sf
	} else {
		skillContent = fileBytes
		supportingFiles = nil
	}

	return c.installSkillContent(ctx, skillContent, supportingFiles, req.Body.ReplyTo, "import", req.Body.Source, req.Body.SkillID)
}

// downloadFromURL downloads file content from a URL or local path.
func downloadFromURL(ctx context.Context, urlStr string) ([]byte, string, error) {
	// Local file path
	if strings.HasPrefix(urlStr, "/") || strings.HasPrefix(urlStr, "file://") {
		filePath := strings.TrimPrefix(urlStr, "file://")
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, "", fmt.Errorf("read local file %s: %w", filePath, err)
		}
		contentType := http.DetectContentType(data)
		return data, contentType, nil
	}

	// HTTP(S) URL
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("download URL: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("URL returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 100_000_000))
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

const maxPerFileSize = 1_048_576 // 1MB per file — consistent with service validateZipSkill

// isZipContent detects whether the bytes represent a ZIP archive.
func isZipContent(data []byte, contentType string) bool {
	if strings.Contains(contentType, "zip") ||
		strings.Contains(contentType, "application/octet-stream") {
		return len(data) >= 4 &&
			data[0] == 0x50 && data[1] == 0x4B &&
			data[2] == 0x03 && data[3] == 0x04
	}
	// Check zip magic bytes regardless of content type
	return len(data) >= 4 &&
		data[0] == 0x50 && data[1] == 0x4B &&
		data[2] == 0x03 && data[3] == 0x04
}

// extractZipSkill extracts SKILL.md and supporting files from a ZIP archive.
func extractZipSkill(zipBytes []byte) ([]byte, map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, nil, fmt.Errorf("open zip: %w", err)
	}

	var skillContent []byte
	files := make(map[string][]byte)
	allowedSubdirs := map[string]bool{
		"assets": true, "references": true, "scripts": true, "templates": true,
	}

	for _, f := range reader.File {
		name := filepath.ToSlash(f.Name)

		// Path traversal protection
		if filepath.IsAbs(name) || strings.Contains(name, "../") {
			continue
		}

		if f.FileInfo().IsDir() {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxPerFileSize))
		rc.Close()
		if err != nil {
			continue
		}

		base := filepath.Base(name)
		if strings.EqualFold(base, "SKILL.md") {
			skillContent = data
		} else {
			// Support any nesting depth within allowed subdirectories.
			topDir, _, hasDir := strings.Cut(name, "/")
			if hasDir && allowedSubdirs[topDir] {
				files[name] = data
			}
		}
	}

	if skillContent == nil {
		return nil, nil, fmt.Errorf("SKILL.md not found in zip")
	}
	return skillContent, files, nil
}

// replySuccess publishes a success response to the reply inbox.
func (c *Consumer) replySuccess(replyTo, action, message string) error {
	resp := protocol.SkillManagementResponse{
		Success: true,
		Action:  action,
		Message: message,
	}
	return c.publishReply(replyTo, resp)
}

// replyError publishes an error response to the reply inbox.
func (c *Consumer) replyError(replyTo, context string, err error) error {
	errMsg := context
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", context, err)
	}
	resp := protocol.SkillManagementResponse{
		Success: false,
		Error:   errMsg,
	}
	if pubErr := c.publishReply(replyTo, resp); pubErr != nil {
		return fmt.Errorf("%s (and failed to publish reply: %v)", errMsg, pubErr)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return fmt.Errorf("%s", context)
}

// publishReply publishes a response to the given reply inbox via core NATS.
func (c *Consumer) publishReply(replyTo string, resp protocol.SkillManagementResponse) error {
	if replyTo == "" {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}
	return c.conn.Publish(replyTo, data)
}
