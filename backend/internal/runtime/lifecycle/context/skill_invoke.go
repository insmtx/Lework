package lifecyclecontext

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"
)

// skillInvokeRE matches consecutive /skill tokens at the beginning of a user message.
// Skill names must start with a letter and may contain letters, digits, underscores, and hyphens.
// The token must be followed by whitespace or end-of-line to avoid matching paths like /path/to/file.
var skillInvokeRE = regexp.MustCompile(`^\s*/([A-Za-z][A-Za-z0-9_-]*)(\s|$)`)

// ErrSkillInactive is returned when a skill is not in active status.
var ErrSkillInactive = fmt.Errorf("skill is not active")

// ApplyInvokedSkills parses leading /skill tokens from user messages, loads
// matching SKILL.md content, validates skill status, strips tokens, and
// writes message_resource records.
//
// The same skill is injected only once across all messages. Later messages that
// mention an already loaded skill only have the token stripped.
//
// Each message is rewritten independently using the same prompt format for one
// or many newly loaded skills.
//
// It returns an error when a requested skill is missing, has a manifest
// mismatch, or is not active.
//
// db is optional; when nil, skill status check and message_resource writing are skipped.
func ApplyInvokedSkills(ctx context.Context, db *gorm.DB, req *agent.RequestContext) error {
	if req == nil || len(req.Input.Messages) == 0 {
		return nil
	}

	activeCodes, dbErr := getActiveSkillCodes(ctx, db)
	if dbErr != nil {
		logs.WarnContextf(ctx, "Skill invoke active check unavailable: db error=%v", dbErr)
		activeCodes = nil
	}

	seenSkills := make(map[string]bool)
	anyMatched := false

	for i := range req.Input.Messages {
		msg := &req.Input.Messages[i]

		if msg.Role != "user" {
			continue
		}

		tokens, remaining := parseSkillTokens(msg.Content)
		if len(tokens) == 0 {
			continue
		}
		anyMatched = true
		logs.InfoContextf(ctx, "Skill invoke tokens parsed: msg_index=%d raw_tokens=%v original_len=%d remaining_len=%d",
			i, tokens, len(msg.Content), len(remaining))

		dedupedTokens := dedupeOrderedLower(tokens)
		if len(dedupedTokens) < len(tokens) {
			logs.DebugContextf(ctx, "Skill invoke intra-message dedup: msg_index=%d before=%d after=%d",
				i, len(tokens), len(dedupedTokens))
		}

		newTokens := make([]string, 0, len(dedupedTokens))
		skippedDedup := make([]string, 0)
		for _, name := range dedupedTokens {
			if !seenSkills[strings.ToLower(name)] {
				newTokens = append(newTokens, name)
			} else {
				skippedDedup = append(skippedDedup, name)
			}
		}
		if len(skippedDedup) > 0 {
			logs.DebugContextf(ctx, "Skill invoke cross-message dedup: msg_index=%d skipped=%v", i, skippedDedup)
		}

		entries := make([]*skillcatalog.Entry, 0, len(newTokens))
		for _, name := range newTokens {
			entry, err := skillcatalog.Get(name)
			if err != nil {
				logs.WarnContextf(ctx, "Skill invoke load failed: msg_index=%d skill=%q error=%v", i, name, err)
				return err
			}
			if activeCodes != nil && !activeCodes[entry.Manifest.Name] {
				logs.WarnContextf(ctx, "Skill invoke rejected: skill=%q is not active", entry.Manifest.Name)
				return fmt.Errorf("%w: %s", ErrSkillInactive, entry.Manifest.Name)
			}
			entries = append(entries, entry)
			logs.InfoContextf(ctx, "Skill invoke loaded: msg_index=%d skill=%q body_len=%d dir=%s",
				i, entry.Manifest.Name, len(entry.Body), entry.AbsoluteDir)
			seenSkills[strings.ToLower(entry.Manifest.Name)] = true
		}
		if len(entries) == 0 {
			msg.Content = remaining
			continue
		}

		if db != nil && msg.DBID != 0 && req.Conversation.DBID != 0 {
			records := make([]*types.MessageResource, 0, len(entries))
			for seq, entry := range entries {
				records = append(records, &types.MessageResource{
					MessageID:    msg.DBID,
					SessionID:    req.Conversation.DBID,
					ResourceType: "skill",
					ResourceCode: entry.Manifest.Name,
					ResourceName: entry.Manifest.Name,
					InvokeType:   "slash_command",
					Seq:          seq,
				})
			}
			if err := infradb.BatchCreateMessageResources(ctx, db, records); err != nil {
				logs.WarnContextf(ctx, "Skill invoke write message_resource failed: msg_index=%d error=%v", i, err)
			} else {
				logs.InfoContextf(ctx, "Skill invoke message_resource written: msg_index=%d count=%d", i, len(records))
			}
		}

		filesMap := make(map[string][]string, len(entries))
		for _, entry := range entries {
			files, err := skillcatalog.ListFiles(entry.Manifest.Name, 0)
			if err != nil {
				logs.WarnContextf(ctx, "Skill invoke list files failed: skill=%q error=%v", entry.Manifest.Name, err)
				files = nil
			}
			filesMap[entry.Manifest.Name] = files
			if len(files) > 0 {
				logs.DebugContextf(ctx, "Skill invoke supporting files: skill=%q count=%d files=%v",
					entry.Manifest.Name, len(files), files)
			}
		}

		loadedNames := make([]string, len(entries))
		for j, entry := range entries {
			loadedNames[j] = entry.Manifest.Name
		}
		msg.Content = buildSkillInvokePrompt(loadedNames, entries, filesMap, remaining)
		logs.InfoContextf(ctx, "Skill invoke message rewritten: msg_index=%d loaded=%v new_prompt_len=%d",
			i, loadedNames, len(msg.Content))
	}

	if !anyMatched {
		return nil
	}

	logs.InfoContextf(ctx, "Applied invoked skills: loaded=%d", len(seenSkills))
	return nil
}

func getActiveSkillCodes(ctx context.Context, db *gorm.DB) (map[string]bool, error) {
	if db == nil {
		return nil, nil
	}
	return infradb.GetActiveSkillCodes(ctx, db)
}

// parseSkillTokens parses consecutive /skill tokens from the start of content.
// It returns skill names without the leading slash and the text left after stripping tokens.
func parseSkillTokens(content string) (tokens []string, remaining string) {
	remaining = content
	for {
		m := skillInvokeRE.FindStringSubmatch(remaining)
		if m == nil {
			break
		}
		tokens = append(tokens, m[1])
		remaining = strings.TrimSpace(remaining[len(m[0]):])
	}
	return tokens, remaining
}

// dedupeOrderedLower removes duplicates case-insensitively while preserving first-seen order.
func dedupeOrderedLower(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.ToLower(item)
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}

// buildSkillInvokePrompt builds the prompt used when one or more skills are loaded.
//
// loadedNames contains the manifest names injected into this message. entries
// must correspond to those names. filesMap maps manifest names to supporting
// files, and userContent is the user text after stripping /skill tokens.
func buildSkillInvokePrompt(
	loadedNames []string,
	entries []*skillcatalog.Entry,
	filesMap map[string][]string,
	userContent string,
) string {
	var sb strings.Builder

	sb.WriteString("[IMPORTANT: The user has invoked ")
	fmt.Fprintf(&sb, "%d", len(loadedNames))
	sb.WriteString(" skill(s): ")
	sb.WriteString(strings.Join(loadedNames, ", "))
	sb.WriteString(". Treat every skill below as active guidance for this turn.]")
	sb.WriteString("\n\nUser instruction:\n\n")
	sb.WriteString(userContent)

	for _, entry := range entries {
		if entry == nil {
			continue
		}

		fmt.Fprintf(&sb, "\n\n[Loaded as part of the \"%s\" skill bundle.]\n\n", entry.Manifest.Name)
		sb.WriteString(entry.Body)

		skillDir := entry.AbsoluteDir
		if skillDir == "" {
			skillDir = entry.Dir
		}
		fmt.Fprintf(&sb, "\n\n[Skill directory: %s]\n", skillDir)

		sb.WriteString("\n[This skill has supporting files:]\n")
		files, ok := filesMap[entry.Manifest.Name]
		if !ok || len(files) == 0 {
			sb.WriteString("None\n")
		} else {
			for _, file := range files {
				sb.WriteString(file)
				sb.WriteString("\n")
			}
		}
	}

	return sb.String()
}
