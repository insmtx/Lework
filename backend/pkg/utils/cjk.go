package utils

import "unicode"

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
