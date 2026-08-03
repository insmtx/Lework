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
	"github.com/insmtx/Leros/backend/pkg/leros"
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

func TestPluginSkillPreparerResetsOnlyTaskSkillSymlinks(t *testing.T) {
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
	if err := os.WriteFile(manual, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewPluginSkillPreparer("", "").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{RunID: "blocked-run"},
		WorkspacePreparation{TaskDir: taskDir},
	); err == nil || !strings.Contains(err.Error(), "refuse to replace non-symlink") {
		t.Fatalf("PrepareSkills() error = %v", err)
	}
	if got, err := os.ReadFile(manual); err != nil || string(got) != "keep" {
		t.Fatalf("manual task Skill file changed: %q, %v", got, err)
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
	firstRequest := &agentrundomain.RunRequest{RunID: "run-one", Plugins: []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 1, firstPackage)}}
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
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != "xlsx:"+firstHash+":1\n" {
		t.Fatalf("manifest = %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, ".leros", "skills", ".plugins")); !os.IsNotExist(err) {
		t.Fatalf("legacy plugin cache must not be created, err=%v", err)
	}

	_, secondCleanup, err := preparer.PrepareSkills(context.Background(), &agentrundomain.RunRequest{RunID: "run-two", Plugins: firstRequest.Plugins, Input: agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/xlsx reuse project Skill"}}}}, WorkspacePreparation{TaskDir: t.TempDir()})
	if err != nil {
		t.Fatalf("reuse installed skill: %v", err)
	}
	defer secondCleanup()
	if downloadURLRequests != 1 || packageDownloads != 1 {
		t.Fatalf("expected matching install to skip requests, urls=%d downloads=%d", downloadURLRequests, packageDownloads)
	}

	_, thirdCleanup, err := preparer.PrepareSkills(context.Background(), &agentrundomain.RunRequest{RunID: "run-three", Plugins: []agentrundomain.PluginSnapshot{testPluginSkillSnapshot("xlsx", 2, secondPackage)}}, WorkspacePreparation{TaskDir: t.TempDir()})
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
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != "xlsx:"+secondHash+":2\n" {
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

type skillBaselineCommitterStub struct {
	commits [][]string
}

func (s *skillBaselineCommitterStub) CommitInstalled(_ context.Context, codes []string) error {
	s.commits = append(s.commits, append([]string(nil), codes...))
	return nil
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
	want := "docx:" + stableHash + ":2\nxlsx:" + testPackageHash(packageBytes) + ":1\n"
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
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.SkillCodes) != 1 || body.SkillCodes[0] != "docx" {
				t.Fatalf("requested Skill codes = %#v", body.SkillCodes)
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
		&agentrundomain.RunRequest{RunID: "run-invoked", Input: agentrundomain.InputContext{Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/docx create a report"}}}},
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
					Role: "user", Content: "/car-selection choose a family car",
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

func TestPluginSkillPreparerReturnsInvokedSkillPreparationError(t *testing.T) {
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
	_, cleanup, err := NewPluginSkillPreparer(server.URL, "worker-token").PrepareSkills(
		context.Background(),
		&agentrundomain.RunRequest{
			RunID: "run-missing-invoked",
			Input: agentrundomain.InputContext{
				Messages: []agentrundomain.InputMessage{{Role: "user", Content: "/missing do work"}},
			},
		},
		WorkspacePreparation{TaskDir: t.TempDir()},
	)
	defer cleanup()
	if err == nil || !strings.Contains(err.Error(), `Skill "missing": server returned no download URL`) {
		t.Fatalf("explicit Skill error = %v", err)
	}
}

func TestSkillInstallManifestIsTolerantAndSorted(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), ".seed-manifest")
	hashA := testPackageHash([]byte("a"))
	hashB := testPackageHash([]byte("b"))
	entries := map[string]skillInstallRecord{
		"xlsx": {SHA256: hashB, Revision: 2},
		"docx": {SHA256: hashA, Revision: 1},
	}
	if err := writeSkillInstallManifest(manifestPath, entries); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	got, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "docx:" + hashA + ":1\nxlsx:" + hashB + ":2\n"
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
		parsed.Records["docx"] != (skillInstallRecord{SHA256: hashA, Revision: 1}) {
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
		records["docx"] != (skillInstallRecord{SHA256: hash, Revision: 1}) {
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
