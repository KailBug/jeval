# 桌面与引擎协议 v1

传输为 UTF-8 JSON Lines：每条消息以 LF 结束。stdout 只传协议帧，stderr 写诊断；请求和响应均小于 1 MiB，超大输入关闭连接。引擎按输入顺序处理请求，目录扫描与已登记记录更新在后台进行，查询与扫描发布快照共用互斥锁；桌面按 ID 匹配响应，允许多个未完成请求。扫描进度由状态请求轮询，无主动通知帧。

请求：

```json
{ "type": "request", "version": 1, "id": "1", "method": "hello", "params": {} }
```

响应只包含 `result` 或 `error` 中的一个字段：

```json
{
  "type": "response",
  "version": 1,
  "id": "1",
  "result": {
    "engineVersion": "0.1.0-dev.0",
    "protocolVersion": 1,
    "recordVersion": 1,
    "capabilities": [
      "demo",
      "runs.list",
      "runs.get",
      "runs.events",
      "codex.import",
      "codex.update",
      "codex.update.start",
      "codex.update.status",
      "codex.update.cancel",
      "codex.scan.start",
      "codex.scan.status",
      "codex.scan.cancel",
      "codex.scan.candidates",
      "codex.scan.import",
      "persistent-library",
      "codex.directories.list",
      "codex.directories.remove",
      "records.export",
      "records.import"
    ]
  }
}
```

```json
{
  "type": "response",
  "version": 1,
  "id": "2",
  "error": { "code": "NOT_FOUND", "message": "Run not found" }
}
```

| 方法                       | 参数                                                             | 结果                                                                                             |
| -------------------------- | ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `hello`                    | `{}`                                                             | 引擎、协议、记录版本与能力列表                                                                   |
| `runs.list`                | `search?`, `status?`, `source?`, `offset?`, `limit?`             | 匹配的运行分页；source 支持 all/demo/codex，缺省为 all                                           |
| `runs.get`                 | `runId`                                                          | 单条 `Run` 元数据；用于跨任务分页的历史/详情选择                                                 |
| `runs.events`              | `runId`, `search?`, `kind?`, `offset?`, `limit?`                 | 单条运行内筛选后的事件分页，保留原始序号与证据                                                   |
| `codex.import`             | `path`（绝对路径）                                               | `{ run, replaced }`；持久化模式先事务提交再发布当前版本，失败保留旧快照                          |
| `codex.update`             | `runId`                                                          | `{ run, replaced, update }`；兼容同步更新，使用检查点或完整重算，失败保留旧版本                  |
| `records.export`           | `runId`, `path`（保存选择器绝对路径）, `format`（json/markdown） | `{ path, format, snapshotId, eventCount }`；持久化模式导出完整当前快照，文件写入成功才返回       |
| `records.import`           | `path`（打开选择器绝对路径）                                     | `{ run, replaced }`；持久化模式严格校验原生 JSON 并提交；新来源只读、同版本幂等                  |
| `codex.directories.list`   | `{}`                                                             | `{ items: { id, path }[] }`；持久化模式保存的目录配置                                            |
| `codex.directories.remove` | `id`                                                             | `{ ok: true }`；只移除目录配置，不删除快照或原文件                                               |
| `codex.scan.start`         | `path`（绝对目录路径）或 `directoryId`，二选一                   | `ScanStatus`；有效目录在持久化模式保存配置，创建后台发现任务并立即返回 discovery/running；不导入 |
| `codex.scan.status`        | `id`（扫描 ID）                                                  | 最新 `ScanStatus`；只保留最近一次扫描                                                            |
| `codex.scan.cancel`        | `id`（扫描 ID）                                                  | `ScanStatus`；活动任务进入 cancelling，终态任务原样返回                                          |
| `codex.scan.candidates`    | `id`, `offset?`, `limit?`                                        | `Page<ScanCandidate>`；最近扫描的候选摘要，不发布记录                                            |
| `codex.scan.import`        | `id`, `ids`（非空、无重复的候选 ID 数组）                        | `ScanStatus`；验证集合与整体配额后启动所选导入，返回 import/running                              |
| `codex.update.start`       | `runId`                                                          | `UpdateStatus`；验证已登记且可更新的来源，立即返回后台任务                                       |
| `codex.update.status`      | `id`                                                             | 最新 `UpdateStatus`；只保留最近一次更新任务                                                      |
| `codex.update.cancel`      | `id`                                                             | `UpdateStatus`；活动更新进入 cancelling，终态幂等                                                |
| `shutdown`                 | `{}`                                                             | `{ "ok": true }`，随后退出                                                                       |

`UpdateStatus` 字段以 [TypeScript 定义](../index.ts) 为准：id/runId、state（running/cancelling/completed/cancelled/failed）、phase（reading/parsing/saving/done）和有界 message；成功附 run/report。phase 为最近处理阶段，不是百分比；取消/失败可保留最后阶段。report 的 mode 为 full/incremental/unchanged，reason 为 no-checkpoint/checkpoint-invalid/source-truncated/source-rewritten/unsafe-prefix/projection-reconciliation/verified-prefix/same-bytes；verifiedBytes 是读取校验字节数，parsedLines 是送入解析器的物理行槽数（含末尾空槽），未变时为零。`codex.import` 和兼容同步 `codex.update` 额外返回 update 报告；初次导入始终完整解析。

扫描、所选导入、更新共享单个来源写入任务；重叠写入返回 SCAN_BUSY，旧终态扫描的取消不会取消更新。更新在锁外读取/解析，浏览仍读取旧版本；发布与取消用同一锁，已接受取消后不再提交，提交先完成则取消返回 completed。重启丢弃任务状态，旧 id 返回 NOT_FOUND；已提交的检查点仍可复用。读取、解析或提交失败通过 state=failed 返回，保留旧快照；启动参数/权限错误仍为错误响应，随机任务 ID 生成失败为 UPDATE_FAILED。无数据库模式使用相同状态协议但不持久化检查点，每次完整解析。桌面更新状态/取消只接受主进程持有的任务 ID。

分页返回 `{ items, total, nextOffset }`。`offset` 从 0 开始，最大 10 亿；默认 `limit=50`，范围 1–100。页内元素的 JSON 编码预算为 512 KiB，必要时提前结束该页；调用方必须使用 nextOffset，不能把请求 limit 当作实际返回数。到达末页时 `nextOffset=null`，空结果 `items=[]`。搜索匹配标题、项目和来源，忽略英文大小写。状态支持 `all`、`completed`、`failed`、`unknown`。

事件搜索匹配标题、正文预览与角色，去掉查询两端空白、忽略英文大小写。`kind` 缺省或 all 表示全部，支持 message/tool_call/tool_result/verification/lifecycle/error，未知值拒绝。先筛选整个快照，再应用分页；total 表示匹配总数，ID、sequence、parentId 与 evidence 不重新编号。未保存的源正文截断部分不参与搜索。

`ScanStatus` / `ScanCandidate` 字段以 [TypeScript 定义](../index.ts) 为准。状态中的 `phase` 为 discovery/import；`visited` 为检查的条目数（不含根），`discovered` 为尝试读取的 JSONL 数，`ready` 为有效候选数。`imported/updated/failed/issues` 为当前阶段的新增/替换/失败统计与前 30 项错误，启动所选导入时清零；发现阶段 imported/updated 恒为 0。`skipped` 记录链接/特殊文件数。错误路径最多 2048 个字符，消息最多 512 个字符，可附省略号，失败总数不截断。

候选提供标题、项目、文件路径、时间、事件数、解析提示数与最多 500 字符的首条消息预览。`existing` 表示任务库已有同身份记录；`imported` 表示本次候选已成功确认导入，再次选择它会被拒绝。`error` 保存该候选最近一次导入错误。SHA-256 和授权路径由引擎保存，渲染层只提交 ID，不能覆盖候选的路径或摘要。

按保存的 directoryId 启动扫描时，根路径解析后必须仍对应已保存的规范化路径；链接/junction 替换导致目标变化返回 SCAN_FAILED，要求重新选择，不写入新的目录配置。

发现状态：`running → completed/limited`，limited 为遍历上限。发现终态可启动所选导入，使用同一扫描 ID 和 `phase=import`，状态为 `running → completed`；任一阶段取消为 `running → cancelling → cancelled`，终态取消幂等。发现不会修改任务库；所选导入先检查 20 个文件/50000 个事件配额，超额整批拒绝。导入重读并核对身份/摘要，变化或失败仅拒绝该项、保留旧快照；成功项按路径替换，取消后不再发布，已经完成的所选项保留。新扫描替换旧候选与状态，旧 ID 或重启前 ID 返回 NOT_FOUND；shutdown/输入结束取消并等待后台退出。

错误码：`PARSE_ERROR`、`INVALID_REQUEST`、`PROTOCOL_MISMATCH`、`METHOD_NOT_FOUND`、`INVALID_PARAMS`、`NOT_FOUND`、`IMPORT_FAILED`（读取或快照提交失败）、`IMPORT_LIMIT`（任务库记录上限）、`SCAN_FAILED`（根目录/启动失败）、`SCAN_BUSY`（已有活动扫描或更新，拒绝另一个来源写入任务）、`STORAGE_FAILED`（存储查询或目录配置写入失败）。无法解析的请求返回空 ID，其后的合法请求仍可处理。请求 ID 是不超过 128 字节的非空字符串。导入与扫描上限见 [Codex 适配器](../../docs/adapters/codex.md)。

桌面请求默认超时 5 秒，单文件导入为 30 秒，后台更新启动/状态/取消为 5 秒，最多 128 个未完成请求。文件/目录选择与重启互斥，活动扫描或更新期间拒绝新的导入/扫描/更新，但可重启引擎取消任务。超时不是取消；不自动重放导入或扫描启动。扫描状态默认 300ms、更新状态 200ms 轮询，失败后 1500ms 重试；错误可见且保留最后已知状态。连接损坏、子进程退出或输入管道错误时拒绝所有等待中的请求；界面提供显式重启。引擎重启恢复已提交快照和目录配置，清除扫描候选和更新任务状态，界面保留当前选择；应用退出清除内存浏览历史。关闭时发送 shutdown，最多等候 1.5 秒后终止子进程。

Electron 渲染层业务方法以 [DesktopAPI](../index.ts) 为准，另暴露只读 platform 字符串，不提供任意 IPC、路径访问或执行命令能力。`importCodex()` 无路径参数；`scanCodex()` 无参数时由主进程选择器提供路径，或仅提交保存的目录 ID；取消选择返回 null。`updateCodex` 只提交已登记运行 ID。候选/导入/状态/取消仅接受主进程持有的扫描 ID，候选集合由 Go 再校验。主进程检查调用窗口与顶层 frame URL，启用 context isolation、sandbox 和 CSP，拒绝新窗口与页面跳转。依据：[Electron 安全指南](https://www.electronjs.org/docs/latest/tutorial/security)、[electron-vite 构建文档](https://electron-vite.org/guide/build)。

C 的 records.import / records.export 仅在持久化模式提供并列入 hello 能力。原生格式和上限见 [交换 v1](../exchange/README.md)，大包正文通过文件读取/写入而非协议帧传输。导出不接受事件筛选，快照版本与全部事件来自同一次当前快照读取，不访问原来源。路径参数由桌面主进程选择器取得，渲染层仅调用 importRecord() 或 exportRecord(runId,format)，取消返回 null；30 秒请求超时不取消底层读写，也不自动重放。

新交换来源返回派生 readOnly=true，重启保留；该字段不是快照内容，导出不包含。包内 file/location 只用于来源证据，不授权 codex.update。相同当前快照导入保留既有授权，同来源不同当前快照返回 RECORD_CONFLICT，失败保留当前库。本地 Codex 选择器或确认扫描重新导入才能恢复来源更新；只读来源的更新请求在读取前拒绝。

交换错误包括 IMPORT_FAILED（包读取/版本/引用/保存失败）、RECORD_CONFLICT（同来源已有不同当前版本）、EXPORT_FAILED（目标保护、编码或文件写入失败），参数、未找到与配额错误沿用 INVALID_PARAMS / NOT_FOUND / IMPORT_LIMIT。输出先临时写入并同步，再替换目标；失败清理，不先截断原文件。数据库/旁路文件与已登记来源受保护，链接/特殊目标拒绝；选择器取消在发送请求前发生，尚无写入过程中的取消协议。

桌面由主进程提供应用数据目录下的 `library.sqlite`，以 `--database ABSOLUTE_FILE` 启动引擎；握手增加 `persistent-library` 及目录方法能力。启动迁移或恢复失败会退出并报错，不回退空内存库，也不删除数据库。无参数独立引擎保留内存模式用于协议测试，此模式退出清空且不提供目录持久化。两者都提供 runs.get / codex.update，演示仍来自 embed，不写数据库。记录契约版本与 SQLite 布局版本独立，详见 [存储说明](../../docs/architecture/storage-validation.md)。
