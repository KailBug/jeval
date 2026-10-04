# M0 开发预览验收

更新日期：2026-10-04。版本：`0.1.0-dev.0`。状态：**本机目录包验证通过，尚未发布**。

当前只交付合成记录浏览。来源兼容、安装后完整使用流程和公开 Alpha 范围均未验收。

2026-10-04 UI 第二版已按 Mac / Codex 风格更新并重新生成同一路径的目录包，类型检查、构建和最终目录包 E2E 通过。当前截图和设计说明见 [M0 界面](../design/m0-ui.md)。重新打开目录包中的程序即可使用新界面。

同日已补充 jeval 原创图标并再次打包。目录包 E2E、侧栏图片加载检查和 exe 图标资源核对通过；九个尺寸与源 ICO 图片字节一致。窗口 PNG 与 exe ICO 均来自同一 SVG，见 [标志说明](../design/branding.md)。

## 产物与运行

`npm run pack` 在当前 Windows 环境生成 `release/win-unpacked`；桌面入口为 `jeval.exe`，Go 引擎位于 `resources/engine/jeval-engine.exe`。必须保留整个目录，产物不纳入版本控制。

`npm run dist:win` 已配置为 NSIS 构建入口，但本轮没有生成或验证 NSIS 安装包。当前配置启用 Windows 图标/版本资源写入，使用 `signExecutable: false` 关闭代码签名；不作为正式签名发布配置。运行与 GoLand 常见问题见 [开发运行指南](../development/getting-started.md)。

## 验收记录

以下结果来自 2026-10-04 的本机开发验证。UI 第二版重新执行了类型检查、打包与桌面 E2E；Go 与跨进程用例结果沿用首次实现验证，具体分轮记录见 [M0 进度](../development/m0-progress.md)。

| 检查项 | 状态 | 依据/剩余工作 |
| --- | --- | --- |
| 类型检查、Go 测试、真实子进程测试、生产构建 | 通过 | `npm run check` |
| 未打包桌面浏览 | 通过 | 生产构建后运行 `npm run test:e2e` |
| Windows 目录包构建 | 通过 | `npm run pack` |
| 目录包加载自带引擎并浏览 | 通过 | 设置 `JEVAL_PACKAGED_EXECUTABLE` 后运行同一 E2E |
| 显式重启引擎 | 通过 | E2E 调用 restartEngine 并重新查询 |
| 强制崩溃、超时后的完整 UI 恢复 | 未验证 | 尚无对应端到端故障注入用例 |
| NSIS 安装、卸载及安装后启动 | 未验证 | 当前仅有构建配置 |
| 没有 Go/Node 的干净 Windows 设备 | 未验证 | 本机具备开发工具，不能替代该验收 |
| 中文/空格安装路径、权限、缩放、升级迁移 | 未验证 | 需独立测试环境与用例 |
| GitHub Actions | 已配置，未远程执行 | [CI 工作流](../../.github/workflows/ci.yml) |
| Codex 真实日志与导出再导入 | 未实现 | 首个真实使用链路的主要缺口 |
| 许可证、签名与公开发布材料 | 未完成 | 由后续开源发布阶段处理 |

当前端到端用例见 [desktop.spec.ts](../../tests/e2e/desktop.spec.ts)。它验证合成数据浏览、过滤、证据、两种窗口尺寸及显式重启，不验证日志采集、数据迁移或安装流程。
