package utils

import (
	"regexp"
	"strings"
	"unicode"
)

// CJKRatio 计算字符串中中文字符（CJK Unified Ideographs）占比，忽略空白字符。
func CJKRatio(s string) float64 {
	if s == "" {
		return 0
	}
	runes := []rune(s)
	total := 0
	cjk := 0
	for _, r := range runes {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			cjk++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(cjk) / float64(total)
}

// 用于过滤 Markdown 代码块的正则
var (
	codeBlockRE  = regexp.MustCompile("(?s)```.+?```")
	inlineCodeRE = regexp.MustCompile("`[^`]+`")
	linkURLRE    = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)
)

// isChinesePunct 判断是否为中文标点。
// 这些标点在中文文本中高频出现，计为中文倾向字符。
func isChinesePunct(r rune) bool {
	switch r {
	case '、', '。', '「', '」', '『', '』', '《', '》',
		'（', '）', '【', '】', '—', '～', '…', '：',
		'；', '！', '？', '，', '．', '・':
		return true
	}
	return false
}

// CJKRatioMarkdown 计算 Markdown 文本中的中文占比。
// 与 CJKRatio 的区别是：先过滤代码块、行内代码、链接 URL 等非自然语言内容，
// 并将中文标点计为中文倾向字符，避免技术文档中代码示例拉低中文比例。
func CJKRatioMarkdown(s string) float64 {
	if s == "" {
		return 0
	}

	// 1. 去掉围栏代码块（```...```）
	cleaned := codeBlockRE.ReplaceAllString(s, "")

	// 2. 去掉行内代码（`...`）
	cleaned = inlineCodeRE.ReplaceAllString(cleaned, "")

	// 3. 去掉链接 URL 部分，保留链接文本 [text](url) → text
	cleaned = linkURLRE.ReplaceAllString(cleaned, "$1")

	// 4. 去掉纯符号行（表格分隔线 |---|---| 等），按行处理
	lines := strings.Split(cleaned, "\n")
	var filteredLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 过滤掉只包含 |、-、:、空格 的行（表格分隔线）
		if isTableSepLine(trimmed) {
			continue
		}
		filteredLines = append(filteredLines, line)
	}
	cleaned = strings.Join(filteredLines, "\n")

	// 5. 对过滤后的文本计算 CJK + 中文标点占比
	runes := []rune(cleaned)
	total := 0
	cjk := 0
	for _, r := range runes {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) || isChinesePunct(r) {
			cjk++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(cjk) / float64(total)
}

// isTableSepLine 判断是否为 Markdown 表格分隔线（如 |---|---| 或 |:---|:---:|）
func isTableSepLine(line string) bool {
	if line == "" {
		return false
	}
	for _, r := range line {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return strings.Contains(line, "-") && strings.Contains(line, "|")
}
