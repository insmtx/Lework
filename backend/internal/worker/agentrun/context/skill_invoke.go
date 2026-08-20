package agentruncontext

import (
	"context"
	"fmt"
	"strings"

	skilltoken "github.com/insmtx/Leros/backend/internal/skill"
	skillcatalog "github.com/insmtx/Leros/backend/internal/skill/catalog"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

// ApplyInvokedSkills loads SKILL.md for codes on user messages and rewrites
// those messages into a prompt that includes the skill bodies. Chip HTML is
// replaced with catalog codes in the user-instruction section (never Chinese
// labels, never raw HTML).
func ApplyInvokedSkills(ctx context.Context, req *agentrundomain.RunRequest) error {
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

		tokens := skilltoken.ParseTokensOnly(msg.Content)
		if len(tokens) == 0 {
			msg.Content = skilltoken.PlainText(msg.Content)
			continue
		}
		plain := skilltoken.PlainText(msg.Content)
		anyMatched = true
		logs.InfoContextf(ctx, "Skill invoke codes: msg_index=%d codes=%v content_len=%d",
			i, tokens, len(msg.Content))

		dedupedTokens := dedupeOrderedLower(tokens)
		if len(dedupedTokens) < len(tokens) {
			logs.DebugContextf(ctx, "Skill invoke intra-message dedup: msg_index=%d before=%d after=%d",
				i, len(tokens), len(dedupedTokens))
		}

		newTokens := make([]string, 0, len(dedupedTokens))
		skippedDedup := make([]string, 0)
		for _, name := range dedupedTokens {
			if req.IsPluginDisabled(types.DisabledPluginKindSkill, name) {
				logs.InfoContextf(ctx, "disabled Skill invocation kept as plain text: msg_index=%d skill=%q", i, name)
				continue
			}
			if !seenSkills[strings.ToLower(name)] {
				newTokens = append(newTokens, name)
			} else {
				skippedDedup = append(skippedDedup, name)
			}
		}
		if len(skippedDedup) > 0 {
			logs.DebugContextf(ctx, "Skill invoke cross-message dedup: msg_index=%d skipped=%v", i, skippedDedup)
		}
		if len(newTokens) == 0 {
			msg.Content = plain
			continue
		}

		catalog, err := catalogForRequest(req)
		if err != nil {
			return fmt.Errorf("resolve run skill catalog: %w", err)
		}
		entries := make([]*skillcatalog.Entry, 0, len(newTokens))
		for _, name := range newTokens {
			entry, err := catalog.Get(name)
			if err != nil {
				logs.WarnContextf(ctx, "Skill invoke load failed: msg_index=%d skill=%q error=%v", i, name, err)
				return err
			}
			entries = append(entries, entry)
			logs.InfoContextf(ctx, "Skill invoke loaded: msg_index=%d skill=%q body_len=%d dir=%s",
				i, entry.Manifest.Name, len(entry.Body), entry.AbsoluteDir)
			seenSkills[strings.ToLower(entry.Manifest.Name)] = true
		}
		if len(entries) == 0 {
			logs.InfoContextf(ctx, "Skill invoke duplicate codes ignored: msg_index=%d content_len=%d",
				i, len(msg.Content))
			msg.Content = plain
			continue
		}

		filesMap := make(map[string][]string, len(entries))
		for _, entry := range entries {
			files, err := catalog.ListFiles(entry.Manifest.Name, 0)
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
		msg.Content = buildSkillInvokePrompt(loadedNames, entries, filesMap, plain)
		logs.InfoContextf(ctx, "Skill invoke message rewritten: msg_index=%d loaded=%v new_prompt_len=%d",
			i, loadedNames, len(msg.Content))
	}

	if !anyMatched {
		return nil
	}

	logs.InfoContextf(ctx, "Applied invoked skills: loaded=%d", len(seenSkills))
	return nil
}

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
