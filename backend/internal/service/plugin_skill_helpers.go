package service

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	skillarchive "github.com/insmtx/Leros/backend/internal/skill/archive"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// validateSkillMDFromBytes parses raw bytes as SKILL.md and validates required fields.
func validateSkillMDFromBytes(raw []byte) error {
	manifest, body, err := skillcatalog.ParseDocument(raw)
	if err != nil {
		return fmt.Errorf("parse SKILL.md: %w", err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("frontmatter must include name")
	}
	if len(manifest.Name) > 64 {
		return fmt.Errorf("skill name exceeds 64 characters")
	}
	if !skillNamePattern.MatchString(manifest.Name) {
		return fmt.Errorf("invalid skill name: use lowercase letters, numbers, hyphens, dots, underscores; start with letter or digit")
	}
	if strings.TrimSpace(manifest.Description) == "" {
		return fmt.Errorf("frontmatter must include description")
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("SKILL.md must have content after frontmatter")
	}
	return nil
}

// validateZipSkill validates a zip archive for security and SKILL.md correctness.
func validateZipSkill(zipBytes []byte) error {
	return skillarchive.Validate(zipBytes)
}

func parseGitHubSkillImportURL(raw string) (skillID string, version string, err error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return "", "", fmt.Errorf("github_url is required")
	}

	if !strings.Contains(input, "://") {
		parts := splitCleanPath(input)
		if len(parts) < 2 {
			return "", "", fmt.Errorf("invalid GitHub skill path %q: expected owner/repo/path", raw)
		}
		if len(parts) == 2 {
			skillID, err := normalizeGitHubSkillIdentifier(parts[0], parts[1], ".")
			return skillID, "", err
		}
		skillID, err := normalizeGitHubSkillIdentifier(parts[0], parts[1], strings.Join(parts[2:], "/"))
		return skillID, "", err
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return "", "", fmt.Errorf("invalid GitHub URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	parts := splitCleanPath(parsed.Path)

	switch host {
	case "github.com", "www.github.com":
		return parseGitHubWebPath(parts, raw)
	case "raw.githubusercontent.com":
		return parseGitHubRawPath(parts, raw)
	default:
		return "", "", fmt.Errorf("unsupported GitHub URL host %q", parsed.Hostname())
	}
}

func parseGitHubWebPath(parts []string, raw string) (string, string, error) {
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL %q: expected owner/repo path", raw)
	}
	owner, repo := parts[0], parts[1]
	if len(parts) < 3 {
		skillID, err := normalizeGitHubSkillIdentifier(owner, repo, ".")
		return skillID, "", err
	}

	switch parts[2] {
	case "tree":
		if len(parts) == 4 {
			ref := parts[3]
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, ".")
			return skillID, ref, err
		}
		if len(parts) >= 5 {
			ref := parts[3]
			skillPath := strings.Join(parts[4:], "/")
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
			return skillID, ref, err
		}
	case "blob":
		if len(parts) >= 5 {
			ref := parts[3]
			skillFilePath := strings.Join(parts[4:], "/")
			skillPath, err := skillDirFromSkillMDPath(skillFilePath)
			if err != nil {
				return "", "", err
			}
			skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
			return skillID, ref, err
		}
	}
	return "", "", fmt.Errorf("unsupported GitHub URL %q: use a repository root, a tree link to a skill directory, or a blob link to SKILL.md", raw)
}

func parseGitHubRawPath(parts []string, raw string) (string, string, error) {
	if len(parts) < 4 {
		return "", "", fmt.Errorf("invalid raw GitHub URL %q: expected owner/repo/ref/path/SKILL.md", raw)
	}
	owner, repo, ref := parts[0], parts[1], parts[2]
	skillFilePath := strings.Join(parts[3:], "/")
	skillPath, err := skillDirFromSkillMDPath(skillFilePath)
	if err != nil {
		return "", "", err
	}
	skillID, err := normalizeGitHubSkillIdentifier(owner, repo, skillPath)
	return skillID, ref, err
}

func normalizeGitHubSkillIdentifier(owner, repo, skillPath string) (string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	skillPath = strings.Trim(strings.TrimSpace(skillPath), "/")
	if owner == "" || repo == "" || skillPath == "" {
		return "", fmt.Errorf("invalid GitHub skill identifier: owner, repo, and skill path are required")
	}
	if strings.EqualFold(path.Base(skillPath), "SKILL.md") {
		dir, err := skillDirFromSkillMDPath(skillPath)
		if err != nil {
			return "", err
		}
		skillPath = dir
	}
	return owner + "/" + repo + "/" + skillPath, nil
}

func skillDirFromSkillMDPath(skillFilePath string) (string, error) {
	clean := strings.Trim(path.Clean(strings.TrimSpace(skillFilePath)), "/")
	if clean == "." || clean == "" || !strings.EqualFold(path.Base(clean), "SKILL.md") {
		return "", fmt.Errorf("GitHub blob/raw link must point to SKILL.md")
	}
	dir := path.Dir(clean)
	if dir == "." || dir == "" {
		return ".", nil
	}
	return dir, nil
}

func splitCleanPath(rawPath string) []string {
	cleaned := strings.Trim(rawPath, "/")
	if cleaned == "" {
		return nil
	}
	rawParts := strings.Split(cleaned, "/")
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
