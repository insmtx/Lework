-- 固定的 Worker 系统 Skill 简体中文展示翻译。
--
-- 本文件在内置 Worker Skill 同步之后执行，使用稳定 code 关联当前系统
-- Skill 与 revision。翻译只维护展示字段，不改动 Skill 的执行内容。
-- 后续 Skill 文案或翻译调整时，请新增更高编号的 SQL 文件人工维护。
-- 若某个 code 当前不存在，JOIN 会跳过该项，避免固定展示 seed 阻断服务启动。
WITH translations(code, metadata_source_hash, translated_name, translated_description) AS (
  VALUES
    ('anysearch', '0a8d2ad53c13f485773b21330b4574309adfa646368560886d4e11a1b6e2cc76', '实时搜索', '实时搜索引擎，支持网页搜索、垂直领域搜索、并行批量搜索和 URL 内容提取。'),
    ('doc-coauthoring', 'ddd9c270f208ff497abb8243437d5028d64c0fd43b9f8fc199cd603e2c3a39e3', '文档协作', '通过结构化流程引导用户共同创作文档，适用于提案、技术规范、决策文档等内容的撰写、迭代和读者验证。'),
    ('docx', '99fdbfeb7964cad472d3fd41967da95175362d7c364b2e2a1a36b4a4d0f401ee', 'Word 文档', '创建、读取、编辑和处理 Word（.docx）文档，包括专业排版、目录、页码、模板、批注和修订。'),
    ('find-skills', 'affedc25900853e276e0a3aed1ab012d2ea58be100bf0ebf224abd9997b0f491', '技能发现', '帮助用户发现并安装可扩展 Agent 能力的技能。'),
    ('pdf', 'c107c3567248ab46aaa322485c46809fdb86cfe5bc1e3399ace8294812939756', 'PDF 处理', '读取、创建、编辑和处理 PDF，包括文本与表格提取、合并拆分、旋转、水印、表单、加密和 OCR。'),
    ('pptx', '976a23de2705171f28f7b4a4c545fc8c821d87b88069413f2f403a6a3cf33477', '演示文稿', '创建、读取和编辑 PowerPoint（.pptx）演示文稿，包括版式、演讲者备注、批注以及合并拆分。'),
    ('skill-creator', '8137b3ac3a0760b81f0cb61d2292f052bdd4f496be36b49c7b7ee0c4296a552f', '技能创建器', '指导用户创建或更新高质量 Skill，以扩展 Agent 的专业知识、工作流和工具集成能力。'),
    ('weather', 'c7a48d901c44ffa803c9b1d8d52ab9624435687d9ca75d4bfda17afb42ab9bb1', '天气查询', '查询当前天气和天气预报，无需 API Key。'),
    ('xlsx', '9c292cbfc1078fcac4134746b2d514a5d17c80f845e282fb01ae5eed12305dba', 'Excel 表格', '创建、读取、编辑和处理 Excel、CSV 与 TSV 表格，包括公式、格式、图表、清洗和转换。')
)
INSERT INTO lework_plugin_translation (
  org_id,
  source_type,
  source_id,
  plugin_revision_id,
  source_revision,
  locale,
  metadata_source_hash,
  translated_name,
  translated_description,
  skill_md_source_hash,
  translated_skill_md,
  created_at,
  updated_at
)
SELECT
  0,
  'system',
  p.id,
  r.id,
  r.revision,
  'zh-CN',
  t.metadata_source_hash,
  t.translated_name,
  t.translated_description,
  '',
  '',
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
FROM translations t
JOIN lework_plugin p
  ON p.code = t.code
 AND p.owner_scope = 'system'
 AND p.org_id = 0
 AND p.kind = 'skill'
 AND p.origin = 'builtin_worker'
 AND p.status = 'active'
 AND p.deleted_at IS NULL
JOIN lework_plugin_revision r
  ON r.plugin_id = p.id
 AND r.revision = p.current_revision
 AND r.status = 'published'
 AND r.deleted_at IS NULL
ON CONFLICT (org_id, source_type, source_id, plugin_revision_id, locale)
DO UPDATE SET
  source_revision = EXCLUDED.source_revision,
  metadata_source_hash = EXCLUDED.metadata_source_hash,
  translated_name = EXCLUDED.translated_name,
  translated_description = EXCLUDED.translated_description,
  updated_at = CURRENT_TIMESTAMP;
