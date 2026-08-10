package seed

import (
	"strings"
	"testing"
)

func TestParseSQLStatementsSplitsOnSemicolonAndTracksLine(t *testing.T) {
	src := "-- comment\nINSERT INTO t VALUES (1);\n\n# another\nINSERT INTO t VALUES (2);\n/* block */\nINSERT INTO t VALUES (3)\n"
	stmts, err := parseSQLStatements(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(stmts))
	}
	if stmts[0].number != 2 {
		t.Errorf("expected first stmt at line 2, got %d", stmts[0].number)
	}
	if !strings.Contains(stmts[2].line, "VALUES (3)") {
		t.Errorf("expected trailing stmt without semi, got %q", stmts[2].line)
	}
}

func TestParseSQLStatementsHandlesNoFinalSemicolon(t *testing.T) {
	stmts, err := parseSQLStatements(strings.NewReader("INSERT INTO t VALUES (10);INSERT INTO u VALUES (20)"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
}

func TestParseSQLStatementsSemicolonInSingleQuoteNotTerminator(t *testing.T) {
	src := `INSERT INTO t VALUES ('a;b');INSERT INTO u VALUES (1);`
	stmts, err := parseSQLStatements(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	if !strings.Contains(stmts[0].line, `('a;b')`) {
		t.Errorf("expected first stmt to contain full ('a;b'), got %q", stmts[0].line)
	}
	if !strings.Contains(stmts[1].line, "VALUES (1)") {
		t.Errorf("expected second stmt VALUES (1), got %q", stmts[1].line)
	}
}

func TestParseSQLStatementsEscapedQuoteInsideString(t *testing.T) {
	src := `INSERT INTO t VALUES ('it''s; ok');`
	stmts, err := parseSQLStatements(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(stmts) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(stmts))
	}
	if !strings.Contains(stmts[0].line, `('it''s; ok')`) {
		t.Errorf("expected stmt to contain ('it''s; ok'), got %q", stmts[0].line)
	}
}

func TestParseSQLStatementsSemicolonInDoubleQuoteNotTerminator(t *testing.T) {
	src := `INSERT INTO t ("a;b") VALUES (1);INSERT INTO u VALUES (2);`
	stmts, err := parseSQLStatements(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	if !strings.Contains(stmts[0].line, `"a;b"`) {
		t.Errorf("expected first stmt to contain \"a;b\", got %q", stmts[0].line)
	}
	if !strings.Contains(stmts[1].line, "VALUES (2)") {
		t.Errorf("expected second stmt VALUES (2), got %q", stmts[1].line)
	}
}

func TestRenderSQLTemplateReplacesVars(t *testing.T) {
	out, err := renderSQLTemplate("INSERT INTO org (code) VALUES ('{{.ORG_CODE}}');", map[string]string{"ORG_CODE": "acme"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "'acme'") {
		t.Errorf("expected rendered value, got %q", out)
	}
}

func TestRenderSQLTemplateMissingVarErrors(t *testing.T) {
	if _, err := renderSQLTemplate("SELECT '{{.MISSING}}';", map[string]string{}); err == nil {
		t.Fatal("expected error for missing variable")
	}
}
