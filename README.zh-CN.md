<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval 标志"></p>

# jeval

[English](README.md)

面向 Agent 开发者的本地桌面工作台，集中查看执行时间线、工具输出和来源证据。

**当前为早期开发预览。** 扫描 Codex 目录后，在二级窗口浏览候选、勾选或全选导入；扫描本身不添加记录。也可单文件导入 classic/paginated JSONL，搜索记录内容、筛选事件类型并查看来源证据，无需 API key。已确认快照与目录配置保存到本地 SQLite，支持重启/离线浏览、手动更新以及任务/事件翻页。可导出单个 Codex 快照为原生 JSON 或 Markdown，并在另一任务库导入 JSON，保留快照和证据。正文仍是每事件 8 KiB 预览加截断提示；导出含来源路径，未自动脱敏。完整正文、自动同步与 Jev 尚未实现。检查使用合成样本，此前曾验证一份 Codex 0.160.0 记录。

![jeval 桌面预览](docs/design/m0-desktop.png)

## 本地运行

需要 Node.js 22.12+ 和 Go 1.26+。使用 Electron、React、TypeScript 与 Go 构建，目前在 Windows x64 上验证。

```sh
npm ci
npm run dev
```

`npm run check` 执行类型检查、引擎/集成测试及生产构建；`npm run pack` 生成桌面目录包。

[开发指南](docs/development/getting-started.md) · [开发规划](docs/jeval-development-plan.md) · [文档索引](docs/README.md)
