# bid-generator — 通用标书生成 Skill

> AI-driven 投标文件生成系统。基于招标文件，通过四步核心流程生成完整的商务标和技术标投标文件。

## 目录结构

```
skills/bid-generator/
├── SKILL.md                    # 主 Skill — 流程编排
├── README.md                   # 本文件
├── .claude-plugin/
│   └── plugin.json             # 插件清单
├── workflows/                  # 工作流（每步一个文件）
│   ├── routing.md              # 路由判断
│   ├── outline-generation.md   # 第1步：大纲生成
│   ├── business-technical-split.md  # 第2步：商务技术拆分
│   ├── service-proposal-writing.md  # 第3步：服务方案写作
│   └── table-backfill.md       # 第4步：报价偏差表回填
├── references/                 # 参考规范
│   ├── format-standards.md     # 排版格式规范
│   ├── checklist.md            # 最终检查清单
│   └── industry-knowledge.md   # 行业经验知识库
├── scripts/                    # 脚本（待扩展）
└── templates/                  # 模板（待扩展）
```

## 核心流程

| 步骤 | 工作流                   | 核心任务                         | 阻塞点        |
| ---- | ------------------------ | -------------------------------- | ------------- |
| 1    | outline-generation       | 提取框架、分项名称、模板清单     | ⛔ 分项确认   |
| 2    | business-technical-split | 只拆需要实质响应的条款           | 不阻塞        |
| 3    | service-proposal-writing | 按模块索要材料并编写正文         | ⛔ 每模块材料 |
| 4    | table-backfill           | 只回填，不重拆条款、不重命名分项 | ⛔ 价格确认   |

## 执行约束

- 前面定框架，后面补内容
- 分项名称一旦确认，全流程不得改名
- 偏差表条款号一旦确认，只校验不重拆
- 服务方案、偏差表、分项报价表必须互相一致
- 绝不编造公司资质、人员、业绩、报价、产品能力

## 使用方式

将本目录作为 Skill 安装。触发词："制作标书"、"生成投标文件"、"写标书"、"投标"
