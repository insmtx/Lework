package lifecyclecontext

import (
	"context"
	"fmt"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	"github.com/ygpkg/yg-go/logs"
)


// ApplyInvokedSkills parses leading /skill tokens from user messages, loads
// matching SKILL.md content, strips the tokens, and rewrites message content.
//
// The same skill is injected only once across all messages. Later messages that
// mention an already loaded skill only have the token stripped.
//
// Each message is rewritten independently using the same prompt format for one
// or many newly loaded skills.
//
// It returns an error only when a requested skill is missing or has a manifest
// mismatch. Messages with no leading skill token are left unchanged.
func ApplyInvokedSkills(ctx context.Context, req *agent.RequestContext) error {
	if req == nil || len(req.Input.Messages) == 0 {
		return nil
	}

	seenSkills := make(map[string]bool)
	anyMatched := false

	for i := range req.Input.Messages {
		msg := &req.Input.Messages[i]

		if msg.Role != "user" {
			continue
		}

		tokens, remaining := skilltoken.ParseTokens(msg.Content)
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
				return err // ErrSkillNotFound / ErrSkillManifestMismatch
			}
			entries = append(entries, entry)
			logs.InfoContextf(ctx, "Skill invoke loaded: msg_index=%d skill=%q body_len=%d dir=%s",
				i, entry.Manifest.Name, len(entry.Body), entry.AbsoluteDir)
			seenSkills[strings.ToLower(entry.Manifest.Name)] = true
		}
		if len(entries) == 0 {
			msg.Content = remaining
			logs.InfoContextf(ctx, "Skill invoke duplicate tokens stripped: msg_index=%d new_content_len=%d",
				i, len(msg.Content))
			continue
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

// buildSkillInvokePrompt removes duplicates case-insensitively while preserving first-seen order.
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
