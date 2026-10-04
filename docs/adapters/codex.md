# Codex 适配器

更新日期：2026-10-04。状态：**classic / paginated rollout JSONL 导入已实现；一份 Codex 0.160.0 真实文件已在本地通过复测，完整版本兼容矩阵未验收**。

点击桌面右上角“导入 Codex 记录”，通过系统文件选择器选择一个 `.jsonl` 文件。Go 只读解析所选文件，界面自动切换到 Codex 来源并选中新记录。不会自动扫描用户目录，不需要 API key，也不会发送日志到远程服务。

## 格式与验证范围

适配范围为 rollout：逐行 `{timestamp, type, payload}`，含唯一的 `session_meta`，以及 `response_item`、`event_msg`。接受缺省、`classic`、`paginated` 的 history_mode；未知模式仍明确拒绝。此前仅允许 classic，导致 Codex 0.160.0 的 paginated 文件在读取 metadata 时被拒绝，本轮已修复。

字段核对依据是 OpenAI Codex 的 [ResponseItem 定义](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/models.rs)、[rollout 持久化策略](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/policy.rs) 和 [回合状态协议](https://github.com/openai/codex/blob/main/codex-rs/docs/protocol_v1.md)，核对日期为 2026-10-04。上游格式持续变化，本实现仅支持下表子集，不承诺覆盖某个发布版本的所有记录。

仓库中的 [classic.jsonl](../../fixtures/adapters/codex/classic.jsonl) 和 [paginated.jsonl](../../fixtures/adapters/codex/paginated.jsonl) 完全由人工构造，synthetic 版本字段不是 Codex 发布版本。本轮按用户指定路径只读检查了一份真实 0.160.0 文件，但没有把私人内容、绝对路径、会话 ID 或截图加入仓库；尚无获准公开的真实脱敏样本集。

| 输入 | 转换与边界 |
| --- | --- |
| `session_meta` | 保留会话 ID、cwd、cli_version、history_mode、forked_from_id、parent_thread_id；父线程与分支来源分别展示，不混为一谈 |
| `response_item.message` | 展示 user/assistant/system/developer 文本；其他内容块显示占位，不加载图片或附件 |
| `function_call` / `custom_tool_call` | 工具名与参数/输入；保留 call_id 关联，重复 call_id 拒绝导入 |
| 对应的 `*_output` | 展示结果原文或 JSON，按 call_id 指向父调用；找不到调用时给出提示 |
| `event_msg.task_started/task_complete/turn_aborted` | 展示为回合状态事件；不据此判定整个会话成功、失败或仍在运行 |
| `event_msg.error` | 展示来源错误，不将单个工具/回合错误推广为会话失败 |
| `event_msg.user_message/agent_message` | 忽略镜像消息，避免与 response_item 重复；仅含镜像的格式不在完整兼容范围内 |
| `event_msg.item_completed` | 支持 UserMessage/AgentMessage，识别 text/Text 内容；优先保留 canonical response_item，按 ID 或同回合的角色/文本摘要去重，只有投影时保留该消息 |
| `turn_context`、`event_msg.token_count`、`token_usage_record` | 暂不展示或计算；耗时、token 为 null，会话状态为 unknown |
| `world_state`、`thread_settings_applied`、reasoning / Reasoning | 已知的环境、设置与内部推理记录，不作为可见时间线事件，不重复产生未知类型提示 |
| 未知类型、无效行与尾部半行 | 跳过并登记物理行号，不停止处理后续合法行 |

## 身份、证据与重复导入

- 文件身份来自解析符号链接后的绝对路径哈希；Windows 按不区分大小写的路径去重。相同路径重导入时完整替换该记录，不追加重复事件；不同路径的副本保留为不同记录。
- 会话 ID 和分支来源单独保留，不将每个回合强行拆成独立运行，也不跨文件自动合并会话。
- paginated 消息去重先检查整个文件中的 canonical 消息，支持投影先于原消息写入；文本匹配限制在同一回合，不把不同回合的同文提示合并。未知 item_completed 类型仍给出提示，不宣称覆盖所有投影工具类型。
- 每个事件的 ID 包含文件身份与一基物理行号；`parentId` 只按来源 call_id 关联，支持结果先于调用的记录。
- 导入信息保存实际读取字节的 SHA-256、来源路径、版本及解析提示。摘要用于核对本次读取，不等于源文件已被持久化备份；读取正在写入的文件不提供源端事务一致性保证。
- 再次导入发生致命错误时保留上一份可用记录。截断、替换后的合法文件可通过重导入更新，但这不是检查点式增量同步。

## 上限与当前限制

单文件最多 16 MiB、50000 行以内、5000 个标准化事件；路径最多 2048 UTF-8 字节。单事件正文保留前 8 KiB 预览并明确标注截断，原文仍在用户文件中。提示总数完整计数，详情最多展示前 30 项。本次启动最多登记 20 个文件，总事件数最多 50000（含演示）。超限文件拒绝导入，不伪装成完整结果。

所有记录仅在引擎内存中保留。应用或引擎退出后需重新选择文件；“刷新记录”只重新查询已导入的快照，不重新读取文件。文件选择器可取消，解析阶段尚无取消入口、自动发现、目录扫描、文件监听、SQLite、增量检查点或重启恢复。

## 本轮检查与后续验收

Go 测试覆盖映射、镜像去重、工具关联、来源字节不变、中文/空格路径、坏行/半行、无效时间、UTF-8、超大文件、事件上限和未知类型。跨进程测试覆盖重复导入、原子替换失败和大段转义文本的字节分页；桌面测试通过系统选择器返回值模拟选择，实际执行 IPC → Go 读取与查询，覆盖取消、错误、导入、替换、来源切换和退出后清空。

本轮增加 paginated 回归：镜像先后顺序、跨回合同文消息保留、缺少 canonical 的消息回退、父线程来源和工具关联。用户指定的 0.160.0 文件已在开发构建及最终目录包中复测：107 个事件、55 条消息、3 页查询，来源文件字节摘要不变；12 项提示均为 8 KiB 正文预览截断，不是解析失败。Electron 实际导入和 Windows 原生窗口控制覆盖层可见性检查通过，私人内容未进入测试产物。

上述真实检查只覆盖一份消息/回合记录，不能推广为所有 0.160.0 功能均兼容。仍需公开脱敏样本和发布版本矩阵、复杂工具投影、长日志性能、真实系统文件选择器人工验收，以及持久化/增量同步。整体状态见 [M0 进度](../development/m0-progress.md)，字段以 [记录草案](../../contracts/record/README.md) 为准。
