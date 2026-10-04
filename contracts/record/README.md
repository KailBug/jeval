# 最小统一记录 v1（M0）

TypeScript 定义位于 `contracts/index.ts`，Go 对应结构位于 `engine/internal/model/record.go`。当前示例为 `engine/internal/demo/records.json`，通过 Go embed 进入可执行文件，以免依赖运行目录或私有文件。真实子进程集成测试验证两端对这些样例的解释。

`Record` 包含 `schemaVersion=1`、`runs` 和 `events`。运行保存标题、项目、来源、是否为演示、状态与可选指标；事件保存运行 ID、来源顺序、种类、角色、原文、父事件 ID 与证据引用。

- `startedAt`、`timestamp`、`durationMs` 和 `tokens` 未知时为 `null`，不可替换为 0 或当前时间。
- `status=unknown` 表示缺少完成状态，不能据此判断成功、失败或仍在运行。
- 时间戳使用带时区的 RFC 3339 格式。
- `sequence` 与证据 `line` 从 1 开始；内置样例的 line 是虚拟事件序号，不是 JSON 文件物理行号。
- `evidence` 保存 `sourceId/location/line`；`embedded:` 表示合成证据。真实适配器需要提供实际来源定位，不能复用合成来源标识。
- `parentId` 保留关联关系，不使用时间相近自动推断因果。
- 文本按普通文本渲染，不执行日志中的 HTML、链接或命令。

本草案尚未覆盖规划中的 Source、Session、Artifact、Finding、Annotation、快照摘要与同步检查点；真实 Codex 适配前需基于脱敏样本完善，再冻结可导入导出的记录格式。当前 schemaVersion 仅标识开发草案，不代表已具备外部兼容承诺。
