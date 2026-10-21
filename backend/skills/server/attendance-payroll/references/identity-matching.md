# 人员身份匹配

本阶段只将当前 `attendance.json` 的人员记录与 Payroll 已有人员档案关联，不更改考勤、不创建或修改 Payroll 人员，也不提交考勤或计算工资。当前流程不支持创建 Payroll 人员；不得引导用户调用创建接口、询问是否新增人员，或把创建档案作为继续核算的前置步骤。

## 只读资料来源

仅通过当前 Runtime 注册的受管 Payroll MCP 工具读取人员资料。不得自行创建 HTTP/MCP 客户端，不得读取、复制或落盘其 Bearer Token，也不得将 Token 写入脚本、命令、JSON、日志、异常或 Artifact。若受管工具不可用，停止并报告；不得以自建请求替代。该 Skill 约束不构成 Runtime 级凭据隔离或调用强制控制。

1. 调用 Payroll MCP `list_roster` 获取当前有效花名册。其记录含 `roster_id`、`name`、`project_name`、`company_name`、`employee_type`、`position` 和 `gender`，**不含 `payroll_person_id`**。两个 ID 属于不同业务标识，禁止按数值相同、顺序或历史样本推导关联。
2. 项目名称先通过任务级项目绑定确定。静态 `project-aliases.json` 已不再参与运行；每个来源项目都必须由用户从当前 `list_roster` 返回的项目候选中明确选择一个 Payroll 项目。单项目和多项目都使用同一流程，一个任务只允许一个工资月份及一份完整绑定。执行前将 `list_roster` 原始 JSON 原样保存到 `<workdir>/.attendance-work/payroll-roster.json`，不得编辑或从 `roster_id` 推断 Payroll 人员 ID。生成项目绑定确认清单：

   ```sh
   python3 scripts/project_binding.py prepare \
     --attendance <workdir>/attendance.json \
     --roster <workdir>/.attendance-work/payroll-roster.json \
     --output <workdir>/.attendance-work/project-binding.json
   ```

   对包含 Payroll 处理意图的任务，在首次考勤核对确认时一并展示考勤月份、考勤人数、全部来源项目映射和考勤核对表；不新增单独确认轮次。展示需使用以下稳定行格式，且每个来源项目只出现一条映射：

   ```text
   考勤月份：2026-06
   考勤人数：XX 人
   考勤确认表：考勤确认表.md
   待核事项：无（如有则列出必要事项）
   项目对应关系：
   来源项目：瀚阅府西苑项目 → Payroll 项目：瀚阅府
   请确认考勤事实及以上项目对应关系。
   ```

   仅识别和交付考勤表时不调用 Payroll MCP，也不建立项目绑定。可以保存 `list_roster` 的直接 DTO，或保存 MCP `CallToolResult` 原始 JSON 包装；包装形式必须同时含单一文本 JSON 内容和 `structuredContent`，两者规范化后必须完全一致。脚本只解包已证实的包装结构，候选来源摘要仍绑定所保存的完整原始响应。收到明确确认后，生成 `<workdir>/.attendance-work/project-binding-request.json`，至少包含：`month`、`attendance_sha256`、`candidate_response_sha256`、完整 `bindings`、`display_evidence`、`user_confirmation` 及 `conversation_messages`。`display_evidence` 应记录助手展示消息的索引、原文、SHA-256、月份、考勤 SHA、花名册响应 SHA、完整映射和规范化映射 SHA；`user_confirmation` 应记录确认消息索引、原文及 SHA-256；`conversation_messages` 应提供这两条带 `index`、`role`、`content` 的可核对消息。摘要由脚本依据原文计算后校验，不要手工猜测摘要。再运行：

   ```sh
   python3 scripts/project_binding.py confirm \
     --attendance <workdir>/attendance.json \
     --roster <workdir>/.attendance-work/payroll-roster.json \
     --request <workdir>/.attendance-work/project-binding-request.json \
     --output <workdir>/.attendance-work/project-binding.json
   ```

   脚本只接受当前有效花名册出现的精确 Payroll 项目名称，要求每个来源项目恰好绑定一次；展示文本必须包含当前月份和每条标准映射行，展示索引必须早于确认索引，展示/确认消息的角色和内容必须与消息清单一致。用户可简短回复“确认以上考勤及项目对应关系”“确认上述项目映射”“以上项目对应关系无误”或“确认，按上述项目归属处理”，无需复述项目名。意图判断会先规范化空白和标点，再要求整条消息完整匹配明确确认格式；旧版逐项确认格式也只接受完整匹配。附带否定、暂缓、条件、疑问、人员/项目范围调整或其他未识别语句时均失败关闭，不能因消息包含确认短语就通过；此规则是有限模板识别，不是任意自然语言语义理解。项目映射摘要必须与请求和最终绑定内容一致，且当前考勤、花名册响应、月份任一变化均须重新展示和确认。既有 R3.9 绑定按规则版本失效，须重新建立。消息清单和摘要只用于 Skill 本地一致性校验；Skills-only 无法认证 Runtime 消息来源，也不能证明消息来自真实用户/助手对话。项目绑定确认不是身份确认，也不是 Payroll 写入授权。

3. 用脚本生成绑定当前考勤、花名册及项目绑定的候选请求：

   ```sh
   python3 scripts/person_matches.py candidate-request \
     --attendance <workdir>/attendance.json \
     --roster <workdir>/.attendance-work/payroll-roster.json \
     --binding <workdir>/.attendance-work/project-binding.json \
     --output <workdir>/candidate-query.json
   ```

   将文件中的 `records` 数组原样传给 `resolve_payroll_person_candidates`，并把实际发送的整个请求体原样保存为 `candidate-query.json`。每批最多 100 条，`client_ref` 必须唯一。脚本使用当前确认的 Payroll 项目名生成候选查询，并省略 `position` 查询字段，避免岗位变更造成候选过滤或冲突；岗位仍作为可解释的辅助信息检查。
4. 将请求批量传给只读接口 `resolve_payroll_person_candidates`，并把完整请求和响应分别保存为候选查询与响应文件。Payroll 的人员关联在独立关系数据中；该响应才可提供 `payroll_person_id` 与 `roster_id` 的明确关联。候选含身份字段、`matched_fields`、`conflicting_fields`、`match_status`、`review_reasons` 及证据。该工具只返回候选，不会替用户确认或写入 Payroll。

   候选响应必须是当前 Task 中实际受管 MCP 调用返回的完整原文；不得用脚本、手工 ID 表、花名册或其他 Task 数据重建候选对象。候选查询文件必须对应当前考勤月份和全部当前 `record_id`，响应中的 `client_ref` 集合必须与查询集合完全相同。`person_matches.py` 会校验集合、字段和原始文件摘要，但本地文件及 SHA-256 只能绑定内容，不能认证 MCP 来源。当前 Runtime 没有向该 Skill 脚本提供可验证的工具调用收据；因此若 Agent 无法直接取得并原样保存当前 Task 的受管工具响应，必须停止身份匹配，不得把本地模拟或重建结果当作真实候选。此边界不是脚本可强制执行的来源认证；要获得机器可验证保证，需要 Runtime 提供绑定 Task、工具名、请求摘要和原始响应摘要的可信只读调用收据。

把工具返回的 JSON 响应保存为临时输入文件，调用：

```sh
python3 scripts/person_matches.py build \
  --attendance <workdir>/attendance.json \
  --roster <workdir>/.attendance-work/payroll-roster.json \
  --binding <workdir>/.attendance-work/project-binding.json \
  --candidates <workdir>/candidate-response.json \
  --query <workdir>/candidate-query.json \
  --output <workdir>/person-matches.json
```

`person-matches.json` 对唯一花名册匹配的正常人员只保存 `record_id`、Payroll 人员 ID、花名册 ID、匹配方法及原因；异常人员保留确认所需的候选详情。文件绑定考勤、花名册、候选响应、请求和项目绑定摘要以支持安全刷新，不复制完整 Payroll 档案。生成结果不能替代考勤核对表，也不应声明为正式 Artifact。

`build` 的 CLI 回执包含原有 `ok`、`count`、`path` 字段，并增加 `status_counts`，可直接汇报已匹配、歧义、未找到和待确认人数。

## 状态及确定性规则

- `matched`：优先快速判断考勤姓名与显式规范项目在当前有效花名册中是否唯一。若唯一，候选响应还必须将唯一 `payroll_person_id` 明确关联到该 `roster_id`，候选与花名册身份字段一致、无已知关键冲突，且没有其他与已知事实相容的 Payroll ID。满足后记录 `match_method=active_roster_unique`。考勤来源缺少公司或用工性质不构成冲突；岗位为软辅助信息，不阻止匹配，差异作为提示保留。匹配只表示身份关系明确。
- `ambiguous`：有多个对已知事实均无冲突的候选。
- `unmatched`：没有候选。
- `pending_confirmation`：候选存在但无法从唯一当前有效花名册记录和可信候选关联中安全确定身份，或出现来源冲突、失效/缺失关联。

姓名、公司、用工性质只做去除首尾空格后的精确比较，不使用姓名相似度、拼音或缺失值补全。项目只采用当前任务 `project-binding.json` 中由用户确认的精确映射；不进行模糊匹配，也不从文件名、项目文本相似度或历史任务推断项目。`attendance.json` 中的原始项目名保留为 `source_project_snapshot`，确认后的 Payroll 项目名作为 `project_snapshot`，两者不得混用。`list_roster` 不暴露 Payroll 人员 ID，所以即使姓名和项目唯一，仍须由候选响应证实 ID 到 roster 的关系；不会将 `roster_id` 当成 Payroll ID。

Payroll 当前候选响应中的 `match_status=REQUIRES_REVIEW` 是候选工具的常规状态，不单独代表候选无效。`evidence_level` 的已实现值为 `NAME_ONLY`、`SUPPORTING`、`CONFLICTING`；`project_match_status` 的已实现值包括 `PROJECT_MATCHED`、`PROJECT_UNKNOWN`、`PROJECT_CONFLICT`、`NOT_REQUESTED`。`review_reasons` 中的 `*_EVIDENCE_MISSING` 表示该候选字段缺少佐证，不表示身份冲突；若当前有效花名册唯一且候选响应确证其 Payroll ID 关联，来源缺少公司/用工性质仍可自动匹配。`*_EVIDENCE_CONFLICT` 和 `MULTIPLE_ROSTER_IDENTITIES` 表示必须阻止普通确认。实际规则以脚本对字段和当前 `list_roster` 响应的交叉校验为准，未知枚举或结构一律保守拒绝。

### 自动匹配与普通确认

- 自动 `matched` 的最低条件是：姓名非空，项目能按显式别名确定；当前有效花名册仅有一条相同姓名和规范项目记录；候选响应明确将一个有效 Payroll 人员 ID 关联到该 roster；候选与花名册身份字段一致；无已知公司、用工性质、项目或其他关键冲突；没有其他相容 Payroll ID。来源公司、用工性质和岗位缺失不阻断，已知冲突会阻断。岗位不是硬匹配条件。
- 普通人工确认可以补足考勤来源缺失的公司、用工性质等字段，但只限于：候选姓名与考勤姓名一致，候选和 Payroll 身份证据内部一致，候选关联当前 `list_roster` 中有效且核心身份字段一致的唯一花名册记录，并且没有关键冲突或多重花名册身份。岗位差异及 `POSITION_EVIDENCE_CONFLICT` 是软提示，不单独阻断确认；`*_EVIDENCE_MISSING` 本身也不是冲突。用户须先看到候选的实际项目、公司、用工性质、岗位、人员 ID 和有效性，再明确指定该候选。
- 普通确认必须拒绝姓名、项目、公司或用工性质冲突，`evidence_level=CONFLICTING` 中的核心字段冲突、项目状态无法由明确别名解释、多个身份来源不一致、花名册缺失/失效/不一致，以及 `MULTIPLE_ROSTER_IDENTITIES`。本 Skill 没有强制覆盖路径；即使用户要求绕过，也不能直接改写身份映射。
- Payroll 可能在未配置服务端别名时仍返回项目冲突。脚本只在候选、花名册和身份证据中出现的所有非空项目名都按显式映射归一为同一个来源项目时，才把该项目冲突视为可解释；真实的多项目证据冲突仍阻断。岗位字段不参与该别名判断。
- 若响应出现无法识别的关键状态、无法核验候选身份或候选证据与当前花名册不一致，保留待确认并重新获取当前只读资料或报告阻塞，不猜测。

### Payroll 服务能力边界

当前 `list_roster` MCP 返回的活动花名册 DTO 只有 `roster_id`、姓名、项目、公司、用工性质、岗位和性别，没有 Payroll 人员关联 ID。Payroll 的只读候选服务会结合花名册、历史工资和历史考勤证据读取已有身份关系，因此脚本仍需要候选响应提供明确 ID 关联；随后以活动花名册唯一索引先作快速判定，仅将正常人员写成精简结果。

这减少的是 Skill 的本地校验分支、保存的候选细节和用户确认次数；由于现有 MCP 契约没有直接的只读 roster-to-person 查询，不能声称减少了 Payroll 服务端历史证据查询。真正跳过正常人员的深度候选解析需要 Payroll 提供受控的只读关联查询，留待 P01。

## 用户确认

用户自然语言明确指定人员关系后，Agent 仅将其转换成下列结构化批次；每项必须引用 `record_id` 和候选响应中的 `payroll_person_id`，并附简短的用户确认依据：

```json
{
  "confirmations": [
    {
      "record_id": "doc-1-p1-r4",
      "payroll_person_id": 123,
      "user_confirmation": "用户明确确认该考勤记录对应此人员"
    }
  ]
}
```

```sh
python3 scripts/person_matches.py confirm \
  --attendance <workdir>/attendance.json \
  --roster <workdir>/.attendance-work/payroll-roster.json \
  --binding <workdir>/.attendance-work/project-binding.json \
  --candidates <workdir>/candidate-response.json \
  --matches <workdir>/person-matches.json \
  --request <workdir>/confirmations.json \
  --query <workdir>/candidate-query.json
```

脚本先整体核对考勤、花名册、项目绑定、候选响应、候选查询请求及映射中的候选快照，再复用与自动匹配相同的核心候选安全校验，验证所有记录、候选 ID 和重复目标，最后原子写入；任一错误都不产生部分修改。确认操作必须使用与构建映射时相同的查询请求、项目绑定和只读 Payroll 响应。用户确认只确认身份对应关系，不批准考勤，不授权进入工资阶段。若候选不在当前响应中，重新查询并向用户说明候选变化，不得手工填入任意 ID。考勤来源、花名册或项目绑定变化后必须重新构建候选。

确认前向用户展示足以辨别身份的候选信息：Payroll 人员 ID、姓名、项目、公司、用工性质、岗位、当前花名册有效性，以及任何冲突/缺失证据。不能只展示姓名并询问“是否匹配”。用户的回复必须能唯一对应某条考勤 `record_id` 和一个候选 ID；含糊回复不得转换为确认。批量确认先校验全部项目，任何一项不安全则整批拒绝且不写入文件。

W03 仅允许 `list_roster` 和 `resolve_payroll_person_candidates` 两项只读查询。禁止调用 `submit_attendance`、`calculate_payroll` 或任何 Payroll 写接口。身份匹配成功不代表考勤审批，也不代表用户授权工资计算。

面向用户统一表达为：`matched` 是“已关联”；`ambiguous` 或 `pending_confirmation` 是“待核实”；`unmatched` 是“当前 Payroll 未找到可关联的人员档案，本次暂不纳入自动核算范围”。只有用户明确同意本次处理范围后，才可继续处理该范围；未匹配记录仍保留为业务异常，不建议创建人员，也不得按姓名猜测关联。

### 本次核算范围排除（不更改身份）

用户明确表示某条仍为 `ambiguous` 或 `pending_confirmation` 的考勤记录暂不纳入本次核算时，可以把它作为**范围排除决定**处理；不得把该记录改成 `matched`、`unmatched`，不得删除它或修改候选快照。需要先以稳定 `record_id`、姓名和具体原因记录决定，并绑定当前 Review 的月份/版本/`attendance.json` SHA-256、`person-matches.json` SHA-256，以及当前对话中用户明确确认该人员本次不纳入的消息索引和消息摘要。随后 `workflow_gate.py` 会校验 `evidence.scope_exclusion_decisions`，要求每条未解决的歧义记录均有对应决定，且确认消息与当前会话、人员和当前摘要一致；未确认、过期、额外或冲突决定均阻断。

排除决定仅作用于本次 `matched + COVERED` 核算范围，不改变考勤事实或 Payroll 人员身份。它会进入范围展示及执行授权摘要，决定、考勤或映射变化后旧范围授权失效。其他未解决的 `ambiguous` / `pending_confirmation` 仍会阻断。`unmatched` 继续按既有规则保留为身份待核并排除在自动核算范围之外；用户范围排除不是身份匹配的替代品。

门禁只能验证调用者提供的消息索引/摘要与当前对话文本一致，不能由 Skills-only 认证消息确实来自 Runtime 用户，也不能阻止 Agent 绕过脚本。因此必须直接核对当前用户原话；本地排除证据不具备服务端授权效力。

完成后向用户说明已关联、待核实和暂不纳入的人数及人员。身份映射不代表考勤已正式提交，也不授权工资核算；后续工资处理必须单独获得用户明确授权。
