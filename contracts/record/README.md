# 最小统一记录 v1（M0）

TypeScript 定义位于 `contracts/index.ts`，Go 对应结构位于 `engine/internal/model/record.go`。当前示例为 `engine/internal/demo/records.json`，通过 Go embed 进入可执行文件，以免依赖运行目录或私有文件。真实子进程集成测试验证两端对这些样例的解释。

`Record` 包含 `schemaVersion=1`、`runs` 和 `events`。运行保存标题、项目、来源、是否为演示、状态与可选指标；事件保存运行 ID、来源顺序、种类、角色、原文、父事件 ID 与证据引用。

- `startedAt`、`timestamp`、`durationMs` 和 `tokens` 未知时为 `null`，不可替换为 0 或当前时间。
- `status=unknown` 表示缺少完成状态，不能据此判断成功、失败或仍在运行。
- 时间戳使用带时区的 RFC 3339 格式。
- `sequence` 与证据 `line` 从 1 开始；内置样例的 line 是虚拟事件序号，不是 JSON 文件物理行号。
- `evidence` 保存 `sourceId/location/line`；`embedded:` 表示合成证据。真实适配器需要提供实际来源定位，不能复用合成来源标识。
- `parentId` 保留关联关系，不使用时间相近自动推断因果。
- `kind=lifecycle` 表示来源回合开始、结束或中止；与 verification 分开，不表示通过验证。Codex 会话仍为 unknown，耗时及 token 暂不映射。
- Codex 运行带可选 `importInfo`：`file/sha256/sessionId/cliVersion/forkedFromId?/warningCount/warnings`。warnings 保存最多 30 条 `{line,message}`，warningCount 为完整计数。摘要对应实际读取的原始字节，不代表已保存原文件副本。
- Codex 的 `source=Codex`、`demo=false`；事件 line 为实际文件物理行号，事件 ID 由规范化路径身份和行号组成。标题取第一条非空 user 消息前 80 个字符；正文最多保留 8 KiB 预览，截断有提示。
- 文本按普通文本渲染，不执行日志中的 HTML、链接或命令。

本草案已补充导入来源、会话标识和快照摘要，尚未覆盖完整 Source、Session、Artifact、Finding、Annotation 与同步检查点。需基于真实脱敏样本进一步验证，再冻结可导入导出的记录格式。当前 schemaVersion 仅标识开发草案，不代表已具备外部兼容承诺。
