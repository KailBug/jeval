# 桌面与引擎协议 v1

传输为 UTF-8 JSON Lines：每条消息以 LF 结束。stdout 只传协议帧，stderr 写诊断；请求和响应均小于 1 MiB，超大输入关闭连接。引擎按输入顺序处理请求，目录扫描在后台进行，查询与扫描发布快照共用互斥锁；桌面按 ID 匹配响应，允许多个未完成请求。扫描进度由状态请求轮询，无主动通知帧。

请求：

```json
{"type":"request","version":1,"id":"1","method":"hello","params":{}}
```

响应只包含 `result` 或 `error` 中的一个字段：

```json
{"type":"response","version":1,"id":"1","result":{"engineVersion":"0.1.0-dev.0","protocolVersion":1,"recordVersion":1,"capabilities":["demo","runs.list","runs.events","codex.import","codex.scan.start","codex.scan.status","codex.scan.cancel","codex.scan.candidates","codex.scan.import"]}}
```

```json
{"type":"response","version":1,"id":"2","error":{"code":"NOT_FOUND","message":"Run not found"}}
```

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `hello` | `{}` | 引擎、协议、记录版本与能力列表 |
| `runs.list` | `search?`, `status?`, `source?`, `offset?`, `limit?` | 匹配的运行分页；source 支持 all/demo/codex，缺省为 all |
| `runs.events` | `runId`, `search?`, `kind?`, `offset?`, `limit?` | 单条运行内筛选后的事件分页，保留原始序号与证据 |
| `codex.import` | `path`（绝对路径） | `{ run, replaced }`；只读解析成功后按路径替换内存记录，失败不改变旧记录 |
| `codex.scan.start` | `path`（绝对目录路径） | `ScanStatus`；创建后台发现任务，立即返回 discovery/running；不导入 |
| `codex.scan.status` | `id`（扫描 ID） | 最新 `ScanStatus`；只保留最近一次扫描 |
| `codex.scan.cancel` | `id`（扫描 ID） | `ScanStatus`；活动任务进入 cancelling，终态任务原样返回 |
| `codex.scan.candidates` | `id`, `offset?`, `limit?` | `Page<ScanCandidate>`；最近扫描的候选摘要，不发布记录 |
| `codex.scan.import` | `id`, `ids`（非空、无重复的候选 ID 数组） | `ScanStatus`；验证集合与整体配额后启动所选导入，返回 import/running |
| `shutdown` | `{}` | `{ "ok": true }`，随后退出 |

分页返回 `{ items, total, nextOffset }`。`offset` 从 0 开始，最大 10 亿；默认 `limit=50`，范围 1–100。页内元素的 JSON 编码预算为 512 KiB，必要时提前结束该页；调用方必须使用 nextOffset，不能把请求 limit 当作实际返回数。到达末页时 `nextOffset=null`，空结果 `items=[]`。搜索匹配标题、项目和来源，忽略英文大小写。状态支持 `all`、`completed`、`failed`、`unknown`。

事件搜索匹配标题、正文预览与角色，去掉查询两端空白、忽略英文大小写。`kind` 缺省或 all 表示全部，支持 message/tool_call/tool_result/verification/lifecycle/error，未知值拒绝。先筛选整个快照，再应用分页；total 表示匹配总数，ID、sequence、parentId 与 evidence 不重新编号。未保存的源正文截断部分不参与搜索。

`ScanStatus` / `ScanCandidate` 字段以 [TypeScript 定义](../index.ts) 为准。状态中的 `phase` 为 discovery/import；`visited` 为检查的条目数（不含根），`discovered` 为尝试读取的 JSONL 数，`ready` 为有效候选数。`imported/updated/failed/issues` 为当前阶段的新增/替换/失败统计与前 30 项错误，启动所选导入时清零；发现阶段 imported/updated 恒为 0。`skipped` 记录链接/特殊文件数。错误路径最多 2048 个字符，消息最多 512 个字符，可附省略号，失败总数不截断。

候选提供标题、项目、文件路径、时间、事件数、解析提示数与最多 500 字符的首条消息预览。`existing` 表示任务库已有同身份记录；`imported` 表示本次候选已成功确认导入，再次选择它会被拒绝。`error` 保存该候选最近一次导入错误。SHA-256 和授权路径由引擎保存，渲染层只提交 ID，不能覆盖候选的路径或摘要。

发现状态：`running → completed/limited`，limited 为遍历上限。发现终态可启动所选导入，使用同一扫描 ID 和 `phase=import`，状态为 `running → completed`；任一阶段取消为 `running → cancelling → cancelled`，终态取消幂等。发现不会修改任务库；所选导入先检查 20 个文件/50000 个事件配额，超额整批拒绝。导入重读并核对身份/摘要，变化或失败仅拒绝该项、保留旧快照；成功项按路径替换，取消后不再发布，已经完成的所选项保留。新扫描替换旧候选与状态，旧 ID 或重启前 ID 返回 NOT_FOUND；shutdown/输入结束取消并等待后台退出。

错误码：`PARSE_ERROR`、`INVALID_REQUEST`、`PROTOCOL_MISMATCH`、`METHOD_NOT_FOUND`、`INVALID_PARAMS`、`NOT_FOUND`、`IMPORT_FAILED`（输入或读取失败）、`IMPORT_LIMIT`（本次启动的记录上限）、`SCAN_FAILED`（根目录/启动失败）、`SCAN_BUSY`（已有活动扫描，拒绝另一个扫描或单文件导入）。无法解析的请求返回空 ID，其后的合法请求仍可处理。请求 ID 是不超过 128 字节的非空字符串。导入与扫描上限见 [Codex 适配器](../../docs/adapters/codex.md)。

桌面请求默认超时 5 秒，单文件导入为 30 秒，最多 128 个未完成请求。文件/目录选择与重启互斥，活动扫描期间拒绝新的导入/扫描，但可重启引擎取消任务。超时不是取消；不自动重放导入或扫描启动。状态默认 300ms 轮询，失败后 1500ms 重试；错误可见且保留最后已知状态。连接损坏、子进程退出或输入管道错误时拒绝所有等待中的请求；界面提供显式重启。重启清除所有内存导入记录。关闭时发送 shutdown，最多等候 1.5 秒后终止子进程。

Electron 渲染层只能访问 `hello/listRuns/listEvents/restartEngine/importCodex/scanCodex/scanStatus/cancelScan/scanCandidates/importScanSelection` 业务方法，以及只读 platform 字符串，不提供任意 IPC、路径访问或执行命令能力。`importCodex()` 和 `scanCodex()` 无参数，由主进程选择器提供路径，取消选择返回 null。候选/导入/状态/取消仅接受主进程持有的扫描 ID，候选集合由 Go 再校验。主进程检查调用窗口与顶层 frame URL，启用 context isolation、sandbox 和 CSP，拒绝新窗口与页面跳转。依据：[Electron 安全指南](https://www.electronjs.org/docs/latest/tutorial/security)、[electron-vite 构建文档](https://electron-vite.org/guide/build)。
