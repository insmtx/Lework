-- 纯 SQL 种子示例：建独立测试表并插入一行，验证 RunSQLScripts 基础执行路径。
-- 幂等由 backend 的 seed_records 表保证：本文件首次成功后再次启动会被跳过。
CREATE TABLE IF NOT EXISTS seed_demo_marker (
  id      SERIAL PRIMARY KEY,
  note    TEXT,
  run_at  TIMESTAMPTZ DEFAULT now()
);
INSERT INTO seed_demo_marker (note) VALUES ('seed: plain sql');
