<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval 标志"></p>

# jeval

[English](README.md)

面向 Agent 开发者的本地桌面工作台，集中查看执行时间线、工具输出和来源证据。

**当前为早期开发预览。** 已支持基于合成演示数据的搜索、筛选和记录查看。首个计划接入的来源为 Codex；真实日志采集和 Jev 分析尚未实现。体验演示无需 API key。

![jeval 桌面预览](docs/design/m0-desktop.png)

## 本地运行

需要 Node.js 22.12+ 和 Go 1.24+。使用 Electron、React、TypeScript 与 Go 构建，目前在 Windows x64 上验证。

```sh
npm ci
npm run dev
```

`npm run check` 执行类型检查、引擎/集成测试及生产构建；`npm run pack` 生成桌面目录包。

[开发指南](docs/development/getting-started.md) · [开发规划](docs/jeval-development-plan.md) · [文档索引](docs/README.md)
