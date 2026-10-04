# 桌面与引擎协议 v1

传输为 UTF-8 JSON Lines：每条消息以 LF 结束。stdout 只传协议帧，stderr 写诊断；请求和响应均小于 1 MiB，超大输入关闭连接。当前引擎按输入顺序处理查询和有界的单文件导入，桌面按 ID 匹配响应，允许多个未完成请求。当前没有目录扫描与取消接口。

请求：

```json
{"type":"request","version":1,"id":"1","method":"hello","params":{}}
```

响应只包含 `result` 或 `error` 中的一个字段：

```json
{"type":"response","version":1,"id":"1","result":{"engineVersion":"0.1.0-dev.0","protocolVersion":1,"recordVersion":1,"capabilities":["demo","runs.list","runs.events","codex.import"]}}
```

```json
{"type":"response","version":1,"id":"2","error":{"code":"NOT_FOUND","message":"Run not found"}}
```

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `hello` | `{}` | 引擎、协议、记录版本与能力列表 |
| `runs.list` | `search?`, `status?`, `source?`, `offset?`, `limit?` | 匹配的运行分页；source 支持 all/demo/codex，缺省为 all |
| `runs.events` | `runId`, `offset?`, `limit?` | 单条运行的事件分页 |
| `codex.import` | `path`（绝对路径） | `{ run, replaced }`；只读解析成功后按路径替换内存记录，失败不改变旧记录 |
| `shutdown` | `{}` | `{ "ok": true }`，随后退出 |

分页返回 `{ items, total, nextOffset }`。`offset` 从 0 开始，最大 10 亿；默认 `limit=50`，范围 1–100。页内元素的 JSON 编码预算为 512 KiB，必要时提前结束该页；调用方必须使用 nextOffset，不能把请求 limit 当作实际返回数。到达末页时 `nextOffset=null`，空结果 `items=[]`。搜索匹配标题、项目和来源，忽略英文大小写。状态支持 `all`、`completed`、`failed`、`unknown`。

错误码：`PARSE_ERROR`、`INVALID_REQUEST`、`PROTOCOL_MISMATCH`、`METHOD_NOT_FOUND`、`INVALID_PARAMS`、`NOT_FOUND`、`IMPORT_FAILED`（输入或读取失败）、`IMPORT_LIMIT`（本次启动的记录上限）。无法解析的请求返回空 ID，其后的合法请求仍可处理。ID 是不超过 128 字节的非空字符串。导入上限与格式见 [Codex 适配器](../../docs/adapters/codex.md)。

桌面请求默认超时 5 秒，单文件导入为 30 秒，最多 128 个未完成请求。文件选择/导入与重启互斥。超时不是取消；不自动重放导入。连接损坏、子进程退出或输入管道错误时拒绝所有等待中的请求；界面提供显式重启。重启清除所有内存导入记录。关闭时发送 shutdown，最多等候 1.5 秒后终止子进程。

Electron 渲染层只能访问 `hello/listRuns/listEvents/restartEngine/importCodex` 五个业务方法，以及只读 platform 字符串（标题栏布局使用），不提供任意 IPC、路径访问或执行命令能力。`importCodex()` 没有参数，只能由主进程文件选择器提供路径；取消返回 null。主进程校验调用窗口与顶层 frame URL。启用 context isolation、sandbox 和 CSP，拒绝新窗口与页面跳转。依据：[Electron 安全指南](https://www.electronjs.org/docs/latest/tutorial/security)、[electron-vite 构建文档](https://electron-vite.org/guide/build)。
