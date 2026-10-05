# SQLite 与可恢复任务库（A / B 切片）

更新日期：2026-10-05。A 的存储验证现已接入 B 的桌面任务库：Electron 主进程提供用户数据目录中的 `library.sqlite`，Go 保存已确认快照与来源目录配置。启动恢复任务元数据，事件从 SQL 按页读取；演示仍内嵌，不写入数据库。检查依据见 [M0 进度](../development/m0-progress.md)。

## 驱动决策

采用 `modernc.org/sqlite v1.60.1`，版本与间接依赖由 [go.mod](../../engine/go.mod) 和 [go.sum](../../engine/go.sum) 锁定。官方文档列出无 cgo 实现和 Windows amd64 支持；本仓库以实际 `CGO_ENABLED=0` 构建和打包引擎检查验证可运行性，不把平台列表当成本项目验收。[驱动文档](https://pkg.go.dev/modernc.org/sqlite@v1.60.1)

比较过的另一方向是 `mattn/go-sqlite3`：其构建依赖 cgo/C 编译器，与现有纯 Go 引擎打包方式不一致，因此本轮没有引入或跑性能比较。选择依据是 Windows 分发和构建约束，不声称所选驱动更快。[候选驱动构建要求](https://github.com/mattn/go-sqlite3#installation)

所选依赖将最低 Go 版本提高到 1.26；本机使用 Go 1.27.0，最低版本尚未单独验证。构建脚本从实际链接模块的本地版本收集 LICENSE/COPYING/NOTICE，输出 `bin/THIRD-PARTY-NOTICES.txt`，随引擎进入目录包。项目自身许可证仍待作者选择。

## 已实现的存储边界

[storage 包](../../engine/internal/storage/store.go) 接受绝对数据库路径，父目录必须存在。桌面创建应用数据目录并以 `--database` 提供路径；渲染层不能提供数据库路径。仅保存经过校验的 Codex 标准化快照，正文仍是 8 KiB 预览；不是原文件备份，也不是完整内容存储。为避免混入 SQLite URI 参数，目前拒绝数据库路径中的 `?`、`#`。

- `snapshots` 保存不可变版本及单记录 JSON；`sources` 保存当前版本指针，复合外键确保指针属于同一来源。数据库布局不作为对外交换契约。
- `snapshot_runs` 保存独立任务元数据，`snapshot_events` 保存按快照/序号索引的事件 JSON、类型和搜索文本。保留 `record_json` 用于历史恢复与一致性检查；重复存储会增加磁盘占用，当前没有历史清理。
- 快照数据、派生索引与当前指针在一个事务提交；相同版本重复保存不增加行；不同字节或适配器版本生成新快照，旧版本继续可读。同一身份对应不同归一化内容会报错，不悄悄改写历史。提交完成后协议服务才发布当前任务元数据。
- `user_version=2`；v1→v2 在同一迁移事务内验证已有记录并回填索引。DDL、回填和版本号一起提交；未来版本、损坏文件与迁移失败均返回错误，不删除或重建数据库，也不静默回退空内存库。SQLite 布局版本不改变记录 schemaVersion=1。
- 单连接池；通过连接参数对每次连接启用外键、1 秒 busy timeout 和 `synchronous=FULL`，使用 WAL。WAL 仍要求本地文件系统；这不是网络共享盘支持承诺。[SQLite WAL](https://www.sqlite.org/wal.html)、[同步设置](https://www.sqlite.org/pragma.html#pragma_synchronous)
- 查询可按来源读取当前快照，也可按快照 ID 读取旧版本；源文件不参与读取。取消和 SQL 错误不发布新指针。SQL 事件查询在同一读事务内取得匹配数及页，搜索文本由 Go 转小写后保存，以保持 Unicode 查询语义；只读取所需事件页。
- `directories` 保存显式选择且通过根目录校验的目录配置，按规范化路径生成稳定 ID。启动不扫描；用户可显式重扫或移除配置，移除不删除记录。扫描候选仍为内存摘要，必须重新确认才导入。

身份和事件引用的字段、摘要算法以 [记录契约](../../contracts/record/README.md) 为准。适配器归一化规则（含预览范围、去重、字段解释）变化时必须更新 adapterVersion；来源会话 ID 不能代替文件身份。当前事件 ID 只在所属快照内解释。

当前任务库仍限制 20 个导入文件、50000 个当前事件（应用包含演示），单快照最多 5000 个事件；重启不重置配额。任务元数据集合有界，UI 每页 10 条；事件 SQL 每次至多请求 100 条，桌面每页请求 50 条，并以实际编码预算返回 nextOffset。新页替换当前页，避免持续累积 DOM。没有全文索引、磁盘配额或历史清理，尚无规模性能结论。

B 的正文策略是先持久化现有标准化预览，保持适配器版本和快照身份稳定；界面明确最多 8 KiB、仅搜索已保存预览。完整正文/原始附件按需读取留待后续契约与存储设计；C 的导出必须携带这一范围，不能把预览包装成完整备份。

## 复核方法与限制

[Go 存储测试](../../engine/internal/storage/store_test.go) 覆盖重复保存、重启、源文件移走、历史快照读取、SQL 触发器注入的发布失败、取消、迁移 DDL 回滚、未来版本拒绝、损坏文件保留、无效证据拒绝，以及未提交事务发生子进程 `os.Exit` 后重新打开并执行 integrity_check。身份测试覆盖同源重读、字节变化、不同路径副本及适配器版本变化。

`npm run test:storage` 调用实际引擎的 `--storage-check ABSOLUTE_DIRECTORY`，在参数目录内新建唯一临时目录，写入合成记录、关闭重开并检查完整性，最后删除自己创建的临时数据。它不读取 Codex 私人目录。测试清空子进程 PATH，检查无需外部 sqlite/go/node 命令；这不等于无开发工具的干净机器验收。

设置 `JEVAL_STORAGE_EXECUTABLE` 可检查目录包自带引擎。CI 已配置打包后的探针；远程结果需以实际 CI 记录为准。可复制命令见 [运行指南](../development/getting-started.md)，本轮实际结果见 [M0 进度](../development/m0-progress.md)。

未覆盖真实磁盘耗尽、断电、文件系统故障、NSIS 安装/升级迁移和规模性能。当前 crash 用例验证进程异常退出，不模拟操作系统掉电；迁移失败通过无效 SQL 与无效历史记录验证，v1 数据库由测试构造，尚无生产升级样本。
