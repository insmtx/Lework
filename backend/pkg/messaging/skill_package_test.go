package messaging

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillPackageUploadedEventIsIndependentFromArtifactPayload(t *testing.T) {
	raw, err := json.Marshal(SkillPackageUploadedEvent{
		EventID: "skill_evt_1", WorkerID: 2, RunID: "run-1",
		SkillCode: "demo", ChangeType: SkillChangeCreated,
		StorageURI: "s3://bucket/demo.zip", SHA256: strings.Repeat("a", 64),
		FileSize: 1, Filename: "demo.zip", MimeType: "application/zip",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, forbidden := range []string{"file_upload_id", "artifact_id", "relative_path", "storage_key"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("event unexpectedly contains %q: %s", forbidden, encoded)
		}
	}
}
