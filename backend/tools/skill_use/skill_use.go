// Package skilluse provides the runtime tool for loading Leros skill documents.
package skilluse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/tools"
)

const (
	// ToolNameSkillUse is the runtime tool used to discover and load skill documents.
	ToolNameSkillUse = "skill_use"
)

const (
	actionList     = "list"
	actionGet      = "get"
	actionReadFile = "read_file"
)

const (
	defaultSkillFileListLimit = 10
	maxSkillFileReadBytes     = 128 * 1024
)

// ErrSkillNotFound is a structured error returned when a skill cannot be found.
// AvailableSkills is always populated so callers can surface available options.
type ErrSkillNotFound struct {
	Name             string
	AvailableSkills  []string
	ManifestMismatch string
}

func (e *ErrSkillNotFound) Error() string {
	if e.ManifestMismatch != "" {
		return e.ManifestMismatch
	}
	return fmt.Sprintf("Skill %q not found.", e.Name)
}

// SkillUseTool lets an agent query and load skills from the runtime skill catalog.
// It dynamically scans the filesystem on every call — no cached state.
type SkillUseTool struct {
	tools.BaseTool
}

// NewSkillUseTool creates a skill use tool that dynamically scans the skills directory.
func NewSkillUseTool() *SkillUseTool {
	return &SkillUseTool{
		BaseTool: tools.NewBaseTool(
			ToolNameSkillUse,
			strings.Join([]string{
				"管理和使用技能（Skill）。",
				"支持 list 列出所有可用技能，get 获取指定技能完整内容和可注入上下文，read_file 读取技能目录下的附加文件。",
				"当任务需要查看、选择或加载技能说明时调用此工具。",
			}, ""),
			tools.Schema{
				Type:     "object",
				Required: []string{"action"},
				Properties: map[string]*tools.Property{
					"action": {
						Type:        "string",
						Enum:        []string{actionList, actionGet, actionReadFile},
						Description: "操作类型：list 列出技能，get 获取技能正文，read_file 读取技能目录下的文件",
					},
					"name": {
						Type:        "string",
						Description: "技能名称，get 和 read_file 时必填",
					},
					"path": {
						Type:        "string",
						Description: "技能目录内的相对文件路径，read_file 时必填",
					},
				},
			},
		),
	}
}

// Validate checks skill use tool input.
func (t *SkillUseTool) Validate(raw json.RawMessage) error {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return err
	}
	return validateInput(input)
}

func validateInput(input map[string]any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}

	action := stringValue(input, "action")
	switch action {
	case actionList:
		return nil
	case actionGet:
		if stringValue(input, "name") == "" {
			return fmt.Errorf("name is required")
		}
		return nil
	case actionReadFile:
		if stringValue(input, "name") == "" {
			return fmt.Errorf("name is required")
		}
		if stringValue(input, "path") == "" {
			return fmt.Errorf("path is required")
		}
		return nil
	case "":
		return fmt.Errorf("action is required")
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
}

// Execute performs the requested skill catalog action.
func (t *SkillUseTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	input, err := tools.DecodeInput(raw)
	if err != nil {
		return "", err
	}
	if err := validateInput(input); err != nil {
		return "", err
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	switch stringValue(input, "action") {
	case actionList:
		result, err := listSkills()
		if err != nil {
			return "", fmt.Errorf("list skills: %w", err)
		}
		return tools.JSONString(result)
	case actionGet:
		return t.executeGet(input)
	case actionReadFile:
		return t.executeReadFile(input)
	default:
		return "", fmt.Errorf("unsupported action %q", stringValue(input, "action"))
	}
}

func listSkills() (map[string]interface{}, error) {
	summaries, err := skillcatalog.List()
	if err != nil {
		return nil, err
	}
	skills := make([]map[string]interface{}, 0, len(summaries))
	categorySet := make(map[string]struct{})
	for _, summary := range summaries {
		skills = append(skills, summaryMap(summary))
		if summary.Category != "" {
			categorySet[summary.Category] = struct{}{}
		}
	}

	categories := make([]string, 0, len(categorySet))
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	return map[string]interface{}{
		"success":    true,
		"count":      len(skills),
		"skills":     skills,
		"categories": categories,
		"hint":       "Use skill_use with action=get and name=<skill_name> to see full content, tags, and linked files",
	}, nil
}

// executeGet looks up the skill by name via the catalog and returns the full entry.
func (t *SkillUseTool) executeGet(input map[string]interface{}) (string, error) {
	name := stringValue(input, "name")

	entry, err := skillcatalog.Get(name)
	if err != nil {
		return tools.JSONString(buildSkillNotFoundResponse(err))
	}

	files, err := skillcatalog.ListFiles(entry.Manifest.Name, defaultSkillFileListLimit)
	if err != nil {
		return tools.JSONString(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
	}

	skillDir := displaySkillDir(entry)
	return tools.JSONString(map[string]interface{}{
		"success":        true,
		"name":           entry.Manifest.Name,
		"description":    entry.Manifest.Description,
		"version":        entry.Manifest.Version,
		"category":       entry.Manifest.Metadata.Category,
		"tags":           entry.Manifest.Metadata.Tags,
		"content":        entry.Body,
		"path":           entry.Path,
		"skill_dir":      skillDir,
		"linked_files":   optionalFiles(files),
		"usage_hint":     usageHint(files),
		"always":         entry.Manifest.Metadata.Always,
		"requires_tools": entry.Manifest.Metadata.RequiresTools,
	})
}

// executeReadFile looks up the skill by name and reads a supporting file.
func (t *SkillUseTool) executeReadFile(input map[string]interface{}) (string, error) {
	name := stringValue(input, "name")
	relativePath := stringValue(input, "path")

	content, err := skillcatalog.ReadFile(name, relativePath)
	if err != nil {
		return tools.JSONString(buildSkillNotFoundResponse(err))
	}

	displayContent, truncated := truncateFileContent(content, maxSkillFileReadBytes)
	return tools.JSONString(map[string]interface{}{
		"success":   true,
		"name":      name,
		"path":      relativePath,
		"content":   displayContent,
		"size":      len(content),
		"truncated": truncated,
	})
}

// buildSkillNotFoundResponse builds the structured JSON response for
// skill-not-found errors.
func buildSkillNotFoundResponse(err error) map[string]interface{} {
	if err == nil {
		return map[string]interface{}{
			"success":          false,
			"error":            "unknown error",
			"available_skills": []string{},
			"hint":             "Use skill_use with action=list to see all available skills",
		}
	}

	// Extract structured error details using errors.As.
	var errMsg string
	var notFoundErr *skillcatalog.ErrSkillNotFound
	var mismatchErr *skillcatalog.ErrSkillManifestMismatch
	switch {
	case errors.As(err, &mismatchErr):
		if mismatchErr.ManifestName != "" {
			errMsg = fmt.Sprintf("Skill '%s' found at path %s but its manifest name is '%s'.",
				mismatchErr.RequestedName, mismatchErr.Path, mismatchErr.ManifestName)
		} else {
			errMsg = fmt.Sprintf("Skill '%s' found at path %s but its manifest name does not match.",
				mismatchErr.RequestedName, mismatchErr.Path)
		}
	case errors.As(err, &notFoundErr):
		errMsg = fmt.Sprintf("Skill '%s' not found.", notFoundErr.Name)
	default:
		errMsg = err.Error()
	}

	// Build available skill names from a fresh scan.
	available := []string{}
	limit := 20
	if summaries, listErr := skillcatalog.List(); listErr == nil {
		for _, s := range summaries {
			available = append(available, s.Name)
			if len(available) >= limit {
				break
			}
		}
	}

	return map[string]interface{}{
		"success":          false,
		"error":            errMsg,
		"available_skills": available,
		"hint":             "Use skill_use with action=list to see all available skills",
	}
}

func summaryMap(summary skillcatalog.Summary) map[string]interface{} {
	return map[string]interface{}{
		"name":           summary.Name,
		"description":    summary.Description,
		"version":        summary.Version,
		"category":       summary.Category,
		"tags":           summary.Tags,
		"always":         summary.Always,
		"requires_tools": summary.RequiresTools,
		"source":         summary.Source,
		"trust":          summary.Trust,
	}
}

func optionalFiles(files []string) interface{} {
	if len(files) == 0 {
		return nil
	}
	return files
}

func usageHint(files []string) interface{} {
	if len(files) == 0 {
		return nil
	}
	return "To view linked files, call skill_use with action=read_file and path set to a linked file path."
}

func displaySkillDir(entry *skillcatalog.Entry) string {
	if entry == nil {
		return ""
	}
	if entry.AbsoluteDir != "" {
		return entry.AbsoluteDir
	}
	return entry.Dir
}

func truncateFileContent(content []byte, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return string(content), false
	}

	truncated := content[:maxBytes]
	for len(truncated) > 0 && !utf8.Valid(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	return string(truncated), true
}

func stringValue(input map[string]interface{}, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}
