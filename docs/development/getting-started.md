# 开发运行指南

更新日期：2026-10-05。命令均从仓库根目录执行，示例使用 Windows PowerShell。

## 环境与首次运行

需要 Node.js 22.12+、npm 和 Go 1.26+（SQLite 驱动依赖要求）。当前已验证环境为 Windows x64、Node.js 24.19.0、Go 1.27.0；这不是所有最低版本均已测试的声明。

```powershell
Set-Location D:\jeval
npm ci
npm run dev
```

`npm ci` 使用已有锁文件安装依赖。首次下载依赖、Electron 二进制与打包工具需要网络。`dev` 先生成应用图标、构建 Go 引擎，再启动 Electron 与渲染层开发服务器；修改 Go 后需停止应用并重新运行。演示与本地导入均不需要 API key；启动不会自动读取 Codex 会话。

## 导入 Codex 文件

点击“导入 Codex 记录”，选择 classic 或 paginated rollout `.jsonl` 文件。首次体验可选择仓库中的 `fixtures/adapters/codex/classic.jsonl` 或 `paginated.jsonl` 合成样本。导入后在 Codex 来源查看记录，展开“导入信息”核对版本、记录模式、解析提示、会话 ID 和摘要；“查看来源证据”显示导入时的实际文件行号。

窗口顶部箭头支持访问记录和来源的后退/前进，无历史时禁用。若旧目录包提示“仅支持 classic rollout”，关闭旧程序并重新打包/打开最新 exe；修复版已支持用户提供的 Codex 0.160.0 paginated 文件。长内容的 8 KiB 预览提示表示展示被截断，不等于文件导入失败。

取消文件选择不会改变已有记录。相同路径再次导入会更新快照；“刷新记录”仅重新查询已保存列表。已确认的标准化快照保存在应用用户数据目录的 `library.sqlite`，应用/引擎重启后恢复，源文件移走仍能浏览保存的预览。手动“更新已登记记录”完整重读当前记录的原路径，读取或写入失败保留旧快照。单文件上限 16 MiB / 5000 个事件；尚无自动同步，完整限制见 [Codex 适配器](../adapters/codex.md)。

## 发现目录中的 Codex 记录

点击“发现本地任务”，选择含 Codex rollout 的目录；可用 `fixtures/adapters/codex` 体验合成记录。扫描只生成候选，不自动导入。结束后弹出“选择要导入的记录”：左侧可搜索、逐条勾选或全选，点击记录查看右侧概要和首条消息，点击“导入所选”才加入任务库。默认没有勾选，关闭或 Esc 不导入；之后可通过“浏览扫描结果”重开。

再次选择同一目录会生成新候选，确认后更新已有快照。全选涵盖所有可选候选；当前任务库最多保留 20 个导入文件、50000 个总事件（含演示），重启不重置配额；超额会提示减少选择且整批不导入。文件在扫描后改变需重新扫描确认。扫描和导入都可取消；取消扫描不改任务库，取消导入保留已完成项。有效的所选目录会保存配置，之后可以按目录显式再次扫描或移除；移除目录配置不删除记录。启动只恢复配置，不自动扫描或导入新文件；“刷新记录”也不会启动重扫。

## 搜索记录内容

打开记录，在执行时间线顶部输入消息、工具或输出关键词，可同时筛选事件类型。查询覆盖当前快照的全部事件，然后分页；原始序号、证据行号和父事件关系保留。搜索忽略英文大小写，匹配标题、正文及角色；清除筛选返回全部事件。范围仅包括已保存的正文预览，源文件中被截断的部分不参与搜索。

任务列表每页最多请求 10 条，详情每次最多请求 50 个事件；使用上一页/下一页翻页，新页替换当前页。协议会按编码字节预算缩短任务页和事件页，界面按 nextOffset 和已访问偏移翻页。当前只保存标准化文本预览，没有原始 JSONL 副本或完整正文；离线查看不代表完整备份。

## 导出与导入 jeval 记录

打开一个已保存的 Codex 任务，在导出区核对快照、事件总数与内容范围，选择 JSON 或 Markdown 并指定保存位置。导出包含当前快照全部事件，不受搜索、类型筛选或当前页影响；来源文件离线也可导出。JSON 可通过侧栏“导入 jeval 记录”选择后加入另一资料库，Markdown 供阅读，不是可再导入格式。

输出包含已保存预览、来源路径和证据，未自动脱敏，不包含原始 JSONL 或截断部分。分享前检查内容。正文预算为 8 KiB，既有截断提示另计；文件上限 64 MiB。只支持单个 Codex 快照，演示和多任务包尚不支持。

新导入的交换记录标记只读，不会根据包中的旧路径读取本机文件；需显式重新导入本地 Codex 来源才启用更新。同一当前快照重复导入不新增任务，同来源已有不同当前版本会拒绝冲突并保留已有记录。取消打开/保存选择器不改变任务库或文件；保存失败保留旧目标，运行中的文件写入尚无取消按钮。格式与版本规则见 [交换契约](../../contracts/exchange/README.md)，检查依据见 [M3 进度](m3-progress.md)。

## 构建与检查

| 命令                   | 内容及前置条件                                                                          |
| ---------------------- | --------------------------------------------------------------------------------------- |
| `npm run typecheck`    | 检查桌面源码与共享 TypeScript 类型                                                      |
| `npm run build:icons`  | 从品牌 SVG 生成窗口 PNG 和 Windows 多尺寸 ICO；dev/build 自动包含此步骤                 |
| `npm run test:engine`  | 运行 Go 测试                                                                            |
| `npm run test:storage` | 检查已构建引擎的合成 SQLite 写入、迁移、关闭重开和清理；test/check 的集成测试也包含此项 |
| `npm test`             | Go 测试、构建 Go 引擎、运行 Node → Go 集成测试                                          |
| `npm run build`        | 生成图标，编译 Go 和 Electron 三个进程层的产物                                          |
| `npm run check`        | 类型检查、Go/集成测试和生产构建；不包含 E2E                                             |
| `npm run test:e2e`     | 启动真实 Electron 测试；需先执行 `npm run build`                                        |
| `npm run pack`         | 先构建，再生成 Windows 本机的目录包                                                     |
| `npm run dist:win`     | 构建 NSIS 开发安装包的入口；尚未执行安装验收                                            |

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

### 独立存储检查

SQLite 已接入桌面任务库。独立引擎的存储检查仍只写入自己新建的临时目录，使用合成数据，不读取私人来源。开发构建后可执行 `npm run test:storage`；目录包复核命令为：

```powershell
$env:JEVAL_STORAGE_EXECUTABLE = 'release/win-unpacked/resources/engine/jeval-engine.exe'
try {
  npm run test:storage
} finally {
  Remove-Item Env:JEVAL_STORAGE_EXECUTABLE -ErrorAction SilentlyContinue
}
```

也可直接运行 `& .\bin\jeval-engine.exe --storage-check $env:TEMP`，目录参数必须为已存在的绝对路径。成功返回 storageCheck=ok 的 JSON，失败返回非零退出码。测试清空子进程 PATH，只验证不调用外部工具，不代替干净 Windows 安装验收。实现和限制见 [存储验证](../architecture/storage-validation.md)。构建会收集 Go 依赖许可文本并随引擎分发 `THIRD-PARTY-NOTICES.txt`。

C 的跨进程交换检查包含在 `npm test` / `npm run check`。复核目录包自带引擎时：

```powershell
$env:JEVAL_EXCHANGE_EXECUTABLE = 'release/win-unpacked/resources/engine/jeval-engine.exe'
try {
  node --import tsx --test tests/integration/record-exchange.test.ts
} finally {
  Remove-Item Env:JEVAL_EXCHANGE_EXECUTABLE -ErrorAction SilentlyContinue
}
```

此检查仍由开发机 Node 执行合成样本和独立临时库，不等同于无开发工具的安装或生产升级验收。

### 桌面入口

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

若诊断显示数据库损坏、版本过新、迁移失败或权限问题，应保留应用用户数据目录及 `library.sqlite`/WAL 文件；应用不会自动删除或重建。修复权限或使用兼容版本后再重连。开发测试可用 `--user-data-dir=绝对目录` 为 Electron 指定独立资料库；不要用已有私人资料库作为合成测试目录。直接运行协议引擎时，`--database ABSOLUTE_FILE` 的父目录须存在；不传参数则为独立内存测试模式。
