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
	"strings"

	"github.com/insmtx/Leros/backend/internal/service"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/internal/worker/skillstate"
	"github.com/insmtx/Leros/backend/internal/worker/skillsync"
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
	Restore(context.Context, string) error
}

type taskSkillDirectoryImporter interface {
	ImportTaskSkillDirs(context.Context, string)
}

type lockedSkillBaselineCommitter interface {
	CommitInstalledLocked(context.Context, []string) error
	RestoreLocked(context.Context, string) error
}

type taskSkillRepositoryResetter interface {
	RestoreAll(context.Context) error
}

// PluginSkillPreparer installs project Skill bundles into the worker workspace
// and creates only symlinks in the task-private Skill view.
type PluginSkillPreparer struct {
	serverAddr        string
	authToken         string
	httpClient        *http.Client
	baselineCommitter SkillBaselineCommitter
}

type skillInstallRecord = skillstate.InstallRecord
type skillInstallManifest = skillstate.Manifest

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

type pluginSkillDescriptor struct {
	Code         string
	Revision     int
	SHA256       string
	SyncPolicy   skillstate.SyncPolicy
	ConnectorRef *connectorSkillDownloadRef
}

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
	policy := applyDisabledPluginPolicy(ctx, req)
	viewRoot, err := taskSkillViewRoot(workspace)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() {
		importTaskSkillDirs(ctx, p.baselineCommitter, viewRoot)
		resetTaskSkillRepository(ctx, p.baselineCommitter)
		removeTaskSkillView(viewRoot)
	}
	importTaskSkillDirs(ctx, p.baselineCommitter, viewRoot)
	if err := resetTaskSkillView(viewRoot); err != nil {
		logs.WarnContextf(ctx, "prepare task Skill directory failed; continuing without run Skill view: root=%s error=%v", viewRoot, err)
		return "", cleanup, nil
	}
	if err := linkSkillChildren(systemSkillsDir(), viewRoot, policy); err != nil {
		logs.WarnContextf(ctx, "link worker system Skills failed; continuing: root=%s error=%v", viewRoot, err)
	}
	p.preparePluginSkills(ctx, req.Plugins, viewRoot, policy)
	if err := p.prepareInvokedSkills(ctx, req.Input.Messages, viewRoot, policy); err != nil {
		return "", cleanup, fmt.Errorf("prepare invoked skills: %w", err)
	}
	return viewRoot, cleanup, nil
}

func (p *PluginSkillPreparer) preparePluginSkills(ctx context.Context, snapshots []agentrundomain.PluginSnapshot, viewRoot string, policy disabledPluginPolicy) {
	skillsRoot, err := leros.JoinWorkspace(".leros", "skills")
	if err != nil {
		logs.WarnContextf(ctx, "resolve worker Skill directory failed: %v", err)
		return
	}
	installLock := skillsync.SkillRepositoryLock(skillsRoot)
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
	descriptors := make(map[string]pluginSkillDescriptor)
	connectorRefs := make([]connectorSkillDownloadRef, 0)
	baselineCodes := make(map[string]struct{})
	manifestChanged := false
	for _, snapshot := range sortedPluginSnapshots(snapshots) {
		descriptor, err := pluginSnapshotSkill(snapshot)
		if err != nil {
			logs.WarnContextf(
				ctx,
				"skip invalid project Skill snapshot: plugin_id=%s code=%s revision=%d error=%v",
				snapshot.PluginID,
				snapshot.Code,
				snapshot.Revision,
				err,
			)
			continue
		}
		if descriptor == nil {
			continue
		}
		if policy.skillDisabled(descriptor.Code) {
			logs.InfoContextf(ctx, "disabled embedded Skill for run: code=%s plugin_id=%s", descriptor.Code, snapshot.PluginID)
			continue
		}
		code, err := organizationSkillName(descriptor.Code)
		if err != nil || descriptor.Revision <= 0 {
			logs.WarnContextf(ctx, "skip invalid project Skill %q: code=%v revision=%d", snapshot.Code, err, snapshot.Revision)
			continue
		}
		descriptor.Code = code
		descriptors[code] = *descriptor
		record, installedOK := installed[code]
		if descriptor.SyncPolicy == skillstate.SyncPolicyLocalOnly ||
			(installedOK && record.SyncPolicy == skillstate.SyncPolicyLocalOnly) {
			p.restoreInstalledBaseline(ctx, code)
		}
		content := filepath.Join(skillsRoot, code)
		identityMatches := installedOK && record.Revision == descriptor.Revision &&
			strings.EqualFold(record.SHA256, descriptor.SHA256)
		if identityMatches && hasSkillDocument(content) {
			if record.SyncPolicy != descriptor.SyncPolicy {
				record.SyncPolicy = descriptor.SyncPolicy
				installed[code] = record
				manifestChanged = true
			}
			available[code] = content
			continue
		}
		pending[code] = struct{}{}
		if descriptor.ConnectorRef != nil {
			connectorRefs = append(connectorRefs, *descriptor.ConnectorRef)
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
			for _, code := range codes {
				download, ok := downloads[code]
				if !ok {
					logs.WarnContextf(ctx, "skip project Skill %q: server returned no download URL", code)
					continue
				}
				descriptor := descriptors[code]
				if download.Revision != descriptor.Revision ||
					!strings.EqualFold(download.SHA256, descriptor.SHA256) {
					logs.WarnContextf(ctx, "skip project Skill %q: server returned mismatched artifact identity", code)
					continue
				}
				content, err := installSkillFromURL(ctx, skillsRoot, code, download)
				if err != nil {
					logs.WarnContextf(ctx, "skip project Skill %q: %v", code, err)
					continue
				}
				installed[code] = skillInstallRecord{
					SHA256: download.SHA256, Revision: download.Revision, SyncPolicy: descriptor.SyncPolicy,
				}
				available[code] = content
				manifestChanged = true
				baselineCodes[code] = struct{}{}
				logs.InfoContextf(ctx, "installed project Skill %q revision=%d", code, download.Revision)
			}
		}
	}
	if manifestChanged {
		if err := writeSkillInstallManifest(manifestPath, installed); err != nil {
			logs.WarnContextf(ctx, "write worker Skill install manifest failed: %v", err)
		} else {
			codes := make([]string, 0, len(baselineCodes))
			for code := range baselineCodes {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			p.commitInstalledBaseline(ctx, codes)
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
func (p *PluginSkillPreparer) prepareInvokedSkills(ctx context.Context, messages []agentrundomain.InputMessage, viewRoot string, policy disabledPluginPolicy) error {
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
			if policy.skillDisabled(code) {
				logs.InfoContextf(ctx, "skip explicitly invoked disabled Skill: code=%s", code)
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
	installLock := skillsync.SkillRepositoryLock(skillsRoot)
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
		installed[code] = skillInstallRecord{
			SHA256: download.SHA256, Revision: download.Revision, SyncPolicy: skillstate.SyncPolicyPublish,
		}
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
	var err error
	if locked, ok := p.baselineCommitter.(lockedSkillBaselineCommitter); ok {
		err = locked.CommitInstalledLocked(ctx, codes)
	} else {
		err = p.baselineCommitter.CommitInstalled(ctx, codes)
	}
	if err != nil {
		logs.WarnContextf(ctx, "commit Worker Skill baseline failed: %v", err)
	}
}

func (p *PluginSkillPreparer) restoreInstalledBaseline(ctx context.Context, code string) {
	if p == nil || p.baselineCommitter == nil {
		return
	}
	var err error
	if locked, ok := p.baselineCommitter.(lockedSkillBaselineCommitter); ok {
		err = locked.RestoreLocked(ctx, code)
	} else {
		err = p.baselineCommitter.Restore(ctx, code)
	}
	if err != nil {
		logs.WarnContextf(ctx, "restore local-only Worker Skill %q baseline failed: %v", code, err)
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

// pluginSnapshotSkill returns a descriptor only when the snapshot contains an
// immutable, downloadable artifact identity. A revision alone cannot identify
// Skill content and must never authorize cache reuse.
func pluginSnapshotSkill(snapshot agentrundomain.PluginSnapshot) (*pluginSkillDescriptor, error) {
	if strings.EqualFold(snapshot.Kind, "skill") {
		if snapshot.Revision <= 0 {
			return nil, fmt.Errorf("Skill revision must be positive")
		}
		artifact, err := service.ArtifactFromDefinition("skill", snapshot.Definition)
		if err != nil {
			return nil, fmt.Errorf("parse Skill artifact definition: %w", err)
		}
		if artifact == nil {
			return nil, fmt.Errorf("Skill snapshot is not backed by a downloadable artifact")
		}
		sha, err := normalizedSHA256(artifact.SHA256)
		if err != nil {
			return nil, fmt.Errorf("normalize Skill artifact sha256: %w", err)
		}
		return &pluginSkillDescriptor{
			Code: snapshot.Code, Revision: snapshot.Revision, SHA256: sha,
			SyncPolicy: skillstate.SyncPolicyPublish,
		}, nil
	}
	if !strings.EqualFold(snapshot.Kind, "mcp") {
		return nil, nil
	}
	definition, err := service.ConnectorFromDefinition(snapshot.Definition)
	if err != nil {
		return nil, fmt.Errorf("parse Connector definition: %w", err)
	}
	if definition == nil || definition.Skill == nil {
		return nil, nil
	}
	if definition.Skill.Artifact == nil {
		return nil, fmt.Errorf("Connector Skill artifact is required")
	}
	sha, err := normalizedSHA256(definition.Skill.Artifact.SHA256)
	if err != nil {
		return nil, fmt.Errorf("normalize Connector Skill artifact sha256: %w", err)
	}
	return &pluginSkillDescriptor{
		Code: definition.Skill.Code, Revision: definition.Skill.Revision, SHA256: sha,
		SyncPolicy: skillstate.SyncPolicyLocalOnly,
		ConnectorRef: &connectorSkillDownloadRef{
			PluginID: snapshot.PluginID, Revision: snapshot.Revision,
		},
	}, nil
}

// ConnectorSkillCodes returns the immutable local-only Skill codes selected for a Run.
func ConnectorSkillCodes(snapshots []agentrundomain.PluginSnapshot) []string {
	codes := make(map[string]struct{})
	for _, snapshot := range snapshots {
		descriptor, err := pluginSnapshotSkill(snapshot)
		if err != nil || descriptor == nil || descriptor.SyncPolicy != skillstate.SyncPolicyLocalOnly {
			continue
		}
		code, err := organizationSkillName(descriptor.Code)
		if err == nil {
			codes[code] = struct{}{}
		}
	}
	result := make([]string, 0, len(codes))
	for code := range codes {
		result = append(result, code)
	}
	sort.Strings(result)
	return result
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
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
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
	return skillstate.Read(path)
}

func writeSkillInstallManifest(path string, entries map[string]skillInstallRecord) error {
	return skillstate.Write(path, entries)
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

func linkSkillChildren(source, destination string, policy disabledPluginPolicy) error {
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
		if policy.skillDisabled(name) {
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
			logs.Warnf("skip Skill link because target is not a symlink: path=%s", target)
			return nil
		}
		if err := os.Remove(target); err != nil {
			logs.Warnf("remove stale Skill link failed: path=%s error=%v", target, err)
			return nil
		}
	} else if !os.IsNotExist(err) {
		logs.Warnf("inspect Skill link target failed: path=%s error=%v", target, err)
		return nil
	}
	if err := os.Symlink(source, target); err != nil {
		logs.Warnf("create Skill link failed: source=%s target=%s error=%v", source, target, err)
	}
	return nil
}

func removeTaskSkillView(root string) {
	if err := os.RemoveAll(root); err != nil {
		logs.Warnf("remove task Skill directory failed: root=%s error=%v", root, err)
	}
}

func importTaskSkillDirs(ctx context.Context, committer SkillBaselineCommitter, root string) {
	importer, ok := committer.(taskSkillDirectoryImporter)
	if !ok {
		return
	}
	importer.ImportTaskSkillDirs(ctx, root)
}

func resetTaskSkillRepository(ctx context.Context, committer SkillBaselineCommitter) {
	resetter, ok := committer.(taskSkillRepositoryResetter)
	if !ok {
		return
	}
	if err := resetter.RestoreAll(context.WithoutCancel(ctx)); err != nil {
		logs.WarnContextf(ctx, "restore Worker Skill repository during task cleanup failed: %v", err)
	}
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
