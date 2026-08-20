package skill

import "testing"

func TestParseTokensFromSkillChips(t *testing.T) {
	content := FormatChip("create-word-doc", "创建 Word 文档") + " 然后 " + FormatChip("government-recognition-policy", "政府认定政策")
	codes, remaining := ParseTokens(content)
	if len(codes) != 2 || codes[0] != "create-word-doc" || codes[1] != "government-recognition-policy" {
		t.Fatalf("codes = %#v", codes)
	}
	if remaining != "create-word-doc 然后 government-recognition-policy" {
		t.Fatalf("remaining = %q", remaining)
	}
	if !HasInvokedSkills(content) {
		t.Fatal("expected HasInvokedSkills")
	}
}

func TestParseTokensIgnoresSlashCodeAndDedupes(t *testing.T) {
	if codes := ParseTokensOnly("/create-word-doc 请写文档"); len(codes) != 0 {
		t.Fatalf("slash code should not parse, got %#v", codes)
	}
	content := FormatChip("docx", "创建 Word 文档") + FormatChip("DOCX", "Word")
	codes := ParseTokensOnly(content)
	if len(codes) != 1 || codes[0] != "docx" {
		t.Fatalf("codes = %#v", codes)
	}
}

func TestPlainTextUsesCatalogCode(t *testing.T) {
	content := `请 <skill-chip data-code="docx">创建 &lt;Word&gt; 文档</skill-chip> 完成`
	if got := PlainText(content); got != "请 docx 完成" {
		t.Fatalf("plain = %q", got)
	}
}

func TestDisplayTextUsesChineseLabel(t *testing.T) {
	content := `<skill-chip data-code="bid-backfill">投标文件回填</skill-chip> 测试`
	if got := DisplayText(content); got != "投标文件回填 测试" {
		t.Fatalf("display = %q", got)
	}
	if got := PlainText(content); got != "bid-backfill 测试" {
		t.Fatalf("plain = %q", got)
	}
}
