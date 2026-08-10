package seed

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/template"
)

type sqlline struct {
	line   string
	number int
}

// parseSQLStatements 按分号切分 SQL 语句，跳过空行与 -- / # / /* */ 注释，记录起始行号。
// 同行内出现多个以分号分隔的语句也会被正确切分。
// 仅当分号位于字符串字面量之外时才会切分：解析器跟踪单引号（”）与双引号（""，
// 含 SQL 转义约定 ” 与 ""）的进出状态，因此字符串字面量内出现的分号不会误切分。
func parseSQLStatements(r io.Reader) ([]sqlline, error) {
	var statements []sqlline
	var current strings.Builder
	scanner := bufio.NewScanner(r)
	lineNumber := 0
	startNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "/*") && strings.HasSuffix(line, "*/") {
			continue
		}
		if current.Len() == 0 {
			startNumber = lineNumber
		}
		current.WriteString(line)
		current.WriteString(" ")
		splitSQLChunk(&current, &statements, &startNumber, lineNumber)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if cur := current.String(); cur != "" {
		statements = append(statements, sqlline{strings.TrimSpace(cur), startNumber})
	}
	return statements, nil
}

// splitSQLChunk 逐字节扫描已累积的 SQL 文本，在引号之外遇到分号时切分出一条语句。
// 切分后未消费的尾部（可能是不完整语句）留在 current 中，等待下一行继续累积。
func splitSQLChunk(current *strings.Builder, statements *[]sqlline, startNumber *int, lineNumber int) {
	leftover := current.String()
	var stmt strings.Builder
	var quote byte
	for len(leftover) > 0 {
		c := leftover[0]
		if quote != 0 {
			// 字符串内部：
			if c == quote {
				// SQL 转义约定：连续两个同种引号表示转义，否则代表字符串结束。
				if len(leftover) >= 2 && leftover[1] == quote {
					stmt.WriteByte(c)
					stmt.WriteByte(quote)
					leftover = leftover[2:]
					continue
				}
				quote = 0
			}
			stmt.WriteByte(c)
			leftover = leftover[1:]
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			stmt.WriteByte(c)
			leftover = leftover[1:]
		case ';':
			piece := strings.TrimSpace(stmt.String())
			if piece != "" {
				*statements = append(*statements, sqlline{piece, *startNumber})
			}
			*startNumber = lineNumber
			stmt.Reset()
			leftover = leftover[1:]
		default:
			stmt.WriteByte(c)
			leftover = leftover[1:]
		}
	}
	current.Reset()
	current.WriteString(strings.TrimSpace(stmt.String()))
}

// renderSQLTemplate 用 envs 渲染 SQL 模板。缺失 key 时返回错误，满足"必填缺失即报错"。
func renderSQLTemplate(tplText string, envs map[string]string) (string, error) {
	t, err := template.New("sql").Option("missingkey=error").Parse(tplText)
	if err != nil {
		return "", fmt.Errorf("parse sql template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, envs); err != nil {
		return "", fmt.Errorf("render sql template (missing required variable?): %w", err)
	}
	return buf.String(), nil
}
