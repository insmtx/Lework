package seed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/insmtx/Leros/backend/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSQLRunTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&types.SeedRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func writeSQLDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestRunSQLScriptsExecutesAndIsIdempotent(t *testing.T) {
	db := newSQLRunTestDB(t)
	dir := writeSQLDir(t, map[string]string{
		"001_create.sql": "CREATE TABLE t (id INTEGER);\nINSERT INTO t VALUES (1);\n",
		"002_data.sql":   "INSERT INTO t VALUES (2);\n",
	})
	if err := RunSQLScripts(context.Background(), db, dir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	var rows int64
	db.Raw("SELECT COUNT(*) FROM t").Scan(&rows)
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}

	// 再跑一次应跳过，不重复插入
	if err := RunSQLScripts(context.Background(), db, dir); err != nil {
		t.Fatalf("second run: %v", err)
	}
	db.Raw("SELECT COUNT(*) FROM t").Scan(&rows)
	if rows != 2 {
		t.Fatalf("expected still 2 rows after idempotent rerun, got %d", rows)
	}

	var recs []types.SeedRecord
	db.Find(&recs)
	if len(recs) != 2 {
		t.Fatalf("expected 2 seed records, got %d", len(recs))
	}
	for _, r := range recs {
		if r.ExecStatus != types.SeedExecStatusSuccess {
			t.Fatalf("expected success for %s, got %v", r.FileName, r.ExecStatus)
		}
	}
}

func TestRunSQLScriptsRendersTemplateFromEnv(t *testing.T) {
	db := newSQLRunTestDB(t)
	t.Setenv("ORG_NAME", "acme")
	dir := writeSQLDir(t, map[string]string{
		"001_init.sqltpl": "CREATE TABLE org (name TEXT);\nINSERT INTO org VALUES ('{{.ORG_NAME}}');\n",
	})
	cfg := dir

	if err := RunSQLScripts(context.Background(), db, cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	var got string
	db.Raw("SELECT name FROM org LIMIT 1").Scan(&got)
	if got != "acme" {
		t.Fatalf("expected org name 'acme', got %q", got)
	}
}

func TestRunSQLScriptsMissingVarErrors(t *testing.T) {
	db := newSQLRunTestDB(t)
	dir := writeSQLDir(t, map[string]string{
		"001_init.sqltpl": "INSERT INTO org VALUES ('{{.MISSING}}');\n",
	})
	cfg := dir

	err := RunSQLScripts(context.Background(), db, cfg)
	if err == nil || !strings.Contains(err.Error(), "missing required variable") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}

func TestRunSQLScriptsDisabledWhenNoDir(t *testing.T) {
	db := newSQLRunTestDB(t)
	if err := RunSQLScripts(context.Background(), nil, "some/dir"); err != nil {
		t.Fatalf("expected nil for nil db, got %v", err)
	}
	if err := RunSQLScripts(context.Background(), db, ""); err != nil {
		t.Fatalf("expected nil for empty dir, got %v", err)
	}
	// 目录不存在时按 B1 策略跳过，不报错。
	if err := RunSQLScripts(context.Background(), db, filepath.Join(t.TempDir(), "no-such-seed-dir")); err != nil {
		t.Fatalf("expected nil for nonexistent dir, got %v", err)
	}
}

func TestRunSQLScriptsIgnoresSubdirectorySQL(t *testing.T) {
	db := newSQLRunTestDB(t)
	dir := writeSQLDir(t, map[string]string{
		"001_create.sql": "CREATE TABLE t (id INTEGER);\n",
	})
	// 子目录内的 .sql 不应被识别为 seed 文件，因此不会执行。
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "002_data.sql"), []byte("INSERT INTO t VALUES (99);\n"), 0o644); err != nil {
		t.Fatalf("write sub file: %v", err)
	}
	cfg := dir

	if err := RunSQLScripts(context.Background(), db, cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	var rows int64
	db.Raw("SELECT COUNT(*) FROM t").Scan(&rows)
	if rows != 0 {
		t.Fatalf("expected subdir sql not executed, rows=0, got %d", rows)
	}
	var recs []types.SeedRecord
	db.Find(&recs)
	if len(recs) != 1 {
		t.Fatalf("expected only 1 seed record for top-level file, got %d", len(recs))
	}
}

func TestRunSQLScriptsResumesFromFailLine(t *testing.T) {
	db := newSQLRunTestDB(t)
	ctx := context.Background()

	failSQL := "CREATE TABLE t (id INTEGER, name TEXT NOT NULL);\nINSERT INTO t (id) VALUES (1);\n"
	dir := writeSQLDir(t, map[string]string{"001_init.sql": failSQL})
	cfg := dir

	// 第一次运行：第 1 行建表成功，第 2 行 name 非空约束被违反而失败。
	if err := RunSQLScripts(ctx, db, cfg); err == nil {
		t.Fatal("expected first run to fail")
	}
	var recs []types.SeedRecord
	db.Where("file_name = ?", "001_init.sql").Find(&recs)
	if len(recs) != 1 {
		t.Fatalf("expected 1 failed record, got %d", len(recs))
	}
	if recs[0].ExecStatus != types.SeedExecStatusFailed {
		t.Fatalf("expected failed status, got %v", recs[0].ExecStatus)
	}
	if recs[0].FailLineAt != 2 {
		t.Fatalf("expected FailLineAt=2, got %d", recs[0].FailLineAt)
	}

	// 改写文件：第 1 行保持不变（续跑时应被跳过），第 2 行改为成功插入。
	dir = writeSQLDir(t, map[string]string{"001_init.sql": "CREATE TABLE t (id INTEGER, name TEXT NOT NULL);\nINSERT INTO t (id, name) VALUES (1, 'x');\n"})
	cfg = dir

	if err := RunSQLScripts(ctx, db, cfg); err != nil {
		t.Fatalf("resume run: %v", err)
	}
	var rows int64
	db.Raw("SELECT COUNT(*) FROM t").Scan(&rows)
	if rows != 1 {
		t.Fatalf("expected exactly 1 row after resume, got %d", rows)
	}

	var succ int64
	db.Model(&types.SeedRecord{}).Where("file_name = ? AND exec_status = ?", "001_init.sql", types.SeedExecStatusSuccess).Count(&succ)
	if succ != 1 {
		t.Fatalf("expected 1 success record, got %d", succ)
	}
}
