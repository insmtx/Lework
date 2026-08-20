package skillsync

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/insmtx/Leros/backend/internal/cli"
	skillstore "github.com/insmtx/Leros/backend/internal/skill/store"
	"github.com/insmtx/Leros/backend/internal/worker/skillstate"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

func TestRepositoryDetectsOrganizationSkillChangesOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "updated", "old")
	writeTestSkill(t, root, "deleted", "old")
	writeTestSkill(t, root, ".system/builtin", "system")
	writeTestSkill(t, root, "runs/run-1/runtime", "runtime")

	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}

	writeTestSkill(t, root, "updated", "new")
	writeTestSkill(t, root, "created", "new")
	if err := os.RemoveAll(filepath.Join(root, "deleted")); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, ".system/changed", "ignored")
	writeTestSkill(t, root, "runs/run-2/runtime", "ignored")
	writeTestSkill(t, root, ".skill-install-pending/staged", "ignored")
	writeTestSkill(t, root, ".skill-backup-updated-pending/staged", "ignored")

	changes, deleted, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	if changes[0] != (Change{Code: "created", Type: messaging.SkillChangeCreated}) ||
		changes[1] != (Change{Code: "updated", Type: messaging.SkillChangeUpdated}) {
		t.Fatalf("changes = %#v", changes)
	}
	if !slices.Equal(deleted, []string{"deleted"}) {
		t.Fatalf("deleted = %#v", deleted)
	}
}

func TestSkillRepositoryLockIsSharedByEquivalentRoots(t *testing.T) {
	root := t.TempDir()
	repository, err := NewRepository(filepath.Join(root, "nested", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if repository.repositoryLock() != SkillRepositoryLock(root) {
		t.Fatal("equivalent Skill roots did not share a lock")
	}
}

func TestRepositoryImportsTaskSkillDirs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	writeTestSkill(t, taskRoot, "created", "from task")
	if err := os.WriteFile(filepath.Join(taskRoot, "not-a-directory"), []byte("temporary"), 0o644); err != nil {
		t.Fatal(err)
	}

	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	repository.ImportTaskSkillDirs(ctx, taskRoot)
	if _, err := os.Stat(filepath.Join(root, "created", "SKILL.md")); err != nil {
		t.Fatalf("task Skill was not imported: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "created")); !os.IsNotExist(err) {
		t.Fatalf("task Skill source remained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "not-a-directory")); err != nil {
		t.Fatalf("task file should remain for view cleanup: %v", err)
	}
	if err := repository.Restore(ctx, "created"); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "created")); !os.IsNotExist(err) {
		t.Fatalf("untracked imported Skill remained after Restore: %v", err)
	}
}

func TestRepositoryRestoreAllRestoresTrackedAndCleansUntracked(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "tracked", "baseline")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, "tracked", "changed")
	writeTestSkill(t, root, ".system/anysearch", "system cache")
	writeTestSkill(t, root, "untracked", "temporary")
	if err := repository.RestoreAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readTestSkillBody(t, root, "tracked"); got != "baseline" {
		t.Fatalf("tracked Skill body = %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "untracked")); !os.IsNotExist(err) {
		t.Fatalf("untracked Skill remained: %v", err)
	}
	if got := readTestSkillBody(t, root, ".system/anysearch"); got != "system cache" {
		t.Fatalf("system Skill cache body = %q, want preserved content", got)
	}
}

func TestRepositoryReplacesExistingTaskSkill(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	writeTestSkill(t, root, "demo", "baseline")
	writeTestSkill(t, taskRoot, "demo", "from task")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	repository.ImportTaskSkillDirs(ctx, taskRoot)
	if got := readTestSkillBody(t, root, "demo"); got != "from task" {
		t.Fatalf("replaced Skill body = %q", got)
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "demo")); !os.IsNotExist(err) {
		t.Fatalf("task Skill source remained: %v", err)
	}
}

func TestRepositorySkipsReservedTaskEntries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	for _, name := range []string{".git", ".system", "runs"} {
		if err := os.MkdirAll(filepath.Join(taskRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(taskRoot, name, "marker"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(taskRoot, ".seed-manifest"), []byte("unexpected"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, taskRoot, "valid", "from task")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	repository.ImportTaskSkillDirs(ctx, taskRoot)
	if got := readTestSkillBody(t, root, "valid"); got != "from task" {
		t.Fatalf("valid Skill body = %q", got)
	}
	for _, name := range []string{".system", "runs", ".seed-manifest"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("reserved Worker Skill path was modified: %s err=%v", name, err)
		}
		if _, err := os.Lstat(filepath.Join(taskRoot, name)); err != nil {
			t.Fatalf("reserved task entry should remain for cleanup: %s err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("Worker Skill Git repository disappeared: %v", err)
	}
}

func TestRepositoryRecoversInterruptedTaskSkillMoveBeforeImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	writeTestSkill(t, root, "demo", "baseline")
	writeTestSkill(t, taskRoot, "demo", "from task")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	txn, err := os.MkdirTemp(root, taskSkillTransactionPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txn, "target"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "demo"), filepath.Join(txn, "backup")); err != nil {
		t.Fatal(err)
	}

	repository.ImportTaskSkillDirs(ctx, taskRoot)
	if got := readTestSkillBody(t, root, "demo"); got != "from task" {
		t.Fatalf("recovered replacement Skill body = %q", got)
	}
	if _, err := os.Stat(txn); !os.IsNotExist(err) {
		t.Fatalf("recovery transaction remained: %v", err)
	}
}

func TestRepositoryRecoversInterruptedTaskSkillMoveAfterImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	writeTestSkill(t, root, "demo", "baseline")
	writeTestSkill(t, taskRoot, "demo", "from task")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	txn, err := os.MkdirTemp(root, taskSkillTransactionPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(txn, "target"), []byte("demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "demo"), filepath.Join(txn, "backup")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(taskRoot, "demo"), filepath.Join(root, "demo")); err != nil {
		t.Fatal(err)
	}

	repository.ImportTaskSkillDirs(ctx, taskRoot)
	if got := readTestSkillBody(t, root, "demo"); got != "from task" {
		t.Fatalf("kept imported Skill body = %q", got)
	}
	if _, err := os.Stat(txn); !os.IsNotExist(err) {
		t.Fatalf("recovery transaction remained: %v", err)
	}
}

func TestRepositoryDetectsSkillStoreCreateAndUpdate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := skillstore.NewSkillStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, skillstore.CreateRequest{
		Name:    "managed",
		Content: testSkillDocument("managed", "created"),
	}); err != nil {
		t.Fatal(err)
	}
	changes, deleted, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changes, []Change{{Code: "managed", Type: messaging.SkillChangeCreated}}) || len(deleted) != 0 {
		t.Fatalf("created changes=%#v deleted=%#v", changes, deleted)
	}
	if err := os.WriteFile(filepath.Join(root, ".seed-manifest"), []byte("managed:sha:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitInstalled(ctx, []string{"managed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Edit(ctx, skillstore.EditRequest{
		Name:    "managed",
		Content: testSkillDocument("managed", "updated"),
	}); err != nil {
		t.Fatal(err)
	}
	changes, deleted, err = repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changes, []Change{{Code: "managed", Type: messaging.SkillChangeUpdated}}) || len(deleted) != 0 {
		t.Fatalf("updated changes=%#v deleted=%#v", changes, deleted)
	}
}

func TestCommitInstalledMakesServerSkillBaseline(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, "installed", "server content")
	if err := os.WriteFile(filepath.Join(root, ".seed-manifest"), []byte(`{"installed":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitInstalled(ctx, []string{"installed"}); err != nil {
		t.Fatal(err)
	}
	changes, deleted, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || len(deleted) != 0 {
		t.Fatalf("server install remained dirty: changes=%#v deleted=%#v", changes, deleted)
	}
}

func TestCommitInstalledCanCommitManifestOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".seed-manifest"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repository.CommitInstalled(ctx, nil); err != nil {
		t.Fatal(err)
	}
	status, err := repository.git(ctx, "status", "--porcelain=v1", "--", ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 0 {
		t.Fatalf("manifest-only baseline left repository dirty: %q", status)
	}
}

func TestProcessorRestoresAfterPublishFailure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "demo", "baseline")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, "demo", "updated")

	publisher := &testSkillPublisher{err: errors.New("JetStream unavailable")}
	processor := testProcessor(repository, publisher)
	run := RunContext{RunID: "run-1", ProjectID: 3, ActorUIN: 9, PublishChanges: true}
	if err := processor.Process(ctx, run); err != nil {
		t.Fatalf("best-effort processor returned fatal error: %v", err)
	}
	changes, _, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("publish failure should restore the repository, got %#v", changes)
	}
	if got := readTestSkillBody(t, root, "demo"); got != "baseline" {
		t.Fatalf("publish failure should restore Skill body, got %q", got)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("failed publish should not record an event, got %d", len(publisher.events))
	}

	publisher.err = nil
	writeTestSkill(t, root, "demo", "updated again")
	if err := processor.Process(ctx, run); err != nil {
		t.Fatal(err)
	}
	changes, _, err = repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("confirmed publish should restore target, got %#v", changes)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d", len(publisher.events))
	}
	event := publisher.events[0]
	if event.SkillCode != "demo" || event.ProjectID != 3 || event.ActorUIN != 9 ||
		event.StorageURI == "" || event.SHA256 == "" || event.FileSize <= 0 {
		t.Fatalf("event = %#v", event)
	}
}

func TestProcessorImportsTaskSkillsBeforeDetectingChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	writeTestSkill(t, taskRoot, "from-task", "created during Run")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	publisher := &testSkillPublisher{}
	if err := testProcessor(repository, publisher).Process(ctx, RunContext{
		RunID:          "run-task-skill",
		PublishChanges: true,
		TaskSkillDir:   taskRoot,
	}); err != nil {
		t.Fatal(err)
	}
	if len(publisher.events) != 1 || publisher.events[0].SkillCode != "from-task" {
		t.Fatalf("published events = %#v", publisher.events)
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "from-task")); !os.IsNotExist(err) {
		t.Fatalf("task Skill source remained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "from-task")); !os.IsNotExist(err) {
		t.Fatalf("published Skill remained after per-Skill Restore: %v", err)
	}
}

func TestProcessorRestoreAllCleansInvalidTaskSkillOnFailedRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	taskRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(taskRoot, "invalid"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskRoot, "invalid", "notes.txt"), []byte("not a Skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := testProcessor(repository, &testSkillPublisher{}).Process(ctx, RunContext{
		RunID:          "run-invalid-failed",
		PublishChanges: false,
		TaskSkillDir:   taskRoot,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "invalid")); !os.IsNotExist(err) {
		t.Fatalf("invalid imported Skill remained after failed Run: %v", err)
	}
	changes, deleted, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 || len(deleted) != 0 {
		t.Fatalf("repository remained dirty after invalid Skill cleanup: changes=%#v deleted=%#v", changes, deleted)
	}
}

func TestProcessorRestoresDeletedBaselineWithoutPublishing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "baseline", "server")
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "baseline")); err != nil {
		t.Fatal(err)
	}

	publisher := &testSkillPublisher{}
	if err := testProcessor(repository, publisher).Process(ctx, RunContext{
		RunID: "run-delete", PublishChanges: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "baseline", "SKILL.md")); err != nil {
		t.Fatalf("baseline Skill was not restored: %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("deletion must not be published, got %d events", len(publisher.events))
	}
}

func TestProcessorRestoresLocalOnlyAndPublishesOtherChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "connector-mail", "connector baseline")
	writeTestSkill(t, root, "shared", "shared baseline")
	if err := skillstate.Write(filepath.Join(root, ".seed-manifest"), map[string]skillstate.InstallRecord{
		"connector-mail": {
			SHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision: 1, SyncPolicy: skillstate.SyncPolicyLocalOnly,
		},
		"shared": {
			SHA256:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Revision: 1, SyncPolicy: skillstate.SyncPolicyPublish,
		},
	}); err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, "connector-mail", "connector changed")
	writeTestSkill(t, root, "shared", "shared changed")

	publisher := &testSkillPublisher{}
	if err := testProcessor(repository, publisher).Process(ctx, RunContext{
		RunID: "run-mixed", PublishChanges: true,
	}); err != nil {
		t.Fatal(err)
	}
	if got := readTestSkillBody(t, root, "connector-mail"); got != "connector baseline" {
		t.Fatalf("local-only Skill body = %q", got)
	}
	if len(publisher.events) != 1 || publisher.events[0].SkillCode != "shared" {
		t.Fatalf("published events = %#v", publisher.events)
	}
}

func TestProcessorUsesCommittedPolicyAndFailedRunFallback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeTestSkill(t, root, "connector-mail", "connector baseline")
	writeTestSkill(t, root, "shared", "shared baseline")
	if err := skillstate.Write(filepath.Join(root, ".seed-manifest"), map[string]skillstate.InstallRecord{
		"connector-mail": {
			SHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision: 1, SyncPolicy: skillstate.SyncPolicyLocalOnly,
		},
	}); err != nil {
		t.Fatal(err)
	}
	repository, err := NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := skillstate.Write(filepath.Join(root, ".seed-manifest"), map[string]skillstate.InstallRecord{
		"connector-mail": {
			SHA256:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Revision: 1, SyncPolicy: skillstate.SyncPolicyPublish,
		},
	}); err != nil {
		t.Fatal(err)
	}
	writeTestSkill(t, root, "connector-mail", "connector changed")
	writeTestSkill(t, root, "shared", "shared changed")

	publisher := &testSkillPublisher{}
	processor := testProcessor(repository, publisher)
	processor.storage = forbiddenSkillStorage{}
	if err := processor.Process(ctx, RunContext{
		RunID: "run-failed", PublishChanges: false,
		LocalOnlySkillCodes: []string{"connector-mail"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := readTestSkillBody(t, root, "connector-mail"); got != "connector baseline" {
		t.Fatalf("local-only Skill body = %q", got)
	}
	changes, _, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("failed Run changes = %#v", changes)
	}
	if got := readTestSkillBody(t, root, "shared"); got != "shared baseline" {
		t.Fatalf("failed Run shared Skill body = %q", got)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("failed Run events = %#v", publisher.events)
	}
}

func TestBuildPackageIsDeterministic(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "demo", "body")
	if err := os.MkdirAll(filepath.Join(root, "demo", "unconventional", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", "guide.md"), []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(root, "demo", "unconventional", "deep", "data.txt"),
		[]byte("data"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	first, err := buildPackage(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildPackage(root, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Skill package is not deterministic")
	}
}

type testSkillStorage struct{}

func (testSkillStorage) Config(context.Context) (*cli.StorageConfig, error) {
	return &cli.StorageConfig{Scheme: "s3", Bucket: "skills"}, nil
}

func (testSkillStorage) PresignUpload(context.Context, string, string) (string, error) {
	return "http://upload.invalid/skill.zip", nil
}

type forbiddenSkillStorage struct{}

func (forbiddenSkillStorage) Config(context.Context) (*cli.StorageConfig, error) {
	return nil, errors.New("storage must not be called")
}

func (forbiddenSkillStorage) PresignUpload(context.Context, string, string) (string, error) {
	return "", errors.New("storage must not be called")
}

type testRoundTripper struct{}

func (testRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPut || request.Header.Get("Content-Type") != skillPackageMimeType {
		return nil, errors.New("unexpected upload request")
	}
	if _, err := io.ReadAll(request.Body); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     make(http.Header),
	}, nil
}

type testSkillPublisher struct {
	err    error
	events []messaging.SkillPackageUploadedEvent
}

func (p *testSkillPublisher) Publish(_ context.Context, _ string, event any) error {
	if p.err != nil {
		return p.err
	}
	uploaded, ok := event.(messaging.SkillPackageUploadedEvent)
	if !ok {
		return errors.New("unexpected event type")
	}
	p.events = append(p.events, uploaded)
	return nil
}

func testProcessor(repository *Repository, publisher Publisher) *Processor {
	return &Processor{
		orgID: 1, workerID: 2, repository: repository, publisher: publisher,
		storage: testSkillStorage{},
		httpClient: &http.Client{
			Transport: testRoundTripper{},
		},
	}
}

func writeTestSkill(t *testing.T, root, code, body string) {
	t.Helper()
	directory := filepath.Join(root, filepath.FromSlash(code))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(directory)
	document := []byte("---\nname: " + name + "\ndescription: test\n---\n\n" + body + "\n")
	if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), document, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestSkillBody(t *testing.T, root, code string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, code, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	parts := bytes.SplitN(raw, []byte("\n\n"), 2)
	if len(parts) != 2 {
		t.Fatalf("invalid Skill document: %q", raw)
	}
	return string(bytes.TrimSpace(parts[1]))
}

func testSkillDocument(name, body string) string {
	return "---\nname: " + name + "\ndescription: test\n---\n\n" + body + "\n"
}
