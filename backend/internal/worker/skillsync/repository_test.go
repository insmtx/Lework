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

func TestProcessorRestoresOnlyAfterPublishConfirmation(t *testing.T) {
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
	run := RunContext{RunID: "run-1", ProjectID: 3, ActorUIN: 9}
	if err := processor.Process(ctx, run); err != nil {
		t.Fatalf("best-effort processor returned fatal error: %v", err)
	}
	changes, _, err := repository.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("publish failure should retain change, got %#v", changes)
	}

	publisher.err = nil
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
	if err := testProcessor(repository, publisher).Process(ctx, RunContext{RunID: "run-delete"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "baseline", "SKILL.md")); err != nil {
		t.Fatalf("baseline Skill was not restored: %v", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("deletion must not be published, got %d events", len(publisher.events))
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

func testSkillDocument(name, body string) string {
	return "---\nname: " + name + "\ndescription: test\n---\n\n" + body + "\n"
}
