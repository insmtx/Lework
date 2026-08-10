package seed

import (
	"context"

	"github.com/ygpkg/yg-go/logs"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter"
	"github.com/insmtx/Leros/backend/internal/service"
)

// Options 自定义种子初始化行为。
type Options struct {
	// LLMConfig 用于初始化系统级 LLM 模型；为空或未配置 APIKey 时跳过 LLM 种子。
	LLMConfig *config.LLMConfig
	// SQLScriptDir SQL 脚本目录（相对进程工作目录）；为空时跳过 SQL 种子。
	SQLScriptDir string
}

// SeedCoreData 初始化核心数据：OSS 账号种子（默认组织/用户/用户组织关联/默认 worker 部署）+ 两版系统级 LLM 模型。
// 与 Run 不同，SeedCoreData 不包含内置内容（AI 队友模板、技能、连接器），供集成测试复用。幂等。
func SeedCoreData(ctx context.Context, db *gorm.DB, edition adapter.Edition, llmCfg *config.LLMConfig) error {
	if db == nil {
		return nil
	}
	if isOSS(edition) {
		if err := seedAccount(ctx, db); err != nil {
			return err
		}
	}
	return seedLLM(ctx, db, llmCfg)
}

// Run 执行启动初始化种子：核心数据（SeedCoreData）+ 内置内容（AI 队友模板、server/worker 技能、连接器）。
// 幂等，重复执行安全。内置技能 sync 失败仅告警（Warn）不阻断启动。
func Run(ctx context.Context, db *gorm.DB, edition adapter.Edition, opts Options) error {
	if db == nil {
		return nil
	}
	if edition == nil {
		logs.Warn("seed: edition is nil, will skip OSS account seeds")
	}

	// 1-2. 核心数据：OSS 账号种子 + 系统级 LLM 模型
	if err := SeedCoreData(ctx, db, edition, opts.LLMConfig); err != nil {
		return err
	}

	// 2.5 可选：基于固定目录的 SQL 文件初始化
	if err := RunSQLScripts(ctx, db, opts.SQLScriptDir); err != nil {
		return err
	}

	// 3. 内置 AI 队友模板（头像系统级归属）
	if err := service.SeedAITeammateTemplates(ctx, db, ""); err != nil {
		return err
	}

	// 4. 内置 server 技能市场（失败仅告警）
	if report, err := service.SyncBuiltinServerSkillMarketplace(ctx, db, ""); err != nil {
		logs.Warnf("seed: built-in server skill sync skipped: %v", err)
	} else {
		logBuiltinSkillReport("seed: built-in server skill", report)
	}

	// 5. 内置连接器模板（失败仅告警）
	if report, err := service.SyncBuiltinConnectorTemplates(ctx, db, ""); err != nil {
		logs.Warnf("seed: built-in connector sync skipped: %v", err)
	} else {
		logBuiltinSkillReport("seed: built-in connector", report)
	}

	// 6. 内置 worker 技能（失败仅告警）
	if report, err := service.SyncBuiltinWorkerSkills(ctx, db, ""); err != nil {
		logs.Warnf("seed: built-in worker skill sync skipped: %v", err)
	} else {
		logBuiltinSkillReport("seed: built-in worker skill", report)
	}

	return nil
}

// logBuiltinSkillReport 汇总内置内容同步报告；失败项逐条告警。
func logBuiltinSkillReport(prefix string, report *service.BuiltinSkillSyncReport) {
	if report == nil {
		return
	}
	for _, failure := range report.Failures {
		logs.Warnf("%s %s sync failed: %v", prefix, failure.Code, failure.Err)
	}
	logs.Infof("%s sync complete: scanned=%d created=%d updated=%d unchanged=%d restored=%d failed=%d",
		prefix, report.Scanned, report.Created, report.Updated, report.Unchanged, report.Restored, len(report.Failures))
}
