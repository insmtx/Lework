//go:build integration

package seed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// 集成测试：连真实 Postgres，验证 RunSQLScripts 在真实库上的执行、幂等与断点续跑。
// seed 包内无法 import testutil（testutil 依赖 seed，会形成 import cycle），
// 因此直接通过 LEROS_TEST_DB_URL（可选覆盖）指向本地开发库建连。
// 默认 DSN 与 deployments/dev/server.config.yaml 保持一致。

const defaultTestDBURL = "postgres://postgres:123456@localhost:5432/leros_init_test?sslmode=disable"

func newIntegTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LEROS_TEST_DB_URL")
	if dsn == "" {
		dsn = defaultTestDBURL
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect test db %s: %v", dsn, err)
	}
	return db
}

// dropIntegTables 清理本次测试创建的种子表，保证幂等复用同一开发库。
func dropIntegTables(t *testing.T, db *gorm.DB, tables ...string) {
	t.Helper()
	for _, name := range tables {
		if err := db.Exec("DROP TABLE IF EXISTS " + name).Error; err != nil {
			t.Fatalf("drop table %s: %v", name, err)
		}
	}
}

// clearIntegSeedRecords 删除指定文件名前缀的 seed 记录，避免上次运行留下的 succ 记录导致跳过。
func clearIntegSeedRecords(t *testing.T, db *gorm.DB, filePrefix string) {
	t.Helper()
	if err := db.Where("file_name LIKE ?", filePrefix+"%").Delete(&types.SeedRecord{}).Error; err != nil {
		t.Fatalf("clear seed records %s%%: %v", filePrefix, err)
	}
}

func TestRunSQLScripts_Integration_SQLAndTemplate(t *testing.T) {
	db := newIntegTestDB(t)
	dropIntegTables(t, db, "seed_demo_marker_integ")
	clearIntegSeedRecords(t, db, "seed_demo_marker_integ")
	dir := writeSQLDirInt(t, map[string]string{
		"seed_demo_marker_integ_001.sql":    "CREATE TABLE seed_demo_marker_integ (id SERIAL PRIMARY KEY, note TEXT);\nINSERT INTO seed_demo_marker_integ (note) VALUES ('plain');\n",
		"seed_demo_marker_integ_002.sqltpl": "INSERT INTO seed_demo_marker_integ (note) VALUES ('{{.LEROS_TEST}}');\n",
	})
	t.Setenv("LEROS_TEST", "tmpl")

	if err := RunSQLScripts(context.Background(), db, dir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	assertRows(t, db, "seed_demo_marker_integ", 2)

	// 幂等：再跑一次应跳过
	if err := RunSQLScripts(context.Background(), db, dir); err != nil {
		t.Fatalf("second run: %v", err)
	}
	assertRows(t, db, "seed_demo_marker_integ", 2)
	assertSeedRecords(t, db, "seed_demo_marker_integ", 2)
}

func TestRunSQLScripts_Integration_ResumeOnFailure(t *testing.T) {
	db := newIntegTestDB(t)
	dropIntegTables(t, db, "seed_demo_fail_integ")
	clearIntegSeedRecords(t, db, "001_init.sql")
	ctx := context.Background()
	dir := writeSQLDirInt(t, map[string]string{
		"001_init.sql": "CREATE TABLE seed_demo_fail_integ (id SERIAL PRIMARY KEY, name TEXT NOT NULL);\nINSERT INTO seed_demo_fail_integ (id) VALUES (1);\n",
	})

	if err := RunSQLScripts(ctx, db, dir); err == nil {
		t.Fatal("expected first run to fail on NOT NULL violation")
	}
	var rec types.SeedRecord
	if terr := db.Where("file_name = ?", "001_init.sql").Order("id DESC").First(&rec).Error; terr != nil {
		t.Fatalf("query failed record: %v", terr)
	}
	if rec.ExecStatus != types.SeedExecStatusFailed || rec.FailLineAt != 2 {
		t.Fatalf("expected failed status with FailLineAt=2, got %v / %d", rec.ExecStatus, rec.FailLineAt)
	}

	// 修复第 2 行后续跑：第 1 行应跳过，只执行第 2 行
	dir = writeSQLDirInt(t, map[string]string{
		"001_init.sql": "CREATE TABLE seed_demo_fail_integ (id SERIAL PRIMARY KEY, name TEXT NOT NULL);\nINSERT INTO seed_demo_fail_integ (id, name) VALUES (1, 'x');\n",
	})
	if err := RunSQLScripts(ctx, db, dir); err != nil {
		t.Fatalf("resume run: %v", err)
	}
	assertRows(t, db, "seed_demo_fail_integ", 1)
	var succ int64
	db.Model(&types.SeedRecord{}).Where("file_name = ? AND exec_status = ?", "001_init.sql", types.SeedExecStatusSuccess).Count(&succ)
	if succ != 1 {
		t.Fatalf("expected 1 success record, got %d", succ)
	}
}

func assertRows(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	var rows int64
	if err := db.Raw("SELECT COUNT(*) FROM " + table).Scan(&rows).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if rows != want {
		t.Fatalf("expected %d rows in %s, got %d", want, table, rows)
	}
}

func assertSeedRecords(t *testing.T, db *gorm.DB, filePrefix string, want int) {
	t.Helper()
	var recs []types.SeedRecord
	db.Where("file_name LIKE ?", filePrefix+"%").Find(&recs)
	if len(recs) != want {
		t.Fatalf("expected %d seed records matching %s%%, got %d", want, filePrefix, len(recs))
	}
	for _, r := range recs {
		if r.ExecStatus != types.SeedExecStatusSuccess {
			t.Fatalf("expected success for %s, got %v", r.FileName, r.ExecStatus)
		}
	}
}

// writeSQLDirInt 写入临时 seed 目录（集成测试内部辅助，避免与单元测试 writeSQLDir 重名）。
func writeSQLDirInt(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}
