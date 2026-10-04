# M0 架构与决策

更新日期：2026-10-04。本文描述当前代码，长期目标见 [开发规划](../jeval-development-plan.md)。

## 实际调用链路

React 界面调用 `window.jeval` 的业务方法，由 preload 经 Electron IPC 交给主进程；主进程校验调用来源，通过 EngineClient 向 Go 子进程发送 JSON Lines 请求。Go 查询内嵌演示和显式导入的 Codex 内存快照，将分页响应写回 stdout。该链路没有 HTTP 服务，也没有数据库。

| 模块 | 当前职责 | 代码入口 |
| --- | --- | --- |
| 渲染层 | 搜索与状态筛选、选中运行、时间线、按需展开工具输出、证据面板 | [App.tsx](../../apps/desktop/src/renderer/app/App.tsx) |
| preload | 仅暴露 hello、listRuns、listEvents、restartEngine、无参数的 importCodex | [preload/index.ts](../../apps/desktop/src/preload/index.ts) |
| 主进程 | 窗口、系统文件选择、IPC 来源检查、引擎路径、启动与退出 | [main/index.ts](../../apps/desktop/src/main/index.ts) |
| EngineClient | 握手、请求 ID、分包响应、超时、进程失败与停止 | [engine-client.ts](../../apps/desktop/src/main/engine-client.ts) |
| Go 引擎入口 | 加载 embed 样例并运行协议服务 | [main.go](../../engine/cmd/jeval-engine/main.go) |
| Go 协议服务 | 请求检查、运行搜索、事件分页、结构化错误和 shutdown | [server.go](../../engine/internal/protocol/server.go) |
| Codex 适配器 | 有界只读快照、消息/工具映射、物理行号与摘要、解析提示 | [import.go](../../engine/internal/adapters/codex/import.go) |
| 数据与类型 | Run/Event 草案和三条合成记录 | [Go 模型](../../engine/internal/model/record.go)、[TS 类型](../../contracts/index.ts)、[样例](../../engine/internal/demo/records.json) |

`engine/internal/storage`、`discovery`、`ingest`、`sync`、`analysis` 等目录仍为规划占位，不应从目录存在推断功能已经实现。

## 已采用的选择

| 选择 | 当前依据与影响 |
| --- | --- |
| Electron + React + TypeScript | 按项目规划实现桌面浏览；主进程和渲染层分别构建 |
| electron-vite 5 + Vite 7 | 使用满足 electron-vite peer dependency 范围的 Vite 版本；准确版本由锁文件固定 |
| 独立 Go 二进制与 stdio | 无需本地端口；应用管理子进程生命周期，可独立测试协议 |
| Go 标准库与 embed 演示数据 | 演示不依赖开发目录、数据库或私有日志，可验证打包链路 |
| electron-builder + extraResources | Go 二进制与窗口图标随目录包携带；Electron 固定为 44.5.1；Windows exe 写入原创 ICO 资源，保留不签名的开发构建 |
| 首个真实来源 Codex | 已实现经典 rollout 的显式导入；版本兼容性尚未通过真实样本验收 |
| Windows x64 本机验证 | 目录包已运行；不据此声明安装验收或跨平台支持完成 |

SQLite 驱动和许可证仍待确定。当前来源身份使用规范化路径，内存快照带原始字节摘要；这是开发草案，不承诺外部导出兼容性。

## 生命周期与边界

开发时从仓库 `bin` 加载引擎；打包后从 `process.resourcesPath/engine` 加载。启动时校验协议/记录版本以及查询能力。关闭时发送 shutdown，最多等待 1.5 秒后终止进程。退出或协议错误会拒绝等待中的请求，不自动重放；桌面提供显式重启接口。具体上限与消息格式见 [协议 v1](../../contracts/protocol/README.md)。

渲染层启用 context isolation 和 sandbox，关闭 Node integration；主进程限制 IPC 调用窗口及顶层 frame URL，拒绝新窗口、导航和权限请求。执行内容按普通文本展示，内置证据引用不访问磁盘。

`importCodex()` 不接受渲染层路径参数。主进程从系统文件选择器取得单个路径后交给 Go；Go 只读普通 UTF-8 JSONL 文件，完整解析成功后才替换内存记录。重复导入按规范化路径去重。导入请求最多等待 30 秒，普通查询仍为 5 秒；不自动重试导入。数据随引擎退出清除，未实现磁盘快照和重启恢复。大小与兼容边界见 [Codex 适配器](../adapters/codex.md)。

引擎故障目前通过请求失败传递给界面；尚无主动推送健康通知、扫描进度、取消扫描或数据落盘机制。端到端测试验证显式重启，尚未覆盖强制崩溃后的完整 UI 恢复流程。
