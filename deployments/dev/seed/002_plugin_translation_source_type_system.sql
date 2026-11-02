-- 扩展系统 Skill 展示翻译的来源类型。
--
-- GORM AutoMigrate 不会稳定替换既有 CHECK 约束，因此以一次性 SQL
-- 迁移兼容已存在的 PostgreSQL 数据库。文件名排在 003 翻译数据之前，
-- 确保 source_type = 'system' 可先写入。
ALTER TABLE lework_plugin_translation
  DROP CONSTRAINT IF EXISTS chk_plugin_translation_source_type;

ALTER TABLE lework_plugin_translation
  ADD CONSTRAINT chk_plugin_translation_source_type
  CHECK (source_type IN ('marketplace', 'organization', 'system'));
