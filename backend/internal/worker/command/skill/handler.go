// Package skill handles cmd.skill lane commands (install, list, detail, uninstall, import).
// Handler implements command.SkillHandler, decoding WorkerCommand payloads
// and executing actions via CLI, catalog, and store packages.
package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	skilllinks "github.com/insmtx/Leros/backend/internal/assistant/bootstrap/skilllinks"
	"github.com/insmtx/Leros/backend/internal/cli"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/internal/skill/fetch"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

// ReplyPublisher is the minimal interface used by Handler to publish
// command replies via core NATS.
type ReplyPublisher interface {
	Publish(subject string, data []byte) error
}

var httpClient = &http.Client{Timeout: 5 * time.Minute}

// Handler handles cmd.skill lane commands.
// It decodes SkillCommandPayload and executes install/list/detail/uninstall/import actions,
// replying via ReplyPublisher.
type Handler struct {
	pub ReplyPublisher
}

// New creates a new skill handler.
func New(pub ReplyPublisher) (*Handler, error) {
	if pub == nil {
		return nil, fmt.Errorf("ReplyPublisher is required")
	}
	return &Handler{pub: pub}, nil
}

// HandleSkillCommand implements command.SkillHandler.
// Decodes the SkillCommandPayload and dispatches by action.
func (h *Handler) HandleSkillCommand(ctx context.Context, cmd messaging.WorkerCommand, msg *nats.Msg) error {
	payload, err := messaging.DecodeCommandPayload[messaging.SkillCommandPayload](&cmd.Body)
	if err != nil {
		return err
	}
	action := strings.TrimSpace(payload.Action)
	logs.InfoContextf(ctx,
		"Received skill management request: action=%s msg_id=%s org_id=%d worker_id=%d reply_to=%s",
		action, cmd.ID, cmd.Route.OrgID, cmd.Route.WorkerID, cmd.Body.ReplyTo,
	)

	switch action {
	case "install":
		return h.handleInstall(ctx, cmd, payload)
	case "list":
		return h.handleList(ctx, cmd, payload)
	case "uninstall":
		return h.handleUninstall(ctx, cmd, payload)
	case "detail":
		return h.handleDetail(ctx, cmd, payload)
	case "import":
		return h.handleImport(ctx, cmd, payload)
	default:
		return h.replyError(msg.Reply, fmt.Sprintf("unknown action: %s", action), nil)
	}
}

func (h *Handler) handleInstall(ctx context.Context, wcmd messaging.WorkerCommand, payload messaging.SkillCommandPayload) error {
	skillID := strings.TrimSpace(payload.SkillID)
	if skillID == "" {
		return h.replyError(wcmd.Body.ReplyTo, "skill_id is empty", nil)
	}

	source := strings.TrimSpace(payload.Source)
	version := strings.TrimSpace(payload.Version)

	// 优先尝试从 server download 缓存
	serverAddr := identity.ServerAddr()
	if serverAddr != "" && source != "" {
		downloaded, err := h.tryDownloadFromServer(ctx, skillID, source, version)
		if err == nil && downloaded != nil {
			logs.InfoContextf(ctx, "Cache HIT for %s/%s@%s from server %s", source, skillID, version, serverAddr)
			return h.installFromZip(ctx, downloaded, wcmd.Body.ReplyTo, source, skillID)
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
		return h.replyError(wcmd.Body.ReplyTo, "find leros binary", err)
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
		return h.replyError(wcmd.Body.ReplyTo, fmt.Sprintf("install skill %q", skillID), err)
	}

	logs.InfoContextf(ctx, "leros skill install succeeded for %q", skillID)
	if wcmd.Body.ReplyTo != "" {
		return h.replySuccess(wcmd.Body.ReplyTo, "install", fmt.Sprintf("skill %q installed", skillID))
	}
	return nil
}

// tryDownloadFromServer 尝试从 server download 接口获取缓存 zip。
// 成功返回 zip 字节，失败返回 error（调用方据此回退远程拉取）。
func (h *Handler) tryDownloadFromServer(ctx context.Context, skillID, source, version string) ([]byte, error) {
	serverAddr := identity.ServerAddr()
	if serverAddr == "" {
		return nil, fmt.Errorf("server addr not configured")
	}

	authToken := os.Getenv(leros.EnvAuthToken)
	data, err := cli.DownloadSkillCache(ctx, serverAddr, authToken, skillID, source, version)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// installFromZip 将 zip 字节解压并安装 skill，同步外部 symlink。
func (h *Handler) installFromZip(ctx context.Context, zipBytes []byte, replyTo, source, skillID string) error {
	skillContent, supportingFiles, err := extractZipSkill(zipBytes)
	if err != nil {
		return h.replyError(replyTo, "extract zip skill", err)
	}
	return h.installSkillContent(ctx, skillContent, supportingFiles, replyTo, "install", source, skillID)
}

func (h *Handler) installSkillContent(ctx context.Context, skillContent []byte, supportingFiles map[string][]byte, replyTo, action, source, skillID string) error {
	manifest, _, err := catalog.ParseDocument(skillContent)
	if err != nil {
		return h.replyError(replyTo, "parse SKILL.md", err)
	}
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return h.replyError(replyTo, "SKILL.md has no name", nil)
	}

	skillsDir, err := leros.SkillsDir()
	if err != nil {
		return h.replyError(replyTo, "resolve skills dir", err)
	}
	store, err := skillstore.NewSkillStore(skillsDir)
	if err != nil {
		return h.replyError(replyTo, "create skill store", err)
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
		return h.replyError(replyTo, "install skill", err)
	}
	if !result.Success {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = "unknown install error"
		}
		return h.replyError(replyTo, fmt.Sprintf("install skill: %s", errMsg), nil)
	}

	// 同步外部 skill symlink
	knownCLISkillDirs := []string{
		"~/.claude/skills",
		"~/.agents/skills",
	}
	if err := skilllinks.EnsureExternalSkillLink(name, knownCLISkillDirs); err != nil {
		logs.WarnContextf(ctx, "sync external links for %q: %v", name, err)
	}

	logs.InfoContextf(ctx, "Skill %s succeeded for %q", action, name)
	if replyTo != "" {
		resultVerb := "installed"
		if action == "import" {
			resultVerb = "imported"
		}
		return h.replySuccess(replyTo, action, fmt.Sprintf("skill %q %s", name, resultVerb))
	}
	return nil
}

func (h *Handler) handleList(ctx context.Context, wcmd messaging.WorkerCommand, payload messaging.SkillCommandPayload) error {
	lerosBin, err := os.Executable()
	if err != nil {
		return h.replyError(wcmd.Body.ReplyTo, "find leros binary", err)
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
			return h.replyError(wcmd.Body.ReplyTo, fmt.Sprintf("list skills: %s", errDetail), err)
		}
		logs.ErrorContextf(ctx, "leros skill list failed: %v", err)
		return h.replyError(wcmd.Body.ReplyTo, "list skills", err)
	}

	var items []messaging.SkillListItem
	if err := json.Unmarshal(output, &items); err != nil {
		logs.ErrorContextf(ctx, "Failed to unmarshal skill list output: %v, raw=%s", err, string(output))
		return h.replyError(wcmd.Body.ReplyTo, "unmarshal list output", err)
	}

	resp := messaging.WorkerCommandResult{
		Success: true,
		Action:  "list",
	}
	// Use json.RawMessage so that the items serialize as raw JSON
	// rather than being base64-encoded (which happens with []byte -> any).
	if data, err := json.Marshal(items); err != nil {
		return h.replyError(wcmd.Body.ReplyTo, "marshal list data", err)
	} else {
		resp.Data = json.RawMessage(data)
	}
	return h.publishReply(wcmd.Body.ReplyTo, resp)
}

func (h *Handler) handleUninstall(ctx context.Context, wcmd messaging.WorkerCommand, payload messaging.SkillCommandPayload) error {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return h.replyError(wcmd.Body.ReplyTo, "name is empty", nil)
	}

	lerosBin, err := os.Executable()
	if err != nil {
		return h.replyError(wcmd.Body.ReplyTo, "find leros binary", err)
	}

	cmd := exec.CommandContext(ctx, lerosBin, "skill", "uninstall", name, "--yes")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logs.InfoContextf(ctx, "Running: %s skill uninstall %s --yes", lerosBin, name)
	if err := cmd.Run(); err != nil {
		logs.ErrorContextf(ctx, "leros skill uninstall failed for %q: %v", name, err)
		return h.replyError(wcmd.Body.ReplyTo, fmt.Sprintf("uninstall skill %q", name), err)
	}

	logs.InfoContextf(ctx, "leros skill uninstall succeeded for %q", name)
	if wcmd.Body.ReplyTo != "" {
		return h.replySuccess(wcmd.Body.ReplyTo, "uninstall", fmt.Sprintf("skill %q uninstalled", name))
	}
	return nil
}

func (h *Handler) handleDetail(ctx context.Context, wcmd messaging.WorkerCommand, payload messaging.SkillCommandPayload) error {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return h.replyError(wcmd.Body.ReplyTo, "name is empty", nil)
	}

	entry, err := catalog.Get(name)
	if err != nil {
		logs.ErrorContextf(ctx, "Failed to get skill detail for %q: %v", name, err)
		return h.replyError(wcmd.Body.ReplyTo, fmt.Sprintf("get skill detail %q", name), err)
	}

	// catalog.ListFiles excludes SKILL.md by design; always include it as the primary file.
	supportingFiles, _ := catalog.ListFiles(name, 0)
	files := append([]string{"SKILL.md"}, supportingFiles...)

	data := messaging.SkillDetailData{
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

	resp := messaging.WorkerCommandResult{
		Success: true,
		Action:  "detail",
	}
	// Use json.RawMessage so that the detail data serializes as raw JSON
	// rather than being base64-encoded.
	if data, err := json.Marshal(data); err != nil {
		return h.replyError(wcmd.Body.ReplyTo, "marshal detail data", err)
	} else {
		resp.Data = json.RawMessage(data)
	}
	return h.publishReply(wcmd.Body.ReplyTo, resp)
}

// handleImport downloads a skill file from a URL (local path or HTTP), extracts
// SKILL.md and supporting files, then installs into the skills directory by name.
func (h *Handler) handleImport(ctx context.Context, wcmd messaging.WorkerCommand, payload messaging.SkillCommandPayload) error {
	if strings.EqualFold(strings.TrimSpace(payload.Source), "github") {
		skillID := strings.TrimSpace(payload.SkillID)
		if skillID == "" {
			return h.replyError(wcmd.Body.ReplyTo, "skill_id is empty", nil)
		}
		bundle, err := fetch.NewGitHubSource().FetchVersion(ctx, skillID, strings.TrimSpace(payload.Version))
		if err != nil {
			return h.replyError(wcmd.Body.ReplyTo, "fetch GitHub skill", err)
		}
		if bundle.TempDir != "" {
			defer os.RemoveAll(bundle.TempDir)
		}
		return h.installSkillContent(ctx, bundle.Content, bundle.Files, wcmd.Body.ReplyTo, "import", payload.Source, payload.SkillID)
	}

	// Prefer the dedicated DownloadURL field; fall back to SkillID for
	// backward compatibility with older server versions.
	sourceURL := strings.TrimSpace(payload.DownloadURL)
	if sourceURL == "" {
		sourceURL = strings.TrimSpace(payload.SkillID)
	}
	if sourceURL == "" {
		return h.replyError(wcmd.Body.ReplyTo, "download_url (or skill_id) is empty", nil)
	}

	// 1. Download from URL (supports local paths and HTTP)
	fileBytes, contentType, err := downloadFromURL(ctx, sourceURL)
	if err != nil {
		return h.replyError(wcmd.Body.ReplyTo, "download skill file", err)
	}

	// 2. Extract SKILL.md content and supporting files
	var skillContent []byte
	var supportingFiles map[string][]byte

	if isZipContent(fileBytes, contentType) {
		sc, sf, err := extractZipSkill(fileBytes)
		if err != nil {
			return h.replyError(wcmd.Body.ReplyTo, "extract zip skill", err)
		}
		skillContent = sc
		supportingFiles = sf
	} else {
		skillContent = fileBytes
		supportingFiles = nil
	}

	return h.installSkillContent(ctx, skillContent, supportingFiles, wcmd.Body.ReplyTo, "import", payload.Source, payload.SkillID)
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
func (h *Handler) replySuccess(replyTo, action, message string) error {
	resp := messaging.WorkerCommandResult{
		Success: true,
		Action:  action,
		Message: message,
	}
	return h.publishReply(replyTo, resp)
}

// replyError publishes an error response to the reply inbox.
func (h *Handler) replyError(replyTo, context string, err error) error {
	errMsg := context
	if err != nil {
		errMsg = fmt.Sprintf("%s: %v", context, err)
	}
	resp := messaging.WorkerCommandResult{
		Success: false,
		Error:   errMsg,
	}
	if pubErr := h.publishReply(replyTo, resp); pubErr != nil {
		return fmt.Errorf("%s (and failed to publish reply: %v)", errMsg, pubErr)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return fmt.Errorf("%s", context)
}

// publishReply publishes a response to the given reply inbox via core NATS.
func (h *Handler) publishReply(replyTo string, resp messaging.WorkerCommandResult) error {
	if replyTo == "" {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}
	return h.pub.Publish(replyTo, data)
}
