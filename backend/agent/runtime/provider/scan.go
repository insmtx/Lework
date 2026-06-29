package provider

import (
	"bufio"
	"io"
)

const maxJSONLineSize = 4 * 1024 * 1024 // 最大单行 JSON 大小 4MB

// ScanJSONLines 扫描换行符分隔的 JSON 日志，支持更大的 token 缓冲区。
func ScanJSONLines(r io.Reader, visit func(line string) bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLineSize)
	for scanner.Scan() {
		if !visit(scanner.Text()) {
			return
		}
	}
}
