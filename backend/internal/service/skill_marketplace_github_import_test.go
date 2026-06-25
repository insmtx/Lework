package service

import "testing"

func TestParseGitHubSkillImportURL(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSkillID string
		wantVersion string
		wantErr     bool
	}{
		{
			name:        "repository root link",
			input:       "https://github.com/browser-use/video-use",
			wantSkillID: "browser-use/video-use/.",
		},
		{
			name:        "repository root tree link",
			input:       "https://github.com/browser-use/video-use/tree/main",
			wantSkillID: "browser-use/video-use/.",
			wantVersion: "main",
		},
		{
			name:        "repository root blob skill md link",
			input:       "https://github.com/browser-use/video-use/blob/main/SKILL.md",
			wantSkillID: "browser-use/video-use/.",
			wantVersion: "main",
		},
		{
			name:        "repository root raw skill md link",
			input:       "https://raw.githubusercontent.com/browser-use/video-use/main/SKILL.md",
			wantSkillID: "browser-use/video-use/.",
			wantVersion: "main",
		},
		{
			name:        "tree link",
			input:       "https://github.com/openai/skills/tree/main/agents/example",
			wantSkillID: "openai/skills/agents/example",
			wantVersion: "main",
		},
		{
			name:        "blob link to skill md",
			input:       "https://github.com/openai/skills/blob/release-1/agents/example/SKILL.md",
			wantSkillID: "openai/skills/agents/example",
			wantVersion: "release-1",
		},
		{
			name:        "raw skill md link",
			input:       "https://raw.githubusercontent.com/openai/skills/v1.0.0/agents/example/SKILL.md",
			wantSkillID: "openai/skills/agents/example",
			wantVersion: "v1.0.0",
		},
		{
			name:        "owner repo path",
			input:       "openai/skills/agents/example",
			wantSkillID: "openai/skills/agents/example",
		},
		{
			name:        "owner repo path ending with skill md",
			input:       "openai/skills/agents/example/SKILL.md",
			wantSkillID: "openai/skills/agents/example",
		},
		{
			name:    "unsupported host",
			input:   "https://example.com/openai/skills/tree/main/agents/example",
			wantErr: true,
		},
		{
			name:        "owner repo root path",
			input:       "openai/skills",
			wantSkillID: "openai/skills/.",
		},
		{
			name:    "blob not skill md",
			input:   "https://github.com/openai/skills/blob/main/agents/example/README.md",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSkillID, gotVersion, err := parseGitHubSkillImportURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotSkillID != tt.wantSkillID {
				t.Fatalf("skillID = %q, want %q", gotSkillID, tt.wantSkillID)
			}
			if gotVersion != tt.wantVersion {
				t.Fatalf("version = %q, want %q", gotVersion, tt.wantVersion)
			}
		})
	}
}
