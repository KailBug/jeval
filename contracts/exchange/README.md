# 原生记录交换 v1

此契约用于 C 切片的单条 Codex 标准化快照 JSON 导出/再导入。可读 Markdown 是报告，不是导入格式。字段对应 [统一记录](../record/README.md)；实现为 [Go exchange 包](../../engine/internal/exchange/exchange.go)，跨语言合成样例为 [preview-v1.json](preview-v1.json)。

## 包与内容范围

JSON 顶层只能包含以下字段，均为必填：

```json
{
  "format": "jeval-record",
  "formatVersion": 1,
  "contentScope": "normalized-preview",
  "maxContentBytes": 8192,
  "sourceFilesIncluded": false,
  "record": { "schemaVersion": 1, "runs": [], "events": [] }
}
```

示意中的 `runs` 实际必须含一条非演示 Codex 运行，`events` 是该快照全部已保存事件，而非当前筛选/分页结果。`maxContentBytes` 是单事件保留正文的 UTF-8 字节预算。为保留现有 `codex-rollout-v1` 记录及身份，现有适配器追加的固定后缀 `\n[内容预览已截断，请按来源行号查看原文件]` 不计入 8192 字节；仅该精确后缀可超出正文预算，交换时不重新截断或标准化。其他正文超过预算则拒绝。

包不含原始 JSONL 字节、完整正文或附件。`importInfo.sha256` 仍是原来源字节摘要，不能验证包未被修改，也不是包内备份摘要。快照 ID 是来源 ID、该摘要和适配规则的身份，不提供签名或真实性证明。导入不读取、扫描或写入 `file/location` 指向的来源，不因为它们看似存在而获得更新授权。导入后任务库的只读权限是本地派生元数据；`readOnly` 不属于交换字段，导出不包含它，导入携带此字段即拒绝。

## 严格校验与边界

- 仅接收 UTF-8 普通文件，最多 64 MiB，JSON 仅一个完整值；不接受重复对象键、未知字段、字段大小写变体、缺失必填字段或 JSON 嵌套深度超过 32。拒绝不成对的 Unicode surrogate 转义，避免 Go JSON 解码替换内容；有效 surrogate 对正常解码。未来 `formatVersion/schemaVersion/adapterVersion` 明确拒绝，不尝试降级解析。
- 运行 `source=Codex`、`demo=false`、`adapterVersion=codex-rollout-v1`，historyMode 为 classic 或 paginated，status 为 completed/failed/unknown。`startedAt/durationMs/tokens` 和事件 `timestamp/parentId` 必须存在，未知保持 `null`；其余必填字段不能为 `null`。指标为非负、精确的安全 JavaScript 整数，禁止用 0 代替未知。
- 时间戳必须为带时区的 RFC 3339 字符串，`null` 表示未知。非整数、负数或超过 `9007199254740991` 的数值拒绝；事件计数额外限制为 0–5000 并严格等于数组长度。
- 来源 ID 为 `codex-` 加 64 位小写十六进制，来源摘要也是 64 位小写十六进制。snapshotId 按 [记录身份公式](../record/README.md#来源快照与永久引用) 重算后必须完全一致；不会根据当前机器上的路径重新生成来源 ID。
- 事件 ID 必须为 `run.id:物理行号`，不可重复；sequence 按数组顺序连续从 1 开始。runId、evidence.sourceId、snapshotId、location 都必须指向所属运行/快照/原定位。物理行号为正的安全整数。parentId 只引用包内事件，允许前向引用但拒绝自引用、环或包外引用。
- kind 为 message/tool_call/tool_result/verification/lifecycle/error；role 为 user/assistant/system/developer/tool。不猜测未知状态或关联。
- file/location 最多 2048 UTF-8 字节；sessionId、cliVersion、parentThreadId、forkedFromId 最多 1024 字节；标题、项目、提示正文分别最多 16384 字节。运行标题、项目、file、sessionId、事件标题和提示正文不得为空。warnings 最多 30 条，warningCount 不少于已列条数；所有 Run 元数据 JSON 编码后额外限制为 256 KiB，以保持有界协议响应。

JSON 导出使用固定结构、字段顺序和两空格缩进，末尾换行；相同规范化记录会得到相同字节。保留来源/运行/快照/事件 ID、关联、证据和 `null`，不附加本地目录配置或来源权限。包中没有模型诊断。

## 文件写入与 Markdown

导出先写入目标同目录的临时文件，写完、Sync、关闭后再 rename 替换目标；失败清理临时文件，替换前不会截断已有目标。已有目标必须为普通文件，拒绝目录、符号链接或其他特殊文件。此策略不声称已经验证断电恢复或所有文件系统的 rename/目录持久性；目录选择和系统对话框的验收另见开发进度。

Markdown 包含运行状态、缺失指标、来源定位、来源摘要、快照、适配版本、会话来源、提示和每个事件的物理行号/父引用。所有记录文本都放在 `text` 围栏内，围栏长度比内容最长连续反引号多一，从而将 HTML、链接、命令和伪造 Markdown 标题保留为普通文本。报告明确声明标准化预览/截断、未包含原文件、未知字段和无模型诊断。

样例为人工构造的未知状态记录、前后事件关联、中文、围栏注入文本和截断提示；文件路径、来源/会话标识及摘要均为合成值，没有私人日志，不代表真实 Codex 版本或安装验收。

## 与 D02 本地完整正文的关系

D02 另存的完整标准化文本不进入 formatVersion=1 JSON 或 Markdown。交换仍为 normalized-preview；在空白库再导入后，正文接口返回 available=false，包括未截断或为空的预览。包内不存在完整性证明，不能据此合成正文。原生导出和再导入保持既有版本/身份，不扩大访问原文件的权限；完整正文交换需要以后单独定义版本与验收。
