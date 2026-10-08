# 手动比较报告 v1

E02 比较报告与 [原生记录交换 v1](../exchange/README.md) 分开版本化。JSON 的 `format=jeval-comparison`、`formatVersion=1`；不是可再导入的记录包，现有 records.import 会拒绝。字段类型以 [TypeScript 契约](../index.ts) 和 [Go 模型](../../engine/internal/model/comparison.go) 为准。

- `contentScope=normalized-preview`、`sourceFilesIncluded=false`、`fullContentIncluded=false`。`generatedAt` 为报告生成的 UTC 时间，不是原轨迹发生时间。
- `left` / `right` 各含 `record`（一个已保存快照的原生 record 对象）及 `annotations`（导出时已保存、未删除的人工标注）。每侧 run/importInfo 固定来源与 snapshotId，允许两侧引用同一快照。当前指针更新不影响明确选定的旧版本，源文件离线仍可导出。
- 每侧包含全部事件预览、来源定位、原始行号、父关系和未知字段值；搜索、事件类型和页码不限制输出。完整标准化正文、原始文件、附件、未保存草稿、模型诊断与自动对齐关系均不包含。路径和预览没有自动脱敏。
- 标注沿用 [协议定义](../protocol/README.md) 的身份、判断、备注、修订号和写入时间。`eventId=null` 表示快照标注，否则必须引用该侧 record.events 中的事件；报告中 `event=null`，避免重复正文，按 eventId 查找对应证据。`deleted=false`，删除标记不导出。修订号不是完整编辑审计。
- `metrics` 含 durationMs、tokens、eventCount，每项 `{left,right,delta}`。delta 为右减左；只在两侧原值都已知时计算，已知零保持零。未知原值和不可计算差值为 null，不从步骤数量推测时间或用量，不据差值宣称因果收益。编码时从已校验记录重算指标。

两份快照与人工标注来自同一个 SQLite 读取事务，不读原来源；标注在该次读取之后的修改不会进入报告。JSON 和 Markdown 各有 64 MiB 文件上限，超过则拒绝并保留旧目标；两个各自合法的快照组合仍可能超出报告限制。Markdown 使用动态长度文本围栏承载来源内容与人工备注，缺失指标标记 unknown (null)。

入口为 [comparisons.export](../protocol/README.md)，返回成功前完成文件写入。保存选择器取消不写文件；路径保护和同目录临时写入/替换复用单记录导出，拒绝资料库、旁路文件、已登记来源、链接和特殊目标。没有写入过程取消；30 秒桌面请求超时不等于底层取消，也不自动重放。文件替换的跨平台/断电限制沿用记录交换。

实现与实际检查见 [M3 进度](../../docs/development/m3-progress.md)，端到端用例见 [比较集成](../../tests/integration/comparison.test.ts) 和 [桌面对比](../../tests/e2e/comparison.spec.ts)。本格式没有跨 harness 兼容、自动匹配、受控实验或 Jev 质量结论。
