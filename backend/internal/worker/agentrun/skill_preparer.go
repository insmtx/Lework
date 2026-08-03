package agentrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/insmtx/Leros/backend/internal/service"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

// SkillPreparer materializes the strictly scoped Skill view used by one run.
type SkillPreparer interface {
	PrepareSkills(context.Context, *agentrundomain.RunRequest, WorkspacePreparation) (string, func(), error)
}

// SkillBaselineCommitter records server-installed Skills as the local Git baseline.
type SkillBaselineCommitter interface {
	CommitInstalled(context.Context, []string) error
}

// PluginSkillPreparer installs project Skill bundles into the worker workspace
// and creates only symlinks in the task-private Skill view.
type PluginSkillPreparer struct {
	serverAddr        string
	authToken         string
	httpClient        *http.Client
	baselineCommitter SkillBaselineCommitter
}

type skillInstallRecord struct {
	SHA256   string
	Revision int
}

type skillInstallManifest struct {
	Records      map[string]skillInstallRecord
	RefreshCodes []string
	Warnings     []string
}

type skillDownloadURLResponse struct {
	Code        string `json:"code"`
	Revision    int    `json:"revision"`
	SHA256      string `json:"sha256"`
	DownloadURL string `json:"download_url"`
}

type connectorSkillDownloadRef struct {
	PluginID string `json:"plugin_id"`
	Revision int    `json:"revision"`
}

var skillInstallLocks sync.Map // map[string]*sync.Mutex, keyed by the worker skills root.

// NewPluginSkillPreparer creates the worker implementation. A zero server
// address is valid only when no project Skill artifact needs downloading.
func NewPluginSkillPreparer(serverAddr, authToken string) *PluginSkillPreparer {
	return NewPluginSkillPreparerWithBaseline(serverAddr, authToken, nil)
}

// NewPluginSkillPreparerWithBaseline creates a preparer that commits server
// installs so they cannot be mistaken for Run-created Skill changes.
func NewPluginSkillPreparerWithBaseline(
	serverAddr, authToken string,
	baselineCommitter SkillBaselineCommitter,
) *PluginSkillPreparer {
	return &PluginSkillPreparer{
		serverAddr: strings.TrimSpace(serverAddr), authToken: strings.TrimSpace(authToken),
		httpClient: &http.Client{}, baselineCommitter: baselineCommitter,
	}
}

func (p *PluginSkillPreparer) PrepareSkills(ctx context.Context, req *agentrundomain.RunRequest, workspace WorkspacePreparation) (string, func(), error) {
	if req == nil {
		return "", func() {}, fmt.Errorf("run request is required")
	}
	viewRoot, err := taskSkillViewRoot(workspace)
	if err != nil {
		return "", func() {}, err
	}
	if err := resetTaskSkillView(viewRoot); err != nil {
		return "", func() {}, fmt.Errorf("prepare task skill directory: %w", err)
	}
	cleanup := func() { removeSkillLinks(viewRoot) }
	if err := linkSkillChildren(systemSkillsDir(), viewRoot); err != nil {
		cleanup()
		return "", cleanup, fmt.Errorf("link worker system skills: %w", err)
	}
	p.preparePluginSkills(ctx, req.Plugins, viewRoot)
	if err := p.prepareInvokedSkills(ctx, req.Input.Messages, viewRoot); err != nil {
		return "", cleanup, fmt.Errorf("prepare invoked skills: %w", err)
	}
	return viewRoot, cleanup, nil
}

func (p *PluginSkillPreparer) preparePluginSkills(ctx context.Context, snapshots []agentrundomain.PluginSnapshot, viewRoot string) {
	skillsRoot, err := leros.JoinWorkspace(".leros", "skills")
	if err != nil {
		logs.WarnContextf(ctx, "resolve worker Skill directory failed: %v", err)
		return
	}
	lockValue, _ := skillInstallLocks.LoadOrStore(skillsRoot, &sync.Mutex{})
	installLock := lockValue.(*sync.Mutex)
	installLock.Lock()
	defer installLock.Unlock()
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		logs.WarnContextf(ctx, "create worker Skill directory failed: %v", err)
		return
	}
	manifestPath := filepath.Join(skillsRoot, ".seed-manifest")
	installed, err := p.readAndRepairSkillInstallManifest(ctx, manifestPath)
	if err != nil {
		logs.WarnContextf(ctx, "read worker Skill install manifest failed: %v", err)
		return
	}
	pending := make(map[string]struct{})
	standardPending := make(map[string]struct{})
	available := make(map[string]string)
	connectorRefs := make([]connectorSkillDownloadRef, 0)
	for _, snapshot := range sortedPluginSnapshots(snapshots) {
		code, revision, connectorRef, ok := pluginSnapshotSkill(snapshot)
		if !ok {
			continue
		}
		code, err := organizationSkillName(code)
		if err != nil || revision <= 0 {
			logs.WarnContextf(ctx, "skip invalid project Skill %q: code=%v revision=%d", snapshot.Code, err, snapshot.Revision)
			continue
		}
		content := filepath.Join(skillsRoot, code)
		if record, ok := installed[code]; ok && record.Revision == revision && hasSkillDocument(content) {
			available[code] = content
			continue
		}
		pending[code] = struct{}{}
		if connectorRef != nil {
			connectorRefs = append(connectorRefs, *connectorRef)
		} else {
			standardPending[code] = struct{}{}
		}
	}
	if len(pending) > 0 {
		codes := make([]string, 0, len(pending))
		for code := range pending {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		standardCodes := make([]string, 0, len(standardPending))
		for code := range standardPending {
			standardCodes = append(standardCodes, code)
		}
		sort.Strings(standardCodes)
		downloads, err := p.resolveDownloadURLs(ctx, standardCodes, connectorRefs)
		if err != nil {
			logs.WarnContextf(ctx, "resolve project Skill download URLs failed: %v", err)
		} else {
			installedCodes := make([]string, 0, len(codes))
			for _, code := range codes {
				download, ok := downloads[code]
				if !ok {
					logs.WarnContextf(ctx, "skip project Skill %q: server returned no download URL", code)
					continue
				}
				content, err := installSkillFromURL(ctx, skillsRoot, code, download)
				if err != nil {
					logs.WarnContextf(ctx, "skip project Skill %q: %v", code, err)
					continue
				}
				installed[code] = skillInstallRecord{SHA256: download.SHA256, Revision: download.Revision}
				available[code] = content
				installedCodes = append(installedCodes, code)
				logs.InfoContextf(ctx, "installed project Skill %q revision=%d", code, download.Revision)
			}
			if err := writeSkillInstallManifest(manifestPath, installed); err != nil {
				logs.WarnContextf(ctx, "write worker Skill install manifest failed: %v", err)
			} else {
				p.commitInstalledBaseline(ctx, installedCodes)
			}
		}
	}
	for code, content := range available {
		if err := replaceRunSkillLink(content, filepath.Join(viewRoot, code)); err != nil {
			logs.WarnContextf(ctx, "skip project Skill %q link: %v", code, err)
		}
	}
}

// prepareInvokedSkills installs the latest organization Skill when an explicit
// invocation is not actually available in the prepared run view.
func (p *PluginSkillPreparer) prepareInvokedSkills(ctx context.Context, messages []agentrundomain.InputMessage, viewRoot string) error {
	missing := make(map[string]struct{})
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, token := range skilltoken.ParseTokensOnly(message.Content) {
			code, err := organizationSkillName(token)
			if err != nil {
				logs.WarnContextf(ctx, "skip invalid invoked Skill %q: %v", token, err)
				continue
			}
			if isSystemSkill(code) || hasSkillDocument(filepath.Join(viewRoot, code)) {
				continue
			}
			if _, err := os.Lstat(filepath.Join(viewRoot, code)); err != nil && !os.IsNotExist(err) {
				logs.WarnContextf(ctx, "inspect invoked Skill %q in run view: %v", code, err)
				return fmt.Errorf("inspect Skill %q in run view: %w", code, err)
			}
			missing[code] = struct{}{}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	codes := make([]string, 0, len(missing))
	for code := range missing {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return p.installLatestSkills(ctx, codes, viewRoot)
}

func isSystemSkill(code string) bool {
	return hasSkillDocument(filepath.Join(systemSkillsDir(), code))
}

func (p *PluginSkillPreparer) installLatestSkills(ctx context.Context, codes []string, viewRoot string) error {
	skillsRoot, err := leros.JoinWorkspace(".leros", "skills")
	if err != nil {
		return fmt.Errorf("resolve worker Skill directory: %w", err)
	}
	lockValue, _ := skillInstallLocks.LoadOrStore(skillsRoot, &sync.Mutex{})
	installLock := lockValue.(*sync.Mutex)
	installLock.Lock()
	defer installLock.Unlock()
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return fmt.Errorf("create worker Skill directory: %w", err)
	}
	manifestPath := filepath.Join(skillsRoot, ".seed-manifest")
	installed, err := p.readAndRepairSkillInstallManifest(ctx, manifestPath)
	if err != nil {
		return fmt.Errorf("read worker Skill install manifest: %w", err)
	}
	downloads, err := p.resolveDownloadURLs(ctx, codes, nil)
	if err != nil {
		return fmt.Errorf("resolve download URLs: %w", err)
	}
	changed := false
	installedCodes := make([]string, 0, len(codes))
	installErrors := make([]error, 0)
	for _, code := range codes {
		download, ok := downloads[code]
		if !ok {
			logs.WarnContextf(ctx, "skip invoked Skill %q: server returned no download URL", code)
			installErrors = append(installErrors, fmt.Errorf("Skill %q: server returned no download URL", code))
			continue
		}
		content, err := installSkillFromURL(ctx, skillsRoot, code, download)
		if err != nil {
			logs.WarnContextf(ctx, "skip invoked Skill %q: %v", code, err)
			installErrors = append(installErrors, fmt.Errorf("Skill %q: %w", code, err))
			continue
		}
		installed[code] = skillInstallRecord{SHA256: download.SHA256, Revision: download.Revision}
		if err := replaceRunSkillLink(content, filepath.Join(viewRoot, code)); err != nil {
			logs.WarnContextf(ctx, "skip invoked Skill %q link: %v", code, err)
			installErrors = append(installErrors, fmt.Errorf("Skill %q link: %w", code, err))
			continue
		}
		changed = true
		installedCodes = append(installedCodes, code)
		logs.InfoContextf(ctx, "installed invoked Skill %q revision=%d", code, download.Revision)
	}
	if changed {
		if err := writeSkillInstallManifest(manifestPath, installed); err != nil {
			logs.WarnContextf(ctx, "write worker Skill install manifest for invoked Skills failed: %v", err)
		} else {
			p.commitInstalledBaseline(ctx, installedCodes)
		}
	}
	return errors.Join(installErrors...)
}

func (p *PluginSkillPreparer) commitInstalledBaseline(ctx context.Context, codes []string) {
	if p == nil || p.baselineCommitter == nil {
		return
	}
	if err := p.baselineCommitter.CommitInstalled(ctx, codes); err != nil {
		logs.WarnContextf(ctx, "commit Worker Skill baseline failed: %v", err)
	}
}

func (p *PluginSkillPreparer) resolveDownloadURLs(
	ctx context.Context,
	codes []string,
	connectorRefs []connectorSkillDownloadRef,
) (map[string]skillDownloadURLResponse, error) {
	if p == nil || strings.TrimSpace(p.serverAddr) == "" {
		return nil, fmt.Errorf("server address is required")
	}
	body, err := json.Marshal(struct {
		SkillCodes      []string                    `json:"skill_codes"`
		ConnectorSkills []connectorSkillDownloadRef `json:"connector_skills,omitempty"`
	}{SkillCodes: codes, ConnectorSkills: connectorRefs})
	if err != nil {
		return nil, fmt.Errorf("encode skill download URL request: %w", err)
	}
	base := strings.TrimRight(p.serverAddr, "/")
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/plugins/skills/download-urls", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create skill download URL request: %w", err)
	}
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request skill download URLs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("request skill download URLs: unexpected status %s", resp.Status)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Skills []skillDownloadURLResponse `json:"skills"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2_000_000)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode skill download URL response: %w", err)
	}
	if payload.Code != 0 {
		return nil, fmt.Errorf("skill download URL request failed with code %d", payload.Code)
	}
	result := make(map[string]skillDownloadURLResponse, len(payload.Data.Skills))
	for _, item := range payload.Data.Skills {
		code, err := organizationSkillName(item.Code)
		sha, shaErr := normalizedSHA256(item.SHA256)
		if err != nil || shaErr != nil || item.Revision <= 0 || strings.TrimSpace(item.DownloadURL) == "" {
			continue
		}
		item.Code, item.SHA256 = code, sha
		result[code] = item
	}
	return result, nil
}

func pluginSnapshotSkill(
	snapshot agentrundomain.PluginSnapshot,
) (string, int, *connectorSkillDownloadRef, bool) {
	if strings.EqualFold(snapshot.Kind, "skill") {
		return snapshot.Code, snapshot.Revision, nil, true
	}
	if !strings.EqualFold(snapshot.Kind, "mcp") {
		return "", 0, nil, false
	}
	definition, err := service.ConnectorFromDefinition(snapshot.Definition)
	if err != nil || definition == nil || definition.Skill == nil {
		return "", 0, nil, false
	}
	return definition.Skill.Code, definition.Skill.Revision, &connectorSkillDownloadRef{
		PluginID: snapshot.PluginID, Revision: snapshot.Revision,
	}, true
}

func installSkillFromURL(ctx context.Context, skillsRoot, code string, download skillDownloadURLResponse) (string, error) {
	artifactSHA, err := normalizedSHA256(download.SHA256)
	if err != nil {
		return "", fmt.Errorf("invalid Skill artifact sha256: %w", err)
	}
	temp, err := os.MkdirTemp(skillsRoot, ".skill-install-*")
	if err != nil {
		return "", fmt.Errorf("create Skill install temp: %w", err)
	}
	defer os.RemoveAll(temp)
	packagePath := filepath.Join(temp, "package.zip")
	if err := downloadArtifact(ctx, download.DownloadURL, packagePath); err != nil {
		return "", err
	}
	bytes, err := os.ReadFile(packagePath)
	if err != nil {
		return "", fmt.Errorf("read downloaded skill package: %w", err)
	}
	if err := os.Remove(packagePath); err != nil {
		return "", fmt.Errorf("remove downloaded skill package: %w", err)
	}
	hash := sha256.Sum256(bytes)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), artifactSHA) {
		return "", fmt.Errorf("downloaded skill package sha256 mismatch")
	}
	if err := skillarchive.Extract(bytes, temp); err != nil {
		return "", fmt.Errorf("validate and extract skill package: %w", err)
	}
	if !hasSkillDocument(temp) {
		return "", fmt.Errorf("downloaded skill package does not contain SKILL.md")
	}
	content := filepath.Join(skillsRoot, code)
	if err := replaceInstalledSkill(temp, content); err != nil {
		return "", err
	}
	return content, nil
}

func downloadArtifact(ctx context.Context, downloadURL, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create skill download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download skill package: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download skill package: unexpected status %s", resp.Status)
	}
	if resp.ContentLength > skillarchive.MaxPackageBytes {
		return fmt.Errorf("skill package exceeds %d byte limit", skillarchive.MaxPackageBytes)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create skill download temp: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, skillarchive.MaxPackageBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write skill package: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close skill package: %w", closeErr)
	}
	if written > skillarchive.MaxPackageBytes {
		return fmt.Errorf("skill package exceeds %d byte limit", skillarchive.MaxPackageBytes)
	}
	return nil
}

func taskSkillViewRoot(workspace WorkspacePreparation) (string, error) {
	base := strings.TrimSpace(workspace.TaskDir)
	if base == "" {
		base = strings.TrimSpace(workspace.WorkDir)
	}
	if base == "" {
		return "", fmt.Errorf("task or work directory is required")
	}
	return filepath.Join(base, "skills"), nil
}

func resetTaskSkillView(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refuse to replace non-symlink %s", path)
		}
		paths = append(paths, path)
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func systemSkillsDir() string {
	dir, _ := leros.JoinWorkspace(".leros", "skills", ".system")
	return dir
}

func normalizedSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 || !isHex(value) {
		return "", fmt.Errorf("must be a %d-character hexadecimal value", sha256.Size*2)
	}
	return value, nil
}

func organizationSkillName(value string) (string, error) {
	name, err := safeSkillName(value)
	if err != nil {
		return "", err
	}
	if name == "runs" || name == ".system" || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("reserved skill name %q", name)
	}
	return name, nil
}

func hasSkillDocument(root string) bool {
	info, err := os.Stat(filepath.Join(root, "SKILL.md"))
	return err == nil && !info.IsDir()
}

func (p *PluginSkillPreparer) readAndRepairSkillInstallManifest(
	ctx context.Context,
	path string,
) (map[string]skillInstallRecord, error) {
	manifest, err := readSkillInstallManifest(path)
	if err != nil {
		return nil, err
	}
	if len(manifest.Warnings) == 0 {
		return manifest.Records, nil
	}
	logs.WarnContextf(
		ctx,
		"repair worker Skill install manifest: issues=%d refresh=%v warnings=%v",
		len(manifest.Warnings),
		manifest.RefreshCodes,
		manifest.Warnings,
	)
	if err := writeSkillInstallManifest(path, manifest.Records); err != nil {
		logs.WarnContextf(ctx, "repair worker Skill install manifest failed: %v", err)
		return manifest.Records, nil
	}
	p.commitInstalledBaseline(ctx, nil)
	return manifest.Records, nil
}

func readSkillInstallManifest(path string) (*skillInstallManifest, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &skillInstallManifest{Records: make(map[string]skillInstallRecord)}, nil
	}
	if err != nil {
		return nil, err
	}
	manifest := &skillInstallManifest{Records: make(map[string]skillInstallRecord)}
	occurrences := make(map[string]int)
	untrustedCodes := make(map[string]struct{})
	refreshCodes := make(map[string]struct{})
	for lineNumber, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		name, err := organizationSkillName(parts[0])
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("line %d has invalid skill name", lineNumber+1))
			continue
		}
		occurrences[name]++
		if len(parts) != 2 && len(parts) != 3 {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("line %d has invalid field count", lineNumber+1))
			untrustedCodes[name] = struct{}{}
			continue
		}
		hash, err := normalizedSHA256(parts[1])
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("line %d has invalid sha256", lineNumber+1))
			untrustedCodes[name] = struct{}{}
			continue
		}
		if len(parts) == 2 {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("line %d uses legacy format", lineNumber+1))
			untrustedCodes[name] = struct{}{}
			refreshCodes[name] = struct{}{}
			continue
		}
		revision, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil || revision <= 0 {
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("line %d has invalid revision", lineNumber+1))
			untrustedCodes[name] = struct{}{}
			continue
		}
		manifest.Records[name] = skillInstallRecord{SHA256: hash, Revision: revision}
	}
	for name, count := range occurrences {
		if count <= 1 {
			continue
		}
		delete(manifest.Records, name)
		untrustedCodes[name] = struct{}{}
		refreshCodes[name] = struct{}{}
		manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("skill %q has duplicate records", name))
	}
	for name := range untrustedCodes {
		delete(manifest.Records, name)
	}
	manifest.RefreshCodes = make([]string, 0, len(refreshCodes))
	for name := range refreshCodes {
		manifest.RefreshCodes = append(manifest.RefreshCodes, name)
	}
	sort.Strings(manifest.RefreshCodes)
	return manifest, nil
}

func writeSkillInstallManifest(path string, entries map[string]skillInstallRecord) error {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		entry := entries[name]
		fmt.Fprintf(&builder, "%s:%s:%d\n", name, strings.ToLower(entry.SHA256), entry.Revision)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(builder.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func replaceInstalledSkill(temp, destination string) error {
	backup := destination + ".backup"
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove stale skill install backup: %w", err)
	}
	if err := os.Rename(destination, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("back up installed skill: %w", err)
	}
	if err := os.Rename(temp, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil && !os.IsNotExist(restoreErr) {
			return fmt.Errorf("promote installed skill: %w; restore previous skill: %v", err, restoreErr)
		}
		return fmt.Errorf("promote installed skill: %w", err)
	}
	if err := os.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove replaced skill: %w", err)
	}
	return nil
}

func isHex(value string) bool {
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func linkSkillChildren(source, destination string) error {
	entries, err := os.ReadDir(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, err := safeSkillName(entry.Name())
		if err != nil {
			continue
		}
		if err := replaceRunSkillLink(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	return nil
}

func replaceRunSkillLink(source, target string) error {
	info, err := os.Lstat(target)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refuse to replace non-symlink %s", target)
		}
		if err := os.Remove(target); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(source, target)
}

func removeSkillLinks(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(path)
		}
	}
	_ = os.Remove(root)
}

func safeSkillName(value string) (string, error) { return safePathID(value) }

func safePathID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.IsAbs(value) || strings.ContainsAny(value, "/\\") {
		return "", fmt.Errorf("invalid path identifier %q", value)
	}
	return value, nil
}

func sortedPluginSnapshots(values []agentrundomain.PluginSnapshot) []agentrundomain.PluginSnapshot {
	result := append([]agentrundomain.PluginSnapshot(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i].Code < result[j].Code })
	return result
}
