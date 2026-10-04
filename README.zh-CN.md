<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval 标志"></p>

# jeval

[English](README.md)

面向 Agent 开发者的本地桌面工作台，集中查看执行时间线、工具输出和来源证据。

**当前为早期开发预览。** 可浏览演示记录，或导入 classic/paginated Codex rollout JSONL 文件，在本地查看消息、工具结果与来源行号，无需 API key。导入仅保留在内存中；自动同步、持久化及 Jev 分析尚未实现。已验证一份 Codex 0.160.0 记录，更广的版本兼容性仍待测试。

![jeval 桌面预览](docs/design/m0-desktop.png)

## 本地运行

需要 Node.js 22.12+ 和 Go 1.24+。使用 Electron、React、TypeScript 与 Go 构建，目前在 Windows x64 上验证。

```sh
npm ci
npm run dev
```

`npm run check` 执行类型检查、引擎/集成测试及生产构建；`npm run pack` 生成桌面目录包。

[开发指南](docs/development/getting-started.md) · [开发规划](docs/jeval-development-plan.md) · [文档索引](docs/README.md)
