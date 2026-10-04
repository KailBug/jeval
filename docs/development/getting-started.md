# 开发运行指南

更新日期：2026-10-04。命令均从仓库根目录执行，示例使用 Windows PowerShell。

## 环境与首次运行

需要 Node.js 22.12+、npm 和 Go 1.24+。当前已验证环境为 Windows x64、Node.js 24.19.0、Go 1.27.0；这不是所有最低版本均已测试的声明。

```powershell
Set-Location D:\jeval
npm ci
npm run dev
```

`npm ci` 使用已有锁文件安装依赖。首次下载依赖、Electron 二进制与打包工具需要网络。`dev` 先生成应用图标、构建 Go 引擎，再启动 Electron 与渲染层开发服务器；修改 Go 后需停止应用并重新运行。演示浏览不需要 API key，不读取本地 Codex 会话。

## 构建与检查

| 命令 | 内容及前置条件 |
| --- | --- |
| `npm run typecheck` | 检查桌面源码与共享 TypeScript 类型 |
| `npm run build:icons` | 从品牌 SVG 生成窗口 PNG 和 Windows 多尺寸 ICO；dev/build 自动包含此步骤 |
| `npm run test:engine` | 运行 Go 测试 |
| `npm test` | Go 测试、构建 Go 引擎、运行 Node → Go 集成测试 |
| `npm run build` | 生成图标，编译 Go 和 Electron 三个进程层的产物 |
| `npm run check` | 类型检查、Go/集成测试和生产构建；不包含 E2E |
| `npm run test:e2e` | 启动真实 Electron 测试；需先执行 `npm run build` |
| `npm run pack` | 先构建，再生成 Windows 本机的目录包 |
| `npm run dist:win` | 构建 NSIS 开发安装包的入口；尚未执行安装验收 |

检查前关闭正在运行的 jeval，避免 Windows 占用待覆盖的引擎或目录包文件。`test:desktop` 单独执行时也要求已有 `bin/jeval-engine.exe`，通常直接运行 `npm test`。

目录包复测：

```powershell
npm run pack
$env:JEVAL_PACKAGED_EXECUTABLE = 'release/win-unpacked/jeval.exe'
try {
  npm run test:e2e
} finally {
  Remove-Item Env:JEVAL_PACKAGED_EXECUTABLE -ErrorAction SilentlyContinue
}
```

E2E 临时用户数据在 `.local/e2e-*`；截图和失败 trace 在 `test-results`。以上目录不纳入 Git。检查范围和已有结果见 [M0 进度](m0-progress.md)，安装验收见 [预览验收](../releases/m0-preview.md)。

## 运行已生成的程序

在 Windows 文件资源管理器中打开 `D:\jeval\release\win-unpacked`，双击 `jeval.exe`。也可以在终端执行：

```powershell
& "D:\jeval\release\win-unpacked\jeval.exe"
```

保留整个 `win-unpacked` 目录，包括 `resources/engine/jeval-engine.exe`；不能仅复制主程序。`bin/jeval-engine.exe` 是没有图形界面的协议子进程，不是桌面启动入口。

源码改动不会自动更新已打包的程序。需要先关闭目录包中的 jeval，再执行 `npm run pack`，然后重新打开同一路径的 `jeval.exe`；开发过程中需要即时查看渲染层修改时使用 `npm run dev`。

### GoLand 弹出 Register New File Type Association

在 GoLand 项目树双击 exe 会触发编辑器的文件打开流程。取消该弹窗，无需给 exe 关联文本类型。改用资源管理器或上面的 PowerShell 命令启动。

### 引擎连接失败

先确认 `bin/jeval-engine.exe`（开发模式）或 `resources/engine/jeval-engine.exe`（目录包）存在。开发模式可重新运行 `npm run build:engine`；目录包缺文件时重新运行 `npm run pack`。基础连接错误会显示在界面中，可使用“重新连接引擎”；需要更多诊断时从终端启动开发模式查看输出。
