package seed

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// buildSeedFileList 返回目录下直接子级的 .sql 与 .sqltpl 文件，按文件名升序。
// 仅扫描单层目录：seed 目录结构为单一目录，不递归子目录（子目录会被忽略）。
func buildSeedFileList(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".sql" || ext == ".sqltpl" {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// RunSQLScripts 执行 seed 目录下的 SQL/模板文件。幂等：已成功文件跳过，失败文件断点续跑。
// sqlDir 为相对进程工作目录的脚本目录；db 为 nil 或 sqlDir 为空/不存在时直接返回 nil。
func RunSQLScripts(ctx context.Context, db *gorm.DB, sqlDir string) error {
	if db == nil || strings.TrimSpace(sqlDir) == "" {
		return nil
	}
	if info, err := os.Stat(sqlDir); err != nil || !info.IsDir() {
		logs.Warnf("seed: sql dir %s not found, skip sql seed", sqlDir)
		return nil
	}
	if err := db.AutoMigrate(&types.SeedRecord{}); err != nil {
		return fmt.Errorf("migrate seed record table: %w", err)
	}

	envs, err := loadEnvVars(ctx)
	if err != nil {
		return err
	}

	files, err := buildSeedFileList(sqlDir)
	if err != nil {
		return fmt.Errorf("list seed files in %s: %w", sqlDir, err)
	}

	// 预取已成功文件集合
	type statusMark map[string]types.SeedExecStatus
	mark := statusMark{}
	var records []types.SeedRecord
	if err := db.WithContext(ctx).Find(&records).Error; err != nil {
		return err
	}
	for _, r := range records {
		if _, ok := mark[r.FileName]; !ok || mark[r.FileName] != types.SeedExecStatusSuccess {
			mark[r.FileName] = r.ExecStatus
		}
	}

	for _, path := range files {
		name := filepath.Base(path)
		prev := types.SeedRecord{}
		if terr := db.WithContext(ctx).Where("file_name = ?", name).Order("id DESC").First(&prev).Error; terr != nil && terr != gorm.ErrRecordNotFound {
			return fmt.Errorf("query seed record for %s: %w", name, terr)
		}

		if mark[name] == types.SeedExecStatusSuccess {
			logs.Infof("seed: skip already executed file %s", name)
			continue
		}

		runAt := 0
		if prev.ID != 0 && prev.ExecStatus == types.SeedExecStatusFailed {
			runAt = prev.FailLineAt
		}

		rec := &types.SeedRecord{
			FileName:   name,
			ExecStatus: types.SeedExecStatusFailed,
			StartTime:  time.Now(),
		}
		if err := runSeedFile(ctx, db, path, envs, runAt, rec); err != nil {
			rec.EndTime = time.Now()
			rec.ExecTime = rec.EndTime.Sub(rec.StartTime).Seconds()
			rec.ErrorMessage = err.Error()
			if perr := db.Create(rec).Error; perr != nil {
				return fmt.Errorf("execute seed file %s: %w (persist fail record: %v)", name, err, perr)
			}
			return fmt.Errorf("execute seed file %s: %w", name, err)
		}
		rec.ExecStatus = types.SeedExecStatusSuccess
		rec.EndTime = time.Now()
		rec.ExecTime = rec.EndTime.Sub(rec.StartTime).Seconds()
		if err := db.Create(rec).Error; err != nil {
			return fmt.Errorf("save seed record %s: %w", name, err)
		}
		logs.Infof("seed: executed file %s", name)
	}
	return nil
}

// runSeedFile 执行单个 SQL/模板文件；失败时把失败行号写回 rec。
func runSeedFile(ctx context.Context, db *gorm.DB, path string, envs map[string]string, runAt int, rec *types.SeedRecord) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(content)
	if strings.HasSuffix(strings.ToLower(path), ".sqltpl") {
		rendered, err := renderSQLTemplate(text, envs)
		if err != nil {
			return err
		}
		text = rendered
	}

	statements, err := parseSQLStatements(strings.NewReader(text))
	if err != nil {
		return err
	}
	for _, stmt := range statements {
		if stmt.number < runAt {
			continue
		}
		if err := db.WithContext(ctx).Exec(stmt.line).Error; err != nil {
			rec.FailLineAt = stmt.number
			return fmt.Errorf("line %d: %w", stmt.number, err)
		}
	}
	return nil
}
