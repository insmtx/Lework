# 分类与来源维度

## 来源类型

公告、供应商须知、资格章节、技术规范、商务条款、评分办法、合同、响应模板、附件、补遗、澄清。

## 响应类别

仅允许：

- `qualification`
- `commercial`
- `technical`
- `format_procedure`

合同不是类别，评分不是类别。它们分别记录为 `source_type=contract`、`scoring=true` 等属性。

## 子类

子类由项目内容动态生成，但应稳定、可解释，如 `delivery_time`、`payment`、`product_function`、`security`、`training`。不得用“其他”大量吞并要求。

## 偏差表展示字段

`category` 表示要求的语义类别，不直接决定其是否进入正式偏差表。每个要求记录必须保留一个且仅一个 `source_clause`，其值为正式文件中的单一来源条款定位；不得存放并列条款、来源范围或多个条款号。

对需要偏差表响应的要求，在覆盖关系中写入：

- `deviation_display.table`：仅允许 `commercial`、`technical` 或 `excluded`；
- `deviation_display.response_clause`：该条款在响应文件中的唯一真实章节及小节定位；
- `deviation_display.deviation_note`：该条款的唯一偏差说明。

`excluded` 仅表示不进入商务或技术偏差表，不表示可以不响应；其响应必须继续落在资格、格式、程序、报价或其他对应位置。不得设置合并键、分组键或多来源展示字段。
