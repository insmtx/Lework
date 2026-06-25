package skill

import (
	"regexp"
	"strings"
)

var skillTokenRE = regexp.MustCompile(`^\s*/([A-Za-z][A-Za-z0-9_-]*)(\s|$)`)

// ParseTokens parses consecutive /skill tokens from the start of content.
// It returns skill names without the leading slash and the text left after stripping tokens.
func ParseTokens(content string) (tokens []string, remaining string) {
	remaining = content
	for {
		m := skillTokenRE.FindStringSubmatch(remaining)
		if m == nil {
			break
		}
		tokens = append(tokens, m[1])
		remaining = strings.TrimSpace(remaining[len(m[0]):])
	}
	return tokens, remaining
}

// ParseTokensOnly is like ParseTokens but returns only the token names.
func ParseTokensOnly(content string) []string {
	tokens, _ := ParseTokens(content)
	return tokens
}
