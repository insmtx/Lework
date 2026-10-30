package workspace

import "testing"

func TestShouldSkipArtifactFileUsesRepositoryRelativePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"report.md", false},
		{"docs/report.md", false},
		{"tmp/report.md", true},
		{"node_modules/pkg/index.js", true},
		{"build/out.bin", true},
		{"logs/run.log", true},
		{".DS_Store", true},
		{"docs/.DS_Store", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := ShouldSkipArtifactFile(tc.path); got != tc.want {
				t.Fatalf("ShouldSkipArtifactFile(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
