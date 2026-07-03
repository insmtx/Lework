---
description: Failure recovery matrix for bid-generator
---

# Failure Recovery Matrix

Central recovery rules for common bid-generator failures. Step-specific workflow files may add narrower handling, but must not weaken these stop/continue decisions.

**Hard rule**: A failed required artifact blocks the next gate. A failed convenience surface falls back and does not block.

---

## 1. Recovery Matrix

| Failure Point | Blocking | Automatic Recovery | User Intervention | Resume Entry |
|---------------|:--------:|--------------------|-------------------|-------------|
| 招标文件中找不到"投标文件格式"章节 | Yes | Try headings "投标文件组成""响应文件格式"等变体 | If all fail, ask user to identify the section | Step 3 re-scan |
| 招标文件无法用 pandoc 解析 | Yes | Try python-docx directly | If both fail, ask user to provide readable format | Step 1 |
| 底版切出后发现缺少关键模板模块 | Yes | None — must re-scan | User confirms the correct body index range | Step 3 re-extraction |
| 底版第一页为空白页 | No | Script auto-skips leading page-break paragraphs | No | Step 3 |
| 分项名称在招标文件中不明确 | Yes | None | User must provide or confirm the names | Step 1 |
| 用户提供的材料不足以支撑某模块编写 | No | Mark "待补充" and continue to next module | User decides whether to provide or skip | Step 4 next module |
| 偏差表回填时发现引用位置不存在 | No | Mark the reference and ask user to verify | User corrects the reference | Step 5 re-backfill |
| 报价合计与投标函总价不一致 | Yes | None | User must reconcile | Step 5 price reconciliation |
| 用户中途想回到上一步修改 | No | Preserve downstream outputs, ask user which step to redo | User confirms which steps are affected | Any step |
| 底版文件损坏或格式异常 | Yes | Re-extract with same range | If persists, check source file | Step 3 re-extraction |
| pandoc 提取文本出现乱码 | No | Try alternative extract methods (python-docx direct) | Only if all methods fail | Step 1 |

## 2. FALLBACK Paths

| When primary path fails | FALLBACK |
|------------------------|----------|
| pandoc 无法提取招标文件文本 | 使用 python-docx 直接读取段落文本 |
| 招标文件中找不到"投标文件格式"章节标题 | 搜索变体: "投标文件组成""响应文件格式""投标文件内容" |
| 无法精确定位章节起始索引 | 列出 body 所有 Heading 下的段落摘要，让用户选择 |
| 用户无法确认分项名称 | 标记"需人工确认"，后续报价表该名称留空 |
| 自拟模块材料不齐 | 标记"待补充"后继续，最终核查时提醒用户 |

## 3. Step-Specific Recovery Rules

| Step | Failure | Recovery |
|------|---------|----------|
| Step 1 | 无法完整读取招标文件 | 尝试分章节读取，标注"部分章节不可读" |
| Step 2 | 某条款响应性质难以判断 | 默认归"商务承诺"，标注"待人工确认" |
| Step 3 | 脚本执行失败 | 检查 python3 环境和依赖，手动调整 --from 参数 |
| Step 4 | 某模块编写中断 | 保存已完成模块，恢复时从断点继续 |
| Step 5 | 偏差表模板列结构不匹配 | 以招标文件模板为准，调整回填字段顺序 |
| Final | 核查发现废标项 | 立即修复对应步骤，重新生成最终 docx |
