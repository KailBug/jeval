# Codex 适配器

更新日期：2026-10-04。状态：**经典 rollout JSONL 的显式文件导入已实现，真实版本兼容性尚未验收**。

点击桌面右上角“导入 Codex 记录”，通过系统文件选择器选择一个 `.jsonl` 文件。Go 只读解析所选文件，界面自动切换到 Codex 来源并选中新记录。不会自动扫描用户目录，不需要 API key，也不会发送日志到远程服务。

## 格式与验证范围

适配范围为经典 rollout：逐行 `{timestamp, type, payload}`，含唯一的 `session_meta`，以及 `response_item`、`event_msg`。`history_mode` 缺省或为 `classic` 时接受；`paginated` 等其他模式明确拒绝。

字段核对依据是 OpenAI Codex 的 [ResponseItem 定义](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/models.rs)、[rollout 持久化策略](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/policy.rs) 和 [回合状态协议](https://github.com/openai/codex/blob/main/codex-rs/docs/protocol_v1.md)，核对日期为 2026-10-04。上游格式持续变化，本实现仅支持下表子集，不承诺覆盖某个发布版本的所有记录。

仓库中的 [classic.jsonl](../../fixtures/adapters/codex/classic.jsonl) 完全由人工构造，版本字段 `synthetic-classic-fixture` 不是 Codex 发布版本。未读取或提交用户的私有会话；尚无获准公开的真实脱敏样本集。

| 输入 | 转换与边界 |
| --- | --- |
| `session_meta` | 保留会话 ID、cwd、cli_version、forked_from_id；无有效 ID 或重复 metadata 时拒绝导入 |
| `response_item.message` | 展示 user/assistant/system/developer 文本；其他内容块显示占位，不加载图片或附件 |
| `function_call` / `custom_tool_call` | 工具名与参数/输入；保留 call_id 关联，重复 call_id 拒绝导入 |
| 对应的 `*_output` | 展示结果原文或 JSON，按 call_id 指向父调用；找不到调用时给出提示 |
| `event_msg.task_started/task_complete/turn_aborted` | 展示为回合状态事件；不据此判定整个会话成功、失败或仍在运行 |
| `event_msg.error` | 展示来源错误，不将单个工具/回合错误推广为会话失败 |
| `event_msg.user_message/agent_message` | 忽略镜像消息，避免与 response_item 重复；仅含镜像的格式不在完整兼容范围内 |
| `turn_context`、`event_msg.token_count` | 暂不展示或计算；耗时、token 为 null，会话状态为 unknown |
| 未知类型、无效行与尾部半行 | 跳过并登记物理行号，不停止处理后续合法行 |

## 身份、证据与重复导入

- 文件身份来自解析符号链接后的绝对路径哈希；Windows 按不区分大小写的路径去重。相同路径重导入时完整替换该记录，不追加重复事件；不同路径的副本保留为不同记录。
- 会话 ID 和分支来源单独保留，不将每个回合强行拆成独立运行，也不跨文件自动合并会话。
- 每个事件的 ID 包含文件身份与一基物理行号；`parentId` 只按来源 call_id 关联，支持结果先于调用的记录。
- 导入信息保存实际读取字节的 SHA-256、来源路径、版本及解析提示。摘要用于核对本次读取，不等于源文件已被持久化备份；读取正在写入的文件不提供源端事务一致性保证。
- 再次导入发生致命错误时保留上一份可用记录。截断、替换后的合法文件可通过重导入更新，但这不是检查点式增量同步。

## 上限与当前限制

单文件最多 16 MiB、50000 行以内、5000 个标准化事件；路径最多 2048 UTF-8 字节。单事件正文保留前 8 KiB 预览并明确标注截断，原文仍在用户文件中。提示总数完整计数，详情最多展示前 30 项。本次启动最多登记 20 个文件，总事件数最多 50000（含演示）。超限文件拒绝导入，不伪装成完整结果。

所有记录仅在引擎内存中保留。应用或引擎退出后需重新选择文件；“刷新记录”只重新查询已导入的快照，不重新读取文件。文件选择器可取消，解析阶段尚无取消入口、自动发现、目录扫描、文件监听、SQLite、增量检查点或重启恢复。

## 本轮检查与后续验收

Go 测试覆盖映射、镜像去重、工具关联、来源字节不变、中文/空格路径、坏行/半行、无效时间、UTF-8、超大文件、事件上限和未知类型。跨进程测试覆盖重复导入、原子替换失败和大段转义文本的字节分页；桌面测试通过系统选择器返回值模拟选择，实际执行 IPC → Go 读取与查询，覆盖取消、错误、导入、替换、来源切换和退出后清空。

这些检查仅验证合成样本与本机流程。仍需真实脱敏样本和发布版本兼容矩阵、长日志性能测试、真实系统文件选择器人工验收，以及后续持久化/增量同步。整体状态见 [M0 进度](../development/m0-progress.md)，字段以 [记录草案](../../contracts/record/README.md) 为准。
