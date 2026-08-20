package skill

import (
	"html"
	"regexp"
	"strings"
)

// Skill chips are stored in message content as:
//
//	<skill-chip data-code="catalog-code">中文展示名</skill-chip>
//
// Catalog ident is data-code; inner text is the user-visible Chinese label.
//
// Contract:
//   - raw content: UI replay + ParseTokensOnly only
//   - PlainText: anything sent to an LLM (chips → catalog code)
//   - DisplayText: user-facing titles/previews (chips → Chinese label)
var skillChipRE = regexp.MustCompile(`(?i)<skill-chip\s+[^>]*\bdata-code="([A-Za-z][A-Za-z0-9_-]*)"[^>]*>([^<]*)</skill-chip>`)

// FormatChip renders a skill mention for persistence and tests.
func FormatChip(code, label string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	text := strings.TrimSpace(label)
	if text == "" {
		text = code
	}
	return `<skill-chip data-code="` + html.EscapeString(code) + `">` + html.EscapeString(text) + `</skill-chip>`
}

// ParseTokens returns catalog codes in document order and the model-facing
// plain text (chips replaced by catalog codes).
func ParseTokens(content string) (tokens []string, remaining string) {
	return ParseTokensOnly(content), PlainText(content)
}

// ParseTokensOnly returns invoked Skill catalog codes scanned from content chips.
func ParseTokensOnly(content string) []string {
	matches := skillChipRE.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	codes := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		code := strings.TrimSpace(match[1])
		if code == "" {
			continue
		}
		key := strings.ToLower(code)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}

// HasInvokedSkills reports whether content includes at least one skill chip.
func HasInvokedSkills(content string) bool {
	return len(ParseTokensOnly(content)) > 0
}

// PlainText replaces skill chips with their catalog codes for LLM / agent input.
func PlainText(content string) string {
	return skillChipRE.ReplaceAllStringFunc(content, func(raw string) string {
		match := skillChipRE.FindStringSubmatch(raw)
		if len(match) < 2 {
			return ""
		}
		return strings.TrimSpace(match[1])
	})
}

// DisplayText replaces skill chips with their Chinese labels for UI titles and previews.
func DisplayText(content string) string {
	return skillChipRE.ReplaceAllStringFunc(content, func(raw string) string {
		match := skillChipRE.FindStringSubmatch(raw)
		if len(match) < 3 {
			return ""
		}
		label := strings.TrimSpace(html.UnescapeString(match[2]))
		if label == "" {
			return strings.TrimSpace(match[1])
		}
		return label
	})
}
