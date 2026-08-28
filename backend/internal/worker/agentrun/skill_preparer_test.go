package agentrun

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/internal/worker/skillstate"
	"github.com/insmtx/Leros/backend/internal/worker/skillsync"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/insmtx/Leros/backend/types"
)

func TestPluginSkillPreparerLinksSystemSkillsAndCleansRunView(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	systemSkill := filepath.Join(workspaceRoot, ".leros", "skills", ".system", "review")
	if err := os.MkdirAll(systemSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemSkill, "SKILL.md"), []byte("---\nname: review\ndescription: review\n---\nReview.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taskDir := t.TempDir()
	prepared, cleanup, err := NewPluginSkillPreparer("", "").PrepareSkills(context.Background(), &agentrundomain.RunRequest{RunID: "run-1", Input: agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/review the document"}}}}, WorkspacePreparation{TaskDir: taskDir})
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	if prepared != filepath.Join(taskDir, "skills") {
		t.Fatalf("prepared Skill directory = %q", prepared)
	}
	link := filepath.Join(prepared, "review")
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected system Skill symlink, info=%v err=%v", info, err)
	}
	catalog, err := skillcatalog.NewCatalog(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Get("review"); err != nil {
		t.Fatalf("project catalog must resolve symlinked skill: %v", err)
	}
	cleanup()
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove run symlink, err=%v", err)
	}
	if _, err := os.Stat(prepared); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove empty task Skill directory, err=%v", err)
	}
}

func TestPluginSkillPreparerCleanupPreservesSystemSkillForOverlappingRun(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	systemSkillDir := filepath.Join(workspaceRoot, ".leros", "skills", ".system", "review")
	if err := os.MkdirAll(systemSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(systemSkillDir, "SKILL.md"), []byte("---\nname: review\ndescription: review\n---\nReview.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	repository, err := skillsync.NewRepository(filepath.Join(workspaceRoot, ".leros", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	preparer := NewPluginSkillPreparerWithBaseline("", "", repository)

	firstView, firstCleanup, err := preparer.PrepareSkills(
		ctx,
		&agentrundomain.RunRequest{RunID: "run-first"},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondView, secondCleanup, err := preparer.PrepareSkills(
		ctx,
		&agentrundomain.RunRequest{RunID: "run-second"},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	if err != nil {
		firstCleanup()
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(firstView, "review", "SKILL.md")); err != nil {
		firstCleanup()
		secondCleanup()
		t.Fatalf("first run system Skill link is unavailable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(secondView, "review", "SKILL.md")); err != nil {
		firstCleanup()
		secondCleanup()
		t.Fatalf("second run system Skill link is unavailable: %v", err)
	}

	firstCleanup()
	if _, err := os.Stat(filepath.Join(systemSkillDir, "SKILL.md")); err != nil {
		secondCleanup()
		t.Fatalf("first run cleanup removed the system Skill: %v", err)
	}
	catalog, err := skillcatalog.NewCatalog(secondView)
	if err != nil {
		secondCleanup()
		t.Fatal(err)
	}
	if _, err := catalog.Get("review"); err != nil {
		secondCleanup()
		t.Fatalf("second run system Skill became unavailable after first cleanup: %v", err)
	}

	secondCleanup()
	if _, err := os.Stat(filepath.Join(systemSkillDir, "SKILL.md")); err != nil {
		t.Fatalf("second run cleanup removed the system Skill: %v", err)
	}
}

func TestPluginSkillPreparerFiltersDisabledSystemSkill(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	for _, code := range []string{"review", "lework-automation-manager"} {
		systemSkill := filepath.Join(workspaceRoot, ".leros", "skills", ".system", code)
		if err := os.MkdirAll(systemSkill, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(systemSkill, "SKILL.md"), []byte("---\nname: "+code+"\ndescription: test\n---\nTest.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	taskDir := t.TempDir()
	prepared, cleanup, err := NewPluginSkillPreparer("", "").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-disabled-system-skill",
			Policy: agentrundomain.PolicyContext{DisabledPlugins: []types.DisabledPlugin{{
				Kind: types.DisabledPluginKindSkill,
				Code: "lework-automation-manager",
			}}},
			Input: agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{{
				Role: "user", Content: "run the workflow",
			}}},
		},
		WorkspacePreparation{TaskDir: taskDir},
	)
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	defer cleanup()

	if _, err := os.Lstat(filepath.Join(prepared, "review")); err != nil {
		t.Fatalf("expected review Skill link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(prepared, "lework-automation-manager")); !os.IsNotExist(err) {
		t.Fatalf("disabled automation Skill should not be linked, err=%v", err)
	}
}

func TestPluginSkillPreparerUsesWorkDirWithoutProjectTask(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	workDir := t.TempDir()
	prepared, cleanup, err := NewPluginSkillPreparer("", "").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "temporary-run"},
		WorkspacePreparation{WorkDir: workDir},
	)
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	defer cleanup()
	if prepared != filepath.Join(workDir, "skills") {
		t.Fatalf("prepared Skill directory = %q", prepared)
	}
}

func TestPluginSkillPreparerResetsTaskSkillView(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	taskDir := t.TempDir()
	viewRoot := filepath.Join(taskDir, "skills")
	oldSource := t.TempDir()
	if err := os.MkdirAll(viewRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldSource, filepath.Join(viewRoot, "old")); err != nil {
		t.Fatal(err)
	}
	prepared, cleanup, err := NewPluginSkillPreparer("", "").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "replacement-run"},
		WorkspacePreparation{TaskDir: taskDir},
	)
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	defer cleanup()
	if prepared != viewRoot {
		t.Fatalf("prepared Skill directory = %q", prepared)
	}
	if _, err := os.Lstat(filepath.Join(viewRoot, "old")); !os.IsNotExist(err) {
		t.Fatalf("stale task Skill link was not removed: %v", err)
	}

	manual := filepath.Join(viewRoot, "manual")
	if err := os.MkdirAll(manual, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manual, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	prepared, cleanup, err = NewPluginSkillPreparer("", "").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "best-effort-run"},
		WorkspacePreparation{TaskDir: taskDir},
	)
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	cleanup()
	if _, err := os.Stat(manual); !os.IsNotExist(err) {
		t.Fatalf("stale task Skill file was not cleaned: %v", err)
	}
}

func TestPluginSkillPreparerInstallsSkillAtWorkerRootAndRefreshesIt(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	firstPackage := testSkillPackage(t, "xlsx", "first revision")
	secondPackage := testSkillPackage(t, "xlsx", "second revision")
	downloadURLRequests := 0
	packageDownloads := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/plugins/skills/download-urls":
			if got := request.Header.Get("Authorization"); got != "Bearer worker-token" {
				t.Fatalf("download URL authorization = %q", got)
			}
			var payload struct {
				ActorUIN  uint   `json:"actor_uin"`
				ProjectID string `json:"project_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ActorUIN != 31 || payload.ProjectID != "project-xlsx" {
				t.Fatalf("download authorization context = actor_uin:%d project_id:%q", payload.ActorUIN, payload.ProjectID)
			}
			downloadURLRequests++
			packages := [][]byte{firstPackage, secondPackage}
			if downloadURLRequests > len(packages) {
				t.Fatalf("unexpected extra download URL request")
			}
			index := downloadURLRequests - 1
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"skills": []map[string]any{{"code": "xlsx", "revision": index + 1, "sha256": testPackageHash(packages[index]), "download_url": server.URL + "/package/" + strconv.Itoa(index+1)}}}})
		case "/package/1":
			packageDownloads++
			_, _ = writer.Write(firstPackage)
		case "/package/2":
			packageDownloads++
			_, _ = writer.Write(secondPackage)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	baseline := &skillBaselineCommitterStub{}
	preparer := NewPluginSkillPreparerWithBaseline(server.URL, "worker-token", baseline)
	firstRequest := &agentrundomain.RunRequest{
		RunID:        "run-one",
		Workspace:    agentrundomain.WorkspaceContext{ProjectID: "project-xlsx"},
		BusinessKeys: agentrundomain.BusinessKeys{UinPK: 31},
		Plugins:      []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 1, firstPackage)},
	}
	firstView, firstCleanup, err := preparer.PrepareSkills(context.Background(), firstRequest, WorkspacePreparation{TaskDir: t.TempDir()})
	if err != nil {
		t.Fatalf("install first skill: %v", err)
	}
	defer firstCleanup()
	skillRoot := filepath.Join(workspaceRoot, ".leros", "skills", "xlsx")
	if got := readSkillBody(t, skillRoot); got != "first revision" {
		t.Fatalf("first installed skill body = %q", got)
	}
	firstHash := testPackageHash(firstPackage)
	manifestPath := filepath.Join(workspaceRoot, ".leros", "skills", ".seed-manifest")
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != "xlsx:"+firstHash+":1:publish\n" {
		t.Fatalf("manifest = %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".leros", "skills", ".plugins")); !os.IsNotExist(err) {
		t.Fatalf("legacy plugin cache must not be created, err=%v", err)
	}

	_, secondCleanup, err := preparer.PrepareSkills(context.Background(), &agentrundomain.RunRequest{
		RunID:        "run-two",
		Workspace:    agentrundomain.WorkspaceContext{ProjectID: "project-xlsx"},
		BusinessKeys: agentrundomain.BusinessKeys{UinPK: 31},
		Plugins:      firstRequest.Plugins,
		Input:        agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/xlsx reuse project Skill"}}},
	}, WorkspacePreparation{TaskDir: t.TempDir()})
	if err != nil {
		t.Fatalf("reuse installed skill: %v", err)
	}
	defer secondCleanup()
	if downloadURLRequests != 1 || packageDownloads != 1 {
		t.Fatalf("expected matching install to skip requests, urls=%d downloads=%d", downloadURLRequests, packageDownloads)
	}

	_, thirdCleanup, err := preparer.PrepareSkills(context.Background(), &agentrundomain.RunRequest{
		RunID:        "run-three",
		Workspace:    agentrundomain.WorkspaceContext{ProjectID: "project-xlsx"},
		BusinessKeys: agentrundomain.BusinessKeys{UinPK: 31},
		Plugins:      []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 2, secondPackage)},
	}, WorkspacePreparation{TaskDir: t.TempDir()})
	if err != nil {
		t.Fatalf("update installed skill: %v", err)
	}
	defer thirdCleanup()
	if downloadURLRequests != 2 || packageDownloads != 2 {
		t.Fatalf("expected updated revision to download once, urls=%d downloads=%d", downloadURLRequests, packageDownloads)
	}
	if got := readSkillBody(t, skillRoot); got != "second revision" {
		t.Fatalf("updated installed skill body = %q", got)
	}
	if got := readSkillBody(t, filepath.Join(firstView, "xlsx")); got != "second revision" {
		t.Fatalf("existing run link must follow updated skill, got %q", got)
	}
	secondHash := testPackageHash(secondPackage)
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != "xlsx:"+secondHash+":2:publish\n" {
		t.Fatalf("updated manifest = %q, err=%v", got, err)
	}
	if len(baseline.commits) != 2 ||
		len(baseline.commits[0]) != 1 || baseline.commits[0][0] != "xlsx" ||
		len(baseline.commits[1]) != 1 || baseline.commits[1][0] != "xlsx" {
		t.Fatalf("installed Skill baseline commits = %#v", baseline.commits)
	}
	firstCleanup()
	if _, err := os.Stat(skillRoot); err != nil {
		t.Fatalf("task cleanup removed persistent Skill cache: %v", err)
	}
	if _, err := os.Stat(firstView); !os.IsNotExist(err) {
		t.Fatalf("task cleanup kept first task Skill view: %v", err)
	}
}

func TestPluginSkillPreparerInstallsConnectorSkillAsLocalOnlyAndReusesIt(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	packageBytes := testSkillPackage(t, "connector-mail", "mail connector")
	hash := testPackageHash(packageBytes)
	downloadURLRequests := 0
	packageDownloads := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/plugins/skills/download-urls":
			downloadURLRequests++
			var payload struct {
				SkillCodes      []string                    `json:"skill_codes"`
				ConnectorSkills []connectorSkillDownloadRef `json:"connector_skills"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.SkillCodes) != 0 || len(payload.ConnectorSkills) != 1 {
				t.Fatalf("download request = %#v", payload)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"skills": []map[string]any{{
					"code": "connector-mail", "revision": 1, "sha256": hash,
					"download_url": server.URL + "/package",
				}}},
			})
		case "/package":
			packageDownloads++
			_, _ = writer.Write(packageBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	baseline := &skillBaselineCommitterStub{}
	preparer := NewPluginSkillPreparerWithBaseline(server.URL, "", baseline)
	request := &agentrundomain.RunRequest{
		RunID: "run-connector",
		Plugins: []agentrundomain.PluginSnapshot{
			testConnectorSkillSnapshot("connector-mail", 1, packageBytes),
		},
	}
	_, cleanup, err := preparer.PrepareSkills(
		context.Background(), request, WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(workspaceRoot, ".leros", "skills", ".seed-manifest")
	if got, err := os.ReadFile(manifestPath); err != nil ||
		string(got) != "connector-mail:"+hash+":1:local_only\n" {
		t.Fatalf("connector manifest = %q, err=%v", got, err)
	}
	_, secondCleanup, err := preparer.PrepareSkills(
		context.Background(), request, WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer secondCleanup()
	if err != nil {
		t.Fatal(err)
	}
	if downloadURLRequests != 1 || packageDownloads != 1 {
		t.Fatalf("connector cache requests=%d downloads=%d", downloadURLRequests, packageDownloads)
	}
	if codes := ConnectorSkillCodes(request.Plugins); !slices.Equal(codes, []string{"connector-mail"}) {
		t.Fatalf("connector Skill codes = %#v", codes)
	}
}

func TestPluginSkillPreparerReclassifiesLegacyConnectorWithoutDownload(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	packageBytes := testSkillPackage(t, "connector-mail", "mail connector")
	hash := testPackageHash(packageBytes)
	skillsRoot := filepath.Join(workspaceRoot, ".leros", "skills")
	writeSkillPackageDirectory(t, filepath.Join(skillsRoot, "connector-mail"), packageBytes)
	manifestPath := filepath.Join(skillsRoot, ".seed-manifest")
	if err := os.WriteFile(manifestPath, []byte("connector-mail:"+hash+":1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("legacy connector reclassification must not download")
	}))
	defer server.Close()
	baseline := &skillBaselineCommitterStub{}
	_, cleanup, err := NewPluginSkillPreparerWithBaseline(server.URL, "", baseline).PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-legacy-connector",
			Plugins: []agentrundomain.PluginSnapshot{
				testConnectorSkillSnapshot("connector-mail", 1, packageBytes),
			},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(manifestPath); err != nil ||
		string(got) != "connector-mail:"+hash+":1:local_only\n" {
		t.Fatalf("reclassified manifest = %q, err=%v", got, err)
	}
	if len(baseline.commits) != 1 || len(baseline.commits[0]) != 0 {
		t.Fatalf("manifest-only baseline commits = %#v", baseline.commits)
	}
}

func TestPluginSnapshotSkillRejectsSkillWithoutTrustedArtifactIdentity(t *testing.T) {
	validHash := testPackageHash([]byte("valid"))
	tests := []struct {
		name       string
		definition string
	}{
		{
			name:       "invalid sha256",
			definition: `{"schema":"skill/v1","artifact":{"file_upload_id":"file_demo","sha256":"not-a-sha"}}`,
		},
		{
			name:       "github source without artifact",
			definition: `{"schema":"skill/v1","source":{"type":"github","url":"https://github.com/example/skill"}}`,
		},
		{
			name:       "malformed definition",
			definition: `{"schema":"skill/v2","artifact":{"file_upload_id":"file_demo","sha256":"` + validHash + `"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := pluginSnapshotSkill(agentrundomain.PluginSnapshot{
				PluginID: "plugin_demo", Code: "demo", Kind: "skill", Revision: 1,
				Definition: []byte(test.definition),
			})
			if err == nil || descriptor != nil {
				t.Fatalf("pluginSnapshotSkill() = %#v, %v; want rejected snapshot", descriptor, err)
			}
		})
	}
}

func TestPluginSnapshotSkillRequiresArtifactSHAForCacheIdentity(t *testing.T) {
	hash := testPackageHash([]byte("skill"))
	descriptor, err := pluginSnapshotSkill(agentrundomain.PluginSnapshot{
		PluginID: "plugin_demo", Code: "demo", Kind: "skill", Revision: 1,
		Definition: []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_demo","sha256":"` + strings.ToUpper(hash) + `"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if descriptor == nil || descriptor.SHA256 != hash {
		t.Fatalf("descriptor = %#v, want normalized sha256 %q", descriptor, hash)
	}
}

type skillBaselineCommitterStub struct {
	commits  [][]string
	restores []string
	imports  []string
	resets   int
	importFn func(string)
}

func (s *skillBaselineCommitterStub) CommitInstalled(_ context.Context, codes []string) error {
	s.commits = append(s.commits, append([]string(nil), codes...))
	return nil
}

func (s *skillBaselineCommitterStub) Restore(_ context.Context, code string) error {
	s.restores = append(s.restores, code)
	return nil
}

func (s *skillBaselineCommitterStub) ImportTaskSkillDirs(_ context.Context, root string) {
	s.imports = append(s.imports, root)
	if s.importFn != nil {
		s.importFn(root)
	}
}

func (s *skillBaselineCommitterStub) RestoreAll(context.Context) error {
	s.resets++
	return nil
}

func TestPluginSkillPreparerImportsStaleTaskSkillBeforeReset(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	taskDir := t.TempDir()
	viewRoot := filepath.Join(taskDir, "skills")
	stale := filepath.Join(viewRoot, "stale")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	committer := &skillBaselineCommitterStub{
		importFn: func(root string) {
			entries, err := os.ReadDir(root)
			if err != nil {
				return
			}
			for _, entry := range entries {
				info, err := os.Lstat(filepath.Join(root, entry.Name()))
				if err != nil || !info.IsDir() {
					continue
				}
				_ = os.Rename(filepath.Join(root, entry.Name()), filepath.Join(destination, entry.Name()))
			}
		},
	}
	preparer := NewPluginSkillPreparerWithBaseline("", "", committer)
	prepared, cleanup, err := preparer.PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "recover-stale-task-skill"},
		WorkspacePreparation{TaskDir: taskDir},
	)
	if err != nil {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	if prepared != viewRoot {
		t.Fatalf("prepared Skill directory = %q", prepared)
	}
	if _, err := os.Stat(filepath.Join(destination, "stale", "SKILL.md")); err != nil {
		t.Fatalf("stale task Skill was not imported before reset: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale task Skill remained in task view: %v", err)
	}
	cleanup()
	if len(committer.imports) != 2 {
		t.Fatalf("task Skill import calls = %d, want prepare and cleanup", len(committer.imports))
	}
	if committer.resets != 1 {
		t.Fatalf("task Skill repository resets = %d, want cleanup reset", committer.resets)
	}
}

func TestPluginSkillPreparerRepairsLegacyAndInvalidManifestEntries(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	skillsRoot := filepath.Join(workspaceRoot, ".leros", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	packageBytes := testSkillPackage(t, "xlsx", "migrated")
	legacyHash := testPackageHash([]byte("legacy"))
	stableHash := testPackageHash([]byte("stable"))
	duplicateHash := testPackageHash([]byte("duplicate"))
	manifestPath := filepath.Join(skillsRoot, ".seed-manifest")
	manifest := "docx:" + stableHash + ":2\n" +
		"xlsx:" + legacyHash + "\n" +
		"bad-line\n" +
		"broken:not-a-hash:1\n" +
		"duplicate:" + duplicateHash + ":1\n" +
		"duplicate:" + duplicateHash + ":2\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/plugins/skills/download-urls":
			requests++
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"skills": []map[string]any{{
						"code":         "xlsx",
						"revision":     1,
						"sha256":       testPackageHash(packageBytes),
						"download_url": server.URL + "/package",
					}},
				},
			})
		case "/package":
			_, _ = writer.Write(packageBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	baseline := &skillBaselineCommitterStub{}
	prepared, cleanup, err := NewPluginSkillPreparerWithBaseline(server.URL, "", baseline).PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID:   "run-invalid",
			Plugins: []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 1, packageBytes)},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("repair manifest and install Skill: %v", err)
	}
	if requests != 1 {
		t.Fatalf("legacy Skill must be refreshed once, got %d requests", requests)
	}
	if !hasSkillDocument(filepath.Join(skillsRoot, "xlsx")) ||
		!hasSkillDocument(filepath.Join(prepared, "xlsx")) {
		t.Fatal("migrated Skill must be installed and linked into the run view")
	}
	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "docx:" + stableHash + ":2:publish\nxlsx:" + testPackageHash(packageBytes) + ":1:publish\n"
	if string(got) != want {
		t.Fatalf("repaired manifest = %q, want %q", got, want)
	}
	if len(baseline.commits) != 2 ||
		len(baseline.commits[0]) != 0 ||
		len(baseline.commits[1]) != 1 || baseline.commits[1][0] != "xlsx" {
		t.Fatalf("manifest repair and install baseline commits = %#v", baseline.commits)
	}
}

func TestPluginSkillPreparerSkipsUnresolvedSkillWithoutFailingRun(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/plugins/skills/download-urls" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"skills": []any{}}})
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "run-unresolved", Plugins: []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 1, testSkillPackage(t, "xlsx", "unused"))}},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("unresolved Skill must not fail run preparation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(prepared, "xlsx")); !os.IsNotExist(err) {
		t.Fatalf("unresolved Skill must not create run link, err=%v", err)
	}
}

func TestPluginSkillPreparerFetchesMissingInvokedSkill(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	packageBytes := testSkillPackage(t, "docx", "invoked skill")
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/plugins/skills/download-urls":
			requests++
			var body struct {
				SkillCodes []string `json:"skill_codes"`
				ActorUIN   uint     `json:"actor_uin"`
				ProjectID  string   `json:"project_id"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.SkillCodes) != 1 || body.SkillCodes[0] != "docx" {
				t.Fatalf("requested Skill codes = %#v", body.SkillCodes)
			}
			if body.ActorUIN != 42 || body.ProjectID != "project-invoked" {
				t.Fatalf("download authorization context = actor_uin:%d project_id:%q", body.ActorUIN, body.ProjectID)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"skills": []map[string]any{{"code": "docx", "revision": 3, "sha256": testPackageHash(packageBytes), "download_url": server.URL + "/package"}}}})
		case "/package":
			_, _ = writer.Write(packageBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID:        "run-invoked",
			Workspace:    agentrundomain.WorkspaceContext{ProjectID: "project-invoked"},
			BusinessKeys: agentrundomain.BusinessKeys{UinPK: 42},
			Input: agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{
				{Role: "user", Content: `<skill-chip data-code="docx">docx</skill-chip> create a report`},
			}},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("prepare invoked Skill: %v", err)
	}
	if requests != 1 {
		t.Fatalf("download URL request count = %d, want 1", requests)
	}
	link := filepath.Join(prepared, "docx")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("invoked Skill link = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".leros", "skills", "docx", "SKILL.md")); err != nil {
		t.Fatalf("installed invoked Skill missing: %v", err)
	}
}

func TestPluginSkillPreparerRetriesInvokedProjectSkillAfterProjectPreparationFails(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	packageBytes := testSkillPackage(t, "car-selection", "choose a car")
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/plugins/skills/download-urls":
			requests++
			skills := []map[string]any{}
			if requests == 2 {
				skills = append(skills, map[string]any{
					"code":         "car-selection",
					"revision":     1,
					"sha256":       testPackageHash(packageBytes),
					"download_url": server.URL + "/package",
				})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"skills": skills},
			})
		case "/package":
			_, _ = writer.Write(packageBytes)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-project-fallback",
			Plugins: []agentrundomain.PluginSnapshot{
				testPluginSkillSnapshot("car-selection", 1, packageBytes),
			},
			Input: agentrundomain.InputContext{
				Messages: []agentrundomain.InputMessage{{
					Role: "user", Content: `<skill-chip data-code="car-selection">car-selection</skill-chip> choose a family car`,
				}},
			},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("fallback invoked project Skill: %v", err)
	}
	if requests != 2 {
		t.Fatalf("download URL requests = %d, want project attempt plus invoked fallback", requests)
	}
	catalog, err := skillcatalog.NewCatalog(prepared)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Get("car-selection"); err != nil {
		t.Fatalf("fallback Skill must be available in run view: %v", err)
	}
}

func TestPluginSkillPreparerSkipsUnavailableInvokedSkillWithoutFailingRun(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/plugins/skills/download-urls" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"skills": []any{}},
		})
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-missing-invoked",
			Input: agentrundomain.InputContext{
				Messages: []agentrundomain.InputMessage{{Role: "user", Content: `<skill-chip data-code="missing">missing</skill-chip> do work`}},
			},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("unavailable invoked Skill must not fail run preparation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(prepared, "missing")); !os.IsNotExist(err) {
		t.Fatalf("unavailable invoked Skill must not create run link, err=%v", err)
	}
}

func TestPluginSkillPreparerSkipsUnavailableSceneSkillWithoutFailingRun(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/plugins/skills/download-urls" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"skills": []any{}},
		})
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID:        "run-unavailable-scene",
			BusinessKeys: agentrundomain.BusinessKeys{UinPK: 42},
			Input:        agentrundomain.InputContext{Scene: salaryAccountingScene},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("unavailable scene Skill must not fail run preparation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(prepared, "attendance-payroll")); !os.IsNotExist(err) {
		t.Fatalf("unavailable scene Skill must not create run link, err=%v", err)
	}
}

func TestPluginSkillPreparerSkipsDownloadRequestFailureWithoutFailingRun(t *testing.T) {
	workspaceRoot := t.TempDir()
	t.Setenv(leros.EnvWorkspaceRoot, workspaceRoot)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/plugins/skills/download-urls" {
			http.Error(writer, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	prepared, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-download-failure",
			Input: agentrundomain.InputContext{
				Messages: []agentrundomain.InputMessage{{Role: "user", Content: `<skill-chip data-code="query-time">query-time</skill-chip> query`}},
			},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("download request failure must not fail run preparation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(prepared, "query-time")); !os.IsNotExist(err) {
		t.Fatalf("download request failure must not create run link, err=%v", err)
	}
}

func TestSkillInstallManifestIsTolerantAndSorted(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), ".seed-manifest")
	hashA := testPackageHash([]byte("a"))
	hashB := testPackageHash([]byte("b"))
	entries := map[string]skillInstallRecord{
		"xlsx": {SHA256: hashB, Revision: 2, SyncPolicy: skillstate.SyncPolicyLocalOnly},
		"docx": {SHA256: hashA, Revision: 1, SyncPolicy: skillstate.SyncPolicyPublish},
	}
	if err := writeSkillInstallManifest(manifestPath, entries); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "docx:" + hashA + ":1:publish\nxlsx:" + hashB + ":2:local_only\n"
	if string(got) != want {
		t.Fatalf("manifest = %q, want %q", got, want)
	}
	missing, err := readSkillInstallManifest(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("missing manifest should be empty: %v", err)
	}
	if len(missing.Records) != 0 || len(missing.Warnings) != 0 {
		t.Fatalf("missing manifest = %#v", missing)
	}

	tolerantPath := filepath.Join(t.TempDir(), ".seed-manifest")
	raw := "docx:" + hashA + ":1\n" +
		"xlsx:" + hashB + "\n" +
		"bad-line\n" +
		"invalid:not-a-hash:1\n" +
		"zero:" + hashA + ":0\n" +
		"duplicate:" + hashA + ":1\n" +
		"duplicate:" + hashB + ":2\n"
	if err := os.WriteFile(tolerantPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := readSkillInstallManifest(tolerantPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Records) != 1 ||
		parsed.Records["docx"] != (skillInstallRecord{
			SHA256: hashA, Revision: 1, SyncPolicy: skillstate.SyncPolicyPublish,
		}) {
		t.Fatalf("trusted manifest records = %#v", parsed.Records)
	}
	if !slices.Equal(parsed.RefreshCodes, []string{"duplicate", "xlsx"}) {
		t.Fatalf("refresh codes = %#v", parsed.RefreshCodes)
	}
	if len(parsed.Warnings) != 5 {
		t.Fatalf("manifest warnings = %#v", parsed.Warnings)
	}
}

func TestRepairSkillInstallManifestWriteFailureKeepsValidRecords(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".seed-manifest")
	hash := testPackageHash([]byte("valid"))
	if err := os.WriteFile(path, []byte("docx:"+hash+":1\nbad-line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	baseline := &skillBaselineCommitterStub{}
	records, err := NewPluginSkillPreparerWithBaseline("", "", baseline).
		readAndRepairSkillInstallManifest(context.Background(), path)
	if err != nil {
		t.Fatalf("repair write failure must not discard valid records: %v", err)
	}
	if len(records) != 1 ||
		records["docx"] != (skillInstallRecord{
			SHA256: hash, Revision: 1, SyncPolicy: skillstate.SyncPolicyPublish,
		}) {
		t.Fatalf("valid records after repair failure = %#v", records)
	}
	if len(baseline.commits) != 0 {
		t.Fatalf("failed repair must not commit baseline: %#v", baseline.commits)
	}
}

func testPluginSkillSnapshot(code string, revision int, packageBytes []byte) agentrundomain.PluginSnapshot {
	return agentrundomain.PluginSnapshot{
		PluginID:   "plugin_" + code,
		Code:       code,
		Kind:       "skill",
		Revision:   revision,
		Definition: []byte(fmt.Sprintf(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_%s","sha256":"%s"}}`, code, testPackageHash(packageBytes))),
	}
}

func testConnectorSkillSnapshot(code string, revision int, packageBytes []byte) agentrundomain.PluginSnapshot {
	return agentrundomain.PluginSnapshot{
		PluginID: "plugin_" + code,
		Code:     "mail",
		Kind:     "mcp",
		Revision: revision,
		Definition: []byte(fmt.Sprintf(
			`{"schema":"connector/v1","channel":"mail","mode":"skill_only",`+
				`"auth":{"type":"none"},"skill":{"code":%q,"revision":%d,`+
				`"artifact":{"file_upload_id":"file_%s","sha256":"%s"}}}`,
			code,
			revision,
			code,
			testPackageHash(packageBytes),
		)),
	}
}

func writeSkillPackageDirectory(t *testing.T, directory string, packageBytes []byte) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(packageBytes), int64(len(packageBytes)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		content, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw := new(bytes.Buffer)
		if _, err := raw.ReadFrom(content); err != nil {
			_ = content.Close()
			t.Fatal(err)
		}
		if err := content.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, file.Name), raw.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func testSkillPackage(t *testing.T, name, body string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(file, "---\nname: %s\ndescription: test\n---\n%s\n", name, body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testPackageHash(value []byte) string {
	hash := sha256.Sum256(value)
	return hex.EncodeToString(hash[:])
}

func readSkillBody(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.Equal(line, []byte("first revision")) || bytes.Equal(line, []byte("second revision")) {
			return string(line)
		}
	}
	return ""
}
