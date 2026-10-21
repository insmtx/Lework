# 正式考勤提交与工资核算

本阶段只在用户明确要求后进入。`考勤确认表已生成`、Gate 1 考勤确认、身份匹配完成或已关联人员，都不等同于授权 Payroll 写入。

## 阶段准入与停顿

正常业务顺序为 `ATTENDANCE_REVIEW + PROJECT_BINDING → IDENTITY_MATCHING → SALARY_READINESS → PAYROLL_EXECUTION → ATTENDANCE_SUBMISSION → PAYROLL_PRECHECK → PAYROLL_CALCULATION → PAYROLL_EXPORT`。仅考勤任务首次只交付 Markdown 核对表和简短中文摘要，然后停止；不得在聊天中输出完整人员表。原始请求明确包含工资处理意图时，首次确认前先只读获取当前 `list_roster`、准备完整项目候选，再在**同一条助手消息**展示考勤 Review、月份、人数、核对表、全部项目映射和待核事项。用户用一次明确回复同时确认考勤事实与项目映射；不得先单独询问考勤确认，再补查项目并重复确认。门禁的 `ATTENDANCE_REVIEW` 路由应先返回准备当前花名册并联合展示的下一步；`project_binding.py confirm` 要求消息中包含当前月份、准确人数、核对表/确认表、待核事项及所有映射，并验证完整展示早于同一确认消息。展示缺项、映射被改、展示晚于确认、考勤或花名册变化、否定/疑问/范围变更语句都会阻断。跨轮次确认仍绑定当前任务可见用户消息序号，以及 Review 的月份、版本、`attendance.json` SHA-256 和人员范围；更正或摘要改变后必须重新确认。Skills-only 门禁只约束正常流程，不能强制运行时实际展示或认证消息来源。人员匹配与只读工资覆盖检查继续按原指令意图推进。覆盖检查不是正式 Precheck 或写入授权。覆盖检查后按下方固定文本格式展示唯一范围和排除原因，随后等待一次明确工资核算执行授权。

### Task 级项目绑定

静态 `project-aliases.json` 不再是运行时来源。每个 Task 都必须重新读取 Payroll `list_roster`，把未经改写的直接 DTO 或 MCP `CallToolResult` JSON 包装保存为 `.attendance-work/payroll-roster.json`；若为包装，单一文本 JSON 与 `structuredContent` 必须一致，脚本会验证后仅解包该 DTO，原文件摘要绑定完整原始响应。然后运行：

```sh
python3 scripts/project_binding.py prepare \
  --attendance <workdir>/attendance.json \
  --roster <workdir>/.attendance-work/payroll-roster.json \
  --output <workdir>/.attendance-work/project-binding.json
```

将脚本给出的所有来源项目和精确的 Payroll 项目候选一并展示给用户。在工资任务的首次联合展示中，同时给出当前考勤核对表、月份、准确人数、待核事项和每组“来源项目 → Payroll 项目”关系；用户只需明确确认上述全部内容，无需逐个复述项目名。候选未准备或映射不完整时，不得先发出单独的考勤确认请求。不能从相似名称、文件名、旧 Task、`roster_id` 或候选排序猜测。根据确认原文构造包含 `month`、`attendance_sha256`、`candidate_response_sha256`、完整 `bindings` 和 `user_confirmation`（消息序号及原文）的请求，再运行：

```sh
python3 scripts/project_binding.py confirm \
  --attendance <workdir>/attendance.json \
  --roster <workdir>/.attendance-work/payroll-roster.json \
  --request <workdir>/.attendance-work/project-binding-request.json \
  --output <workdir>/.attendance-work/project-binding.json
```

脚本要求每个来源项目恰有一个当前花名册候选项目映射，并绑定当前月份、考勤 SHA、花名册响应 SHA 和确认消息摘要。一个 Task 可包含多个项目但只能有一个工资月份；之后所有人员按来源项目使用此映射。全部项目合并为一个核算范围、一个固定 Batch、一次提交、一次正式 Precheck 和一次核算；同一 Payroll Person ID 重复出现时提交前阻断。源字段 `source_project_snapshot` 保留原考勤项目名，`project_snapshot` 采用用户确认的 Payroll 项目名。项目绑定变化、考勤 SHA 变化或花名册响应变化时，身份映射和后续覆盖/范围证据必须重新生成。项目确认不代表身份匹配，不代表范围认可，也不代表 Payroll 写入授权。

提交前 `workflow_gate.py` 会确定性地重新验证 Task 项目绑定、当前考勤、人员映射摘要和实际提交请求中的逐人员事实。成功响应中的 `validated_submission_request` 是门禁读取并检查的完整请求对象，`submission_request_sha256` 绑定本次读取的文件内容。Agent 必须把这个对象原样作为 MCP 参数；禁止手工重组、截断或按样例挑选 `records`。门禁会将每条记录按当前 `record_id → payroll_person_id` 身份关系重新投影，并复用工作簿导出器的 Payroll DTO 校验，比较姓名、项目、汇总字段和全部逐日事实；Payroll DTO 没有 `record_id` 字段，若请求额外携带则因结构不符合 DTO 而阻断。单次授权与兼容的旧提交路径都经过该校验。写入后必须立即只读核查固定操作回执及完整人员范围；未知、缺失或不一致时停止，不重试。Skill 不能强制控制 Agent 最终传给 MCP 的参数，因此这只是正常 Skill 流程的校验，不能声称是 Runtime 参数强制保证。Excel 导出也再次验证项目绑定和快照字段一致性。

项目候选及对话消息清单由 Agent 根据工具响应和可见对话构造；Skill 脚本核对消息索引顺序、角色、原文摘要、完整映射及当前业务摘要，但不能认证 MCP 响应或 Runtime 用户消息的真实性，也不能阻止 Agent 直接绕过 Skill 调用 MCP。该本地检查不是 Runtime 级认证。缺少证据或对应不完整时失败关闭。

一次性授权前必须提供实际 `.attendance-work/payroll-readiness.json` 路径。门禁限定报告为当前 `.attendance-work` 下的固定普通文件，并调用 `payroll_readiness.report_status()` 重新核对报告与当前 `attendance.json`、`person-matches.json`、月份及匹配人员范围；再将报告的逐人结果、状态计数、规范化响应摘要、请求人员集合和默认 `COVERED` 范围与门禁输入及展示范围逐项比较。报告缺失、损坏、不可用、过期、符号链接、范围/摘要冲突都失败关闭。正式提交前必须再次调用只读覆盖 MCP 并完成批次冲突和范围复核；不能只凭旧报告或旧授权提交。相同范围及相同覆盖响应内容可继续使用当前授权；若规范化覆盖响应或人员范围改变，必须重新展示并取得新的执行授权，不增加额外授权阶段。

报告校验只是本地内容一致性检查。`payroll_response_sha256` 是原始 MCP 响应文件摘要，状态检查不会重新读取或认证该原始响应，也不证明 MCP 来源真实；一次性授权同样不能认证 Runtime 用户消息。不得将本地校验描述为远端证明或服务端授权。

阶段转换使用 `scripts/workflow_gate.py --request <当前任务内部 JSON 路径>`。输入的 `user_messages` 必须逐字来自当前任务中可见的用户消息；`conversation_messages` 用于核对范围确实由 Agent 展示在授权之前，但脚本不能认证这些内容来自 Runtime。`evidence` 必须提供当前 `attendance.json` 和 `person-matches.json` 路径；考勤复核会读取 `attendance.json` 字节并计算 SHA-256，`PAYROLL_EXECUTION` 还会读取身份映射文件并核对其 SHA-256。一次性执行路径还须提供同目录固定路径 `.attendance-work/payroll-readiness.json`；门禁实际调用 `payroll_readiness.report_status()`，随后对照报告原文、逐人状态、计数、`normalized_response_sha256`、请求人员集合和默认可核算范围，不以请求内自报字段代替文件核验。Review 证据还要说明其展示发生在第几条用户消息之后；跨轮次确认需回传检查器产生的 `attendance_confirmation_evidence`，并由脚本重新核对对应原文、序号、月份、版本、摘要和人员范围。考勤更正或重新生成后的内容摘要/版本与历史确认不一致时必须重新确认。其他阶段证据由身份映射、覆盖报告及只读 Payroll 回执/Precheck 结果构成，并携带同一月份、考勤版本、内容摘要及稳定 `record_id` 范围。可提交阶段还必须带 `task_context.py` 派生的 Task/Batch/Operation ID 和已完成的只读批次冲突检查；范围改变时必须重新查询覆盖和复核批次。`PAYROLL_PRECHECK` 只接受同范围成功提交回执，`PAYROLL_CALCULATION` 在单次执行模式中复用绑定到同范围的执行授权，但仍要求成功提交回执、同范围正式 `valid=true` Precheck 和固定操作 ID；兼容模式仍需独立核算授权。

检查器不提供用户消息读取接口；它不能验证调用者传入的对话文字或 MCP 回执是否真实，也不能阻止 Agent 绕过脚本直接调用 Payroll MCP。一次性授权输出始终标记 `message_source_verified=false`、`is_authorization_proof=false`、`runtime_enforced=false`；覆盖报告状态中的 `payroll_response_sha256_verified=false` 也必须保留。用户授权中若提出增加、排除或替换人员，或处理范围语义不清，门禁拒绝按旧范围执行；先重新匹配、检查覆盖并展示唯一新范围，再用一次新的明确执行授权。无需另设授权阶段。Skills-only 本地复核不能认证用户消息或原始 MCP 响应来源。这是流程校验，不是 Runtime 强制权限。Agent 必须直接对照当前会话的用户原文和真实回执；无法核实时阻断。绝不能把本地 JSON 的 `authorized` 字段或 Agent 自述当作授权。

调用方式：将当前任务用户原文按时间顺序放入 `user_messages`，把当前真实业务文件/只读回执中可核对的阶段数据放入 `evidence`，然后执行：

```sh
python3 scripts/workflow_gate.py --request <workdir>/workflow-gate-request.json
```

`action` 只能是上面的阶段名。正常一次性路径调用 `PAYROLL_EXECUTION`；旧独立授权模式仍可使用 `SUBMISSION_SCOPE_REVIEW`、`ATTENDANCE_SUBMISSION`、`PAYROLL_CALCULATION`。各状态证据须提供一致的 `month`、`version`、`attendance_sha256`、`record_ids`；涉及人员时提供按 `record_id` 对应的 `people`。一次性路径的 `identity_matching`、`salary_readiness`、`execution_scope` 和 `batch_context` 必须互相一致；覆盖结果需带 `normalized_response_sha256`，以便重查后确认响应内容未变。`scope_display` 指向授权前的最新范围展示消息，并附同一范围摘要。准入输出的 `execution_authorization` 只用于后续同一请求链的本地一致性复核，不保存到业务授权系统。缺少任一证据、证据范围不一致或批次冲突都停止。

### 更正后的安全重入

受控考勤更正只修改当前 `attendance.json`；原始 PDF、页级 JSON、每日 `raw_mark` 和原始识别状态保持不变。更正后重新运行核对、导出并声明新的本地考勤 Review。以 `attendance.json` 当前 SHA-256 作为事实版本；当前 Skill 没有独立持久化 Generation。Review 的 `version` 字符串仅作关联标签，不能替代内容摘要。SHA 改变后，旧 Gate 1 确认、Identity、Salary Readiness、一次性 `PAYROLL_EXECUTION` 授权及旧 Precheck/核算证据均不得用于新版；重新确认后，依次重建身份映射、覆盖报告、范围展示和执行授权。

正式执行前，使用 `task_context.py` 从当前 Runtime Task 路径派生的固定 Batch/Operation ID，分别只读调用 `get_attendance_submission(operation_id=submit:<batch_id>)` 和 `get_payroll_calculation(operation_id=calculate:<batch_id>)`。将实际结果规范为 `evidence.payroll_operation_state`：外层必须为 `status=VERIFIED`，并包含当前 `task_id`、`batch_id`、`month`；`operations.submission` 与 `operations.calculation` 各含固定 `operation_id` 和 `status`（仅接受 `NOT_FOUND` 或 `SUCCEEDED`）。`SUCCEEDED` 项还须带 `month`、`attendance_sha256`、稳定 `record_ids` 及精确 `payroll_person_ids`；这些范围必须从保留的原始提交请求/核算结果及其绑定 Review 核对，不能用当前值覆盖旧值。门禁会将它们与本次固定范围比对。查询失败、结果不完整、范围不一致或无法取得原始来源时，状态为未知并停止，禁止声明 `VERIFIED`。

`PAYROLL_EXECUTION` 和 `ATTENDANCE_SUBMISSION` 仅在两个固定操作均查明 `NOT_FOUND` 时允许首次写入；已有成功回执时禁止再次提交或新授权重跑。`PAYROLL_PRECHECK` 必须对应当前 SHA 和范围的成功提交，并确认尚无已成功核算；`PAYROLL_CALCULATION` 必须对应当前成功提交和正式有效 Precheck，且固定计算操作当前为 `NOT_FOUND`。已有核算成功时不再计算；只有当前版本与原成功结果完全一致，才可走既有导出/声明路径。旧提交/核算的 SHA 或人员范围与当前 Review 不同，返回阻断，不以旧回执充当新版成功。

这两个 MCP 查询只按给定 Operation ID 查单个操作，不能列举该月份所有 Task/Batch，也不能证明另一个 Task 没有提交。Payroll 当前没有已核实的修订、撤销、替代或冲销流程。因此，新 Task ID 或 `VERIFIED_NO_CONFLICT` 的本地标记本身不是月度历史无冲突证明；若无法核实跨 Task/Batch 影响，仍须停止正式写入。此处 `payroll_operation_state` 是本地结构校验输入，不认证 MCP 来源、Runtime 对话或授权，也不是服务端强制门禁。

对业务用户只需说明：已有正式考勤或核算记录，当前考勤更正尚未同步到 Payroll；系统没有可核实的安全修订/冲销流程，因此已停止正式提交或重算。不要向用户要求 Task ID、Batch ID、SHA 或脚本错误码。

### 一次性工资核算执行授权（F3.2）

正常路径将范围展示和两个独立写授权收敛为一次业务授权。调用 `PAYROLL_EXECUTION` 前，必须有当前有效考勤确认、身份映射、有效历史工资覆盖报告、唯一的 `matched + COVERED` 默认范围，以及已通过只读核验的固定批次上下文。所有 `ambiguous` / `pending_confirmation` 记录必须保持原身份状态；若用户决定某条仅暂不纳入本次核算，必须先有下述独立排除决定，否则仍阻断。向用户展示月份、准确纳入人员及所有排除人员和原因，并明确说明后续将**正式提交考勤、执行正式预检查、通过后进行工资核算**。范围展示消息位置和其范围摘要随授权证据提交检查；授权消息必须晚于该展示，且有明确工资核算意图、无否定或疑问、没有与范围冲突的人数。

机器解析字段采用稳定文本模板，不要在关键字段内部加入强调标记。有限规范化允许 `**`/`__` 粗体、行首列表标记、冒号、空白和换行差异；月份、人数、纳入名单、排除名单及原因仍须完整一致：

```text
工资核算范围：2026-06，共 31 人
纳入：张三、李四
排除事项：王五：缺少历史工资；赵六：身份待核实
后续将正式提交考勤、执行正式预检查，并在通过后进行工资核算。
```

对仍为 `ambiguous` 或 `pending_confirmation` 的记录，用户可以明确决定该稳定 `record_id` 本次暂不纳入。此决定是独立的 `evidence.scope_exclusion_decisions`，不得写入或伪装成 `person-matches.json` 的身份状态。每项必须含 `record_id`、姓名、具体排除原因、当前月份、Review 版本、考勤 SHA、身份映射 SHA、`confirmation_message_index`、`confirmation_message_sha256`，并标记 `message_source_verified=false`。门禁核验确认消息确实对应当前传入对话中的用户消息，提及该人员并明确表达本次不纳入；决策集合必须与全部 unresolved identity conflicts 一一对应。决策摘要绑定范围摘要和一次性授权；输入版本/映射、决定或授权范围变化都会使旧证据失效。没有确认、证据过期、目标不是未解决身份记录、或有其他未处理冲突时阻断。正常 `unmatched` 继续按既有规则列为身份待核并不进入匹配候选范围。

该排除记录仅表示本地范围决定，不改变考勤或身份事实，不创建 Payroll 人员、不修改 Payroll 花名册，也不证明用户消息来源可信。Skills-only 门禁只能校验调用者提供的消息索引/内容摘要与当前会话文本一致；它不能认证 Runtime 消息，也不能阻止绕过脚本直接调用 MCP。对话来源无法核实则停止。

允许“按刚才展示的 31 人开始工资核算”“确认本次 20 人范围，开始工资核算”“请按上面确认的范围执行工资核算”或“同意按当前已展示范围进行工资核算”等明确执行表达。门禁允许有限的“开始/启动/进行/执行 + 工资核算或工资计算”措辞变化；仍拒绝疑问、否定、范围变更、仅查看/生成表格、仅说“确认考勤”“人员匹配完成”或“继续处理”。不要求复述姓名、版本、SHA 或批次标识；只含“按刚才展示的”而没有核算动作也不是授权。

准入器将授权绑定到月份、Review 版本和 SHA、身份映射 SHA、规范化历史覆盖响应 SHA、精确 `record_ids`、Payroll Person ID 集合、Task/Batch ID 和授权/范围展示消息序号。正式提交前必须重新调用历史覆盖只读 MCP，并重新只读核对已有批次。覆盖响应规范化 SHA、考勤/映射摘要、范围或批次任一变化都使旧授权失效，必须重新展示并取得新授权；不能用新 Batch ID 绕过已有范围。执行授权仍只是本地流程证据，不能认证用户消息，也不是 Payroll 回执。

一次性授权仅在这些精确绑定的证据不变时允许顺序执行：先 `submit_attendance` 并核验真实成功回执；再对相同批次和完整范围执行正式 `precheck_payroll`；仅 `valid=true` 且无阻塞人员时才执行 `calculate_payroll`。结果不明先只读查询，禁止盲目重试；提交回执缺失/失败、Precheck 无效、范围漂移或远端状态无法核实时立即停止，不自动缩围。完整 `SUCCESS` 后，Agent 必须自动进入既有 `PAYROLL_EXPORT` 门禁、执行五 Sheet 导出和 Artifact 声明，不再询问用户是否生成工资表；这属于该一次性授权对应的正常交付步骤，不是额外写授权。导出仍须通过 F2.2 原始响应/考勤快照内容绑定检查。仅完整 SUCCESS 可正式导出。

`PAYROLL_EXECUTION` 是正常流程的单次授权模式。旧版 `SUBMISSION_SCOPE_REVIEW → ATTENDANCE_SUBMISSION` 独立提交授权与独立 `PAYROLL_CALCULATION` 授权路径继续兼容；兼容旧模式时，仍需明确正式提交授权，并且必须另行确认用户明确要求开始工资核算，即另行明确授权工资核算。两种路径都不得跳过提交回执、正式 Precheck、固定批次和范围校验。两种授权模式不可混用；发现 execution token 时如果校验失败，必须阻断，不得退回旧路径。

## 正式提交前：历史工资覆盖只读检查

身份匹配完成且原始任务包含工资处理意图后，自动按当前 `attendance.json`、`person-matches.json` 和目标月份核对历史工资覆盖。该步骤不创建批次，不调用提交、Precheck 或核算写接口。

1. 确认考勤与身份映射仍绑定同一份 `attendance.json`。通过脚本生成精确 MCP 请求：

   ```sh
   python3 scripts/payroll_readiness.py request \
     --attendance <workdir>/attendance.json \
     --matches <workdir>/person-matches.json \
     --month YYYY-MM
   ```

   输出中的 `mcp_request` 对象就是 `check_historical_salary_readiness` 请求体，包含 `payroll_month` 和唯一正整数 `payroll_person_ids`。只使用 `status=matched` 且来源形式符合现有 `person_matches.py` 自动匹配/用户确认契约的 ID；绝不从姓名或 `roster_id` 推断 Payroll ID。未关联、歧义和待核记录单独计数并列出。Payroll MCP 单次最多接收 500 个 ID，超出时脚本拒绝，不能静默截断。
2. 只读调用 Payroll MCP `check_historical_salary_readiness`，将返回的 DTO JSON 原样保存为临时响应文件。工具缺失、超时、错误或结果结构不完整时，调用 `unavailable` 记录检查不可用，并停止正式提交；不得退回旧流程。
3. 校验并写入内部报告：

   ```sh
   python3 scripts/payroll_readiness.py check \
     --attendance <workdir>/attendance.json \
     --matches <workdir>/person-matches.json \
     --month YYYY-MM \
     --response <workdir>/historical-salary-readiness-response.json \
     --output <project-root>/.attendance-work/payroll-readiness.json
   ```

   必须逐项核对月份、人员 ID 集合、人数、状态和计数。`COVERED` 必须有不晚于目标月的 `source_month`；其他状态不能携带来源月份。任何错误都失败关闭。报告状态检查会重新核对逐人明细、统计、身份映射和归一化响应摘要；原始 MCP 响应 SHA 只保留为原始文件的追溯摘要，因原始响应不随报告保存，状态检查不会重新验证其来源。归一化内容摘要用于发现报告内部未同步修改，不提供来源真实性或防恶意篡改保证。`COVERAGE_CHECKED` 仅表示本次历史覆盖响应结构完整且与本地范围一致，不代表用户授权、正式 Precheck 通过或远端数据当前仍未变化。报告不是提交回执或 Precheck Snapshot。输入损坏、报告无效或不可用状态均必须停止；若不可用状态无法写入，调用方仍须停止，不能沿用旧报告。输入数据变化后运行 `status` 会返回过期。每次正式提交前都必须重新调用 MCP 并覆盖该报告，因为远端历史工资导入不会改变本地 SHA；不得仅凭已有报告复用覆盖结论。

   检查成功时 CLI 还会给出 `default_calculable_scope`：仅取身份状态为 `matched` 且工资历史为 `COVERED` 的记录，并列出稳定 `record_id`、精确 Payroll Person ID、考勤摘要及映射摘要。当前 Review 的版本号必须从已验证 Review 证据带入范围证据；范围与考勤摘要或身份映射摘要任一不一致都需重新检查。此范围只是候选范围，不是用户认可、提交授权或核算授权。`MISSING_HISTORY`、`AMBIGUOUS_HISTORY`、`UNKNOWN` 和身份未关联人员均保留在排除清单及原因中。身份冲突不得自动排除；如用户明确决定某条歧义记录本次暂不纳入，按上文创建独立绑定证据，并在范围展示中列明人员和原因。其他 unresolved identity 仍阻断。结果包含重复姓名时以 `record_id` 和 Payroll Person ID 区分，不能按姓名去重。

   遇到 MCP 明确不可用时记录阻断状态：

   ```sh
   python3 scripts/payroll_readiness.py unavailable \
     --attendance <workdir>/attendance.json \
     --matches <workdir>/person-matches.json \
     --month YYYY-MM \
     --reason <内部错误类别> \
     --output <project-root>/.attendance-work/payroll-readiness.json
   ```

   `UNAVAILABLE` 永远不是部分通过。`READINESS_STALE` 表示报告与当前考勤、映射或人员范围不一致；`READINESS_REPORT_INVALID` 表示报告结构或内部数据无法通过校验；`READINESS_CHECK_BLOCKED` 表示当前输入或检查过程无法安全验证。失败状态写入本身失败时，CLI 返回错误且不得继续；文件系统故障可能使旧文件仍留在磁盘，因此调用方必须以本次命令成功结果为前提，绝不能在写入失败后读取旧报告继续流程。

4. 在向用户提供缩围选项前，按 W03.5.1 从当前 Runtime 上下文派生固定 Batch/Operation ID，并用 `get_attendance_submission` 核对既有提交回执及其月份、内容和完整人员范围。新 Task ID 或单条 Operation 查询无记录都不能证明不存在其他相关批次；若不能确认这是安全的首次处理，停止，不提供“仅处理已覆盖人员”的可执行路径。若已有提交回执，原批次人员范围已固定，不得缩围、扩围或换 Batch ID。
5. 用一次简洁摘要展示考勤人数、身份可靠匹配人数、历史覆盖人数、默认候选处理范围，以及身份待核/缺少历史/歧义/未知人员及排除原因。不得在聊天回复中重发完整考勤表。`COVERED` 只代表找到历史工资来源，不代表完整 Payroll Precheck 通过。未知或歧义人员不得算作覆盖。身份冲突必须保持待核；用户明确排除某条记录时，显示其身份待核和本次排除原因，按独立排除证据绑定当前考勤及身份摘要。首次处理且批次检查未发现冲突时，可将 `default_calculable_scope` 作为范围审阅的默认建议；它由当前月份、Review 版本、考勤 SHA、身份映射 SHA、`record_id` 和精确 Payroll Person ID 绑定。F3.2 正常路径将范围展示、正式考勤提交授权和工资核算授权收敛为一次授权：展示唯一范围、所有排除原因及后续正式副作用后，等待用户明确开始工资核算。授权之后仍先复查覆盖及批次，再按既有顺序提交、正式 Precheck、核算；不得自动缩围。范围或证据变化时重新检查、展示并取得新授权。

面客说明使用业务语言：`COVERED` 表示“已找到历史工资来源”；`MISSING_HISTORY` 表示“未找到符合条件的历史工资”；`AMBIGUOUS_HISTORY` 表示“历史工资记录存在歧义”；`UNKNOWN` 表示“暂时无法核实”；未匹配人员单独列为“身份待核实”。不向用户展示内部状态码、SHA 或 Batch ID。

P03 MCP 运行服务可能尚未部署或未注册该工具（该工具依赖 Payroll 身份服务配置）。工具不可用时不能继续提交。覆盖查询是只读能力；本地脚本只校验 MCP 返回内容，不直连 Payroll 数据库。

## 四个独立完成阶段

1. **考勤识别完成**：考勤事实已核对，正式 `考勤确认表.md` 已生成并成功声明。
2. **身份匹配完成**：人员与 Payroll 现有人档建立了可靠关联；待核实或暂不纳入记录仍单独列明。
3. **考勤提交完成**：用户明确授权的人员范围已通过校验，`submit_attendance` 返回成功回执。身份匹配成功或用户确认考勤本身不代表提交成功。
4. **工资核算完成**：当前有效的单次执行授权（或兼容的旧独立核算授权）绑定精确范围；正式提交成功、同范围 Precheck `valid=true`、`calculate_payroll` 返回成功，且工资核对文件实际生成并成功声明。考勤提交成功不代表工资已核算。

历史工资覆盖检查必须先于正式考勤提交；正式提交前重新查询当前覆盖结果及已有批次。提交成功后仍必须执行正式 Payroll `precheck_payroll`，且只有 `valid=true` 才表示完整范围通过。不能用 `can_calculate=true`、全部 `COVERED` 或任何就绪报告代替正式 Precheck。F3.2 的单次授权只在提交成功、范围未变且 Precheck `valid=true` 时继续授权同一范围的工资核算；不能授权其他月份、版本、人员集合、批次或新范围。

不得把文件生成、Artifact 声明、考勤提交或核算批次的任一单独事实描述成其他阶段完成。Payroll 提交和增量计算会持久化正式业务记录，不是 Dry-run，也不能描述为无写入的模拟试算。

## 人员范围与授权

- 未匹配人员面客说明为“当前 Payroll 未找到可关联的人员档案，本次暂不纳入自动核算范围”，不提示创建或补建人员。
- 部分人员未匹配时，不必永久阻断其他人员；但必须先向用户列明本次拟处理人数、暂不纳入人数及具体人员/原因，并将唯一范围与当前月份、考勤和身份摘要绑定。不能因用户同意处理已关联人员，就把未匹配者视为已处理或授权其他范围。
- 正常 F3.2 路径中，只有用户在范围展示之后明确表示按该范围开始工资核算，且展示已说明将正式提交考勤、执行正式 Precheck、再核算，才可调用 `PAYROLL_EXECUTION`。该动作允许依次执行两个有写入副作用的操作；考勤确认、匹配完成或覆盖通过都不够。证据绑定当前月份、考勤版本及 SHA、身份映射 SHA、规范化覆盖响应 SHA、精确 `record_ids`、Payroll Person ID 集合和固定 Batch ID。范围不唯一、用户意图不明确、证据变化或批次冲突时停止。
- 核算期间不得扩大到授权范围之外的人员。重新查询覆盖后若规范化响应摘要改变，或考勤/映射、月份、人员范围、批次发生变化，旧授权失效；不得以新 Batch/Operation ID 绕过。
- Precheck 必须针对相同月份、批次和用户认可的完整人员范围。若 `valid` 不是 `true`，说明具体阻塞人员和原因并停止；不得仅因 `can_calculate=true` 就忽略范围内的异常。需要缩小范围时先重新向用户说明并取得该新范围的明确认可，再用新范围执行。

## 可信批次上下文与写入阻断

- Lework Runtime 的 `buildWorkspaceContext()` 在当前请求含项目、Task 和 Request 上下文时，会呈现项目仓库根目录及本轮 Turn 临时目录；临时目录由 `ResolveTaskWorkspace()` 按 `<repo>/.leros/tasks/<task_id>/turns/<request_id>/tmp` 构造。Task ID 没有独立文本字段，但位于固定路径段，跨 Turn 时 Task 路径段稳定、Request 段变化。路径派生是当前上下文的结构性证据，不是密码学认证。
- 进入批次流程后，只把本轮工作区上下文中给出的项目根目录及 Turn 临时目录传给 `scripts/task_context.py`，不得从用户文本、项目 ID、Turn/Request ID、目录扫描、最近任务或随机值取得 Task ID；旧任务的 Batch ID 不得自动继承。路径不完整、仓库不匹配、结构错误、符号链接或 ID 格式错误时，停止相关流程；不要要求用户查找 `.leros` 目录，也不要询问用户填写 Task ID 或 Batch ID。
- 发现 Task ID：

  ```sh
  python3 scripts/task_context.py discover --repo-root "<Runtime 项目根目录>" --turn-tmp-dir "<Runtime 本轮临时目录>"
  ```

  仅当输出 `ok=true` 且 `source=runtime_turn_tmp_path` 时，才可将返回的 Task ID 用于当前任务。输出 `confidence=context_derived` 说明其为路径结构推导值；`is_authorization_proof=false` 始终成立。
- 派生本月稳定批次上下文时，考勤和身份映射摘要必须来自当前已确认工作文件，人员 ID 必须是本次完整且已确认的 Payroll 人员范围：

  ```sh
  python3 scripts/task_context.py batch-id \
    --repo-root "<Runtime 项目根目录>" \
    --turn-tmp-dir "<Runtime 本轮临时目录>" \
    --month YYYY-MM \
    --attendance-sha256 <当前考勤文件SHA-256> \
    --identity-sha256 <当前身份映射SHA-256> \
    --payroll-person-ids '[1,2,3]'
  ```

  `batch_id` 固定为 `<task_id>-<YYYY-MM>`；业务操作 ID 固定为 `submit:<batch_id>`、`precheck:<batch_id>` 和 `calculate:<batch_id>`。脚本按 Payroll DTO 约束验证 ID 字符及包含操作前缀后的 100 字符长度上限，并返回规范化人员范围及请求范围摘要。相同 Task/月得到相同 Batch ID；人员范围、考勤版本或身份映射变化会改变范围摘要，但绝不追加人数、随机数或时间后缀生成新批次。不得通过新 Batch ID 或 Operation ID 绕过冲突。
- 若当前可用的已确认请求上下文中保存了上一范围摘要，可附加 `--expected-scope-sha256 <摘要>`；不匹配时脚本以 `BATCH_SCOPE_CHANGED` 失败。此摘要不会跨 Turn 自动持久化；若不存在可核实的上一范围或 Payroll 回执，不得靠 Agent 记忆猜测是否可复用，按后文只读回执核对及失败关闭规则处理。
- 同一任务/月再次进入时，重算并复用相同基础 Batch ID；变更范围或版本不能拿同一个操作 ID提交不同请求。若已有 Payroll 回执或计算结果，先用对应只读查询核对月份、Batch ID、完整人员范围、内容/版本摘要及 Precheck 绑定；不一致、状态不明或无法检查跨批次影响时停止。现有只读接口不能完整检查其他批次，故即使用户重新授权，也不能据此推断新批次安全，不得换 ID 绕过冲突。
- 派生器只计算上下文和标识，不访问 Payroll、不提交、不预检、不核算，也不保存本地批次状态。输出的 `scope_sha256` 仅用于比较本次请求内容；它不是授权、Payroll 回执或服务端事实。Task ID 发现成功不触发任何 Payroll 操作。用户授权必须仍来自当前对话中对确切月份、考勤版本和完整人员范围的明确表达。
- 现有 `get_attendance_submission` 和 `get_payroll_calculation` 按 `operation_id` 读取单条回执，不提供按月份和人员范围查询其他批次的完整能力。因而，人员范围、考勤版本、身份映射或月份变化时，先说明变化并重新取得对应业务授权；若不能可靠核实其他批次及已计算结果的影响，停止正式写入，不得通过新 Batch ID 或 Operation ID 绕过冲突。
- Precheck 阻塞不授权自动排除人员。不得自行缩小或扩展范围；逐项说明变化原因并等待用户明确批准准确范围。不得自行把 39 人改成 31 人。即使获得新范围授权，如果无法确认跨批次影响仍须停止。
- 当前任务上下文不可用是内部执行环境阻塞，不是要求业务用户选择恢复方案；向用户只说明暂不能安全启动正式提交/核算，并继续允许不依赖 Batch ID 的考勤核对和只读身份匹配。

进入任何 Payroll 批次操作时按此顺序处理：读取本轮 Runtime 路径并发现 Task ID；用当前月份和已确认工作文件摘要派生稳定 ID；先按固定 `operation_id` 查询已有回执/结果；核对查询结果、用户明确授权和完整人员范围；只有查询证明可安全继续且授权覆盖当前精确请求时，才调用对应正式接口。上下文发现本身不触发 Payroll 调用；缺少当前授权、操作结果不明、范围摘要冲突或跨批次影响无法核实时均停止。

## 重复调用与结果不确定

Payroll 当前提交契约要求 `submission_mode=BATCH_SNAPSHOT`，并把 `operation_id` 固定为 `submit:<batch_id>`；范围型 Precheck 使用 `precheck:<batch_id>`；核算要求精确的 `payroll_person_ids[]`、当前 `precheck_snapshot_id`，且 `operation_id` 为 `calculate:<batch_id>`。同一 `operation_id` 绑定不同批次内容/月/人员范围或 Precheck 快照时会冲突。只有当前 Task 上下文、用户授权范围和现有操作回执均已核实，才可执行对应正式调用；修改版本或范围不能自动视作安全的新批次。不要把这些标识展示给业务用户。

如果写接口超时、断线、返回错误但是否写入不明确，**不得立即重试写操作**。先调用 Payroll 只读查询：考勤提交使用 `get_attendance_submission` 按 `operation_id` 查回执；工资核算使用 `get_payroll_calculation` 按 `operation_id` 查结果。核对月份、批次、人员范围、内容摘要及当前 Precheck 绑定后，才能判定原操作结果。若只读查询不可用、找不到记录但无法证明原调用未落库，或读回数据与本次请求不一致，停止并向用户说明结果待核实；不得另造操作 ID 绕过冲突。

明确读回原操作已成功时，沿用原结果，不再提交或核算。只有可靠查询明确证明操作未持久化，且可使用完全相同的批次和请求时，才考虑再次执行；不允许重复创建新批次来掩盖未知结果。

## 结果交付

只有 Payroll 明确返回成功，且工资结果文件由当前已配置的正式生成能力实际生成后，才可声明该工资表。不得自行重算工资金额、生成空白表或把 Payroll 响应臆造为工资表。使用 `artifact_declare` 声明真实文件并检查 `ok=true`。只有在成功核算和文件声明均得到验证后，才能告知“工资核算已完成”；否则准确说明当前停在哪个阶段。

### 五 Sheet 工资文件导出

先通过阶段准入并核对当前成功核算、提交及正式 Precheck 回执，再保留只读来源文件：Payroll `get_payroll_calculation` 返回的原始结果 DTO、实际提交的 `BATCH_SNAPSHOT` 请求 JSON、当前 `attendance.json`、当前 `person-matches.json` 和工作簿输入 `payroll-workbook-input.json`。其中人员映射须以当前考勤摘要为绑定。不得重建或编辑 Payroll 回执、重造提交请求或静态录入工资数据；任一原始来源不可用即停止。

```sh
python3 scripts/build_payroll_workbook.py \
  --input <workdir>/payroll-workbook-input.json \
  --gate-request <workdir>/payroll-export-gate-request.json \
  --calculation-response <workdir>/get-payroll-calculation-response.json \
  --person-matches <workdir>/person-matches.json \
  --submission-request <workdir>/submit-attendance-request.json \
  --output <project-root>/2026年6月工资核算表.xlsx
```

输出目标必须是新的 `.xlsx` 路径，脚本不会覆盖已有文件。`--gate-request` 是当前阶段请求证据，脚本会再次调用本地 `workflow_gate.py` 的 `PAYROLL_EXPORT` 判定；其中身份、提交范围、提交回执、正式 Precheck 与核算回执的每个人员条目都必须包含 `record_id`、姓名和精确 `payroll_person_id`，并与核算结果按人员 ID 一一对应，不能仅按姓名关联。Payroll 源码当前确认成功响应为 `PayrollCalculationResult` DTO；MCP `NewToolResultJSON` 同时返回 `structuredContent` 和包含同一 DTO JSON 的单个 text `content`。导出器可读取直接 DTO，或校验此 MCP 包装后提取 DTO；两份载荷必须相同，原始响应文件保持不变。Go nil `issues` 切片可序列化为 `null`，仅对此字段按空问题列表规范化；关键 `items`/工资分项数组为 `null` 时阻断。`source_amount:null` 仅在 `source_present=false` 时合法，零金额仍是明确金额。DTO 的 `omitempty` 可选字段不会被补造，正式导出仍要求所需批次、操作、Precheck 快照及考勤绑定信息存在。Payroll 当前源码没有证实其他字段别名，故未知别名、外层包装或字段一律拒绝。导出器对标准化 DTO 与 `calculation_result` 做键顺序/空白无关的规范化结构比较，覆盖整体状态、计数、问题、所有人员、工资分项、来源状态与金额及汇总。

考勤核验要求快照中每条记录带有稳定 `record_id`，与当前 `attendance.json` 和当前 `person-matches.json` 全量一一对应；核验考勤摘要、月份、人员/Payroll ID 关系、逐日状态、`value`、原始标记、印刷/派生/确认出勤、请假旷工统计及备注。提交请求的批次、月份、模式、操作 ID、人员集合须与回执、核算和快照吻合，提交人员记录须与快照逐字段一致。Payroll 日状态只显式映射 `work`、`personal_leave`、`sick_leave`、`absent`、`rest`、`comp_leave`；其他考勤状态没有经过证明的一对一 Payroll 状态时阻断，不猜测映射。对于**未纳入核算且未进入正式提交请求**的人员，若来源日状态没有 Payroll 映射，工作簿输入快照的该日 `status` 必须为 JSON `null`。这只是本地导出表示“没有可证明的 Payroll 状态”，不是 Payroll 的日状态，也不得作为 `BATCH_SNAPSHOT` 提交；导出器会以当前 `attendance.json` 中的状态、原始标记和值核对它，任何非 null 状态都拒绝。已核算人员仍必须具有合法且与来源一致的 Payroll 状态。每日 `value` 只使用当前来源中实际存在的字段，缺失按 DTO 的 null 表示；与原始标记不同的用户确认标记无法在现有 DTO 中同时表达时阻断。人员范围之外的考勤人员仍须保留在完整快照和工作簿中。

Payroll 当前 Go 实现的 `payload_sha256` 是对 `BATCH_SNAPSHOT` 的 `{attendance_month,batch_id,submission_mode,records}` DTO 子集执行 `json.Marshal` 后计算；`operation_id` 不在摘要载荷中。Skill 不自行重造 Go 序列化器来复算该摘要，导出器只核验摘要格式、回执的批次/范围，并将提供的原始提交请求逐字段与快照及当前事实对齐。原始请求、回执和响应仍由 Agent/运行环境提供，因此本地一致性检查不提供来源真实性证明。

脚本只校验并展示输入，不连接 Payroll、不调用 MCP、不补算工资。仅完整 `SUCCESS`、有效 Precheck、匹配的固定批次及 `PAYROLL_EXPORT` 阶段准入可导出正式文件；`PARTIAL`、`FAILED`、损坏/缺失回执、来源内容不一致或**已核算人员**状态无法映射均失败关闭，不能改走内部或手工工资表路径。五个 Sheet 按已审计模板顺序生成，金额直接来自原始 Payroll 核算响应；完整考勤快照单独保留，未纳入核算人员及有证据的异常进入“人工复核事项”。“考勤汇总”按稳定 `record_id` 每人一行，日期列按目标月份动态展开；单元格以状态、值和原始标记的复合文本展示，日期由列标题确定，月度字段不再按天重复。逐日记录仍在来源快照中逐字段校验，未纳入核算的人员也保留。“人工复核事项”按稳定人员标识与问题类别聚合，说明中保留各项具体问题及可追溯人员 ID；不同类别、不同人员不合并。工资基准始终保留九项的原始来源状态和来源金额：来源明确存在且为零不产生来源缺失告警；来源明确不存在、金额为空且计算为零只在工资基准展示该来源事实；来源不存在但计算非零会列为来源/计算不一致；来源未知必须待核，不能按正常零金额处理。Payroll 当前核算 DTO 要求 `source_present` 为布尔值，若原始 DTO 给出 null/缺字段，仍按 DTO 校验失败关闭，不将其改写成布尔值或零金额。

脚本成功返回 `ok=true` 和输出路径后，确认文件存在，再调用 `artifact_declare` 声明该新文件并检查 `ok=true`。不得在完整 SUCCESS 后另行询问是否导出。若导出失败，应报告“工资核算已完成，但正式工资表导出失败”；若文件已生成但声明失败，应说明文件生成成功、交付未完成。任何导出/声明重试都只重试对应步骤，不得再次调用 `calculate_payroll`；已有成功核算结果先按原 `operation_id` 只读核实并复用。PARTIAL、FAILED、结果不明、回执缺失、范围漂移、DTO 或来源绑定校验失败时停止，不得绕过导出器。Artifact `ok=true` 证明声明成功，不证明 UI 已显示。阶段门禁是 Skill 正常路径约束，不是可阻止任意 Shell 或 MCP 调用的 Runtime 权限边界。

身份和范围的自然语言展示使用“已关联 / 待核实 / 暂不纳入”，不向业务用户展示内部枚举或要求其选择恢复策略。
# Payroll 处理与执行边界

始终使用当前 Runtime 注册的 Payroll MCP 工具。禁止自行编写 HTTP/MCP 客户端、直接请求 MCP URL、读取 Runtime 配置或凭据环境变量，也不得把 Bearer Token 写入脚本、命令行、JSON、日志、异常、Artifact 或聊天。工具缺失/不可用时停止并报告，不以临时客户端替代。该约束只规定 Skill 正常路径；它不是 Runtime 权限边界，也不证明 MCP 凭据对 Agent 进程不可见。

正常两次业务确认：第一次在同一展示中核对考勤事实与所有项目映射，用户明确确认后不再分别追问考勤或项目关系；第二次在唯一工资范围、纳入人员、排除人员及正式操作顺序完整展示后，用户明确授权按该范围核算。已明确展示的排除事项无需用户再次复述，也不要求重确认整份考勤。若考勤事实、项目绑定、身份或纳入范围实质变化，重新展示受影响内容并取得新的相应确认；既有排除人员维持原展示决定本身不构成范围变化。
