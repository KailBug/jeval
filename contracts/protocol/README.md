# 桌面与引擎协议 v1

传输为 UTF-8 JSON Lines：每条消息以 LF 结束。stdout 只传协议帧，stderr 写诊断；请求和响应均小于 1 MiB，超大输入关闭连接。当前引擎按输入顺序处理短时查询，桌面按 ID 匹配响应，允许多个未完成请求。当前没有耗时扫描与取消接口。

请求：

```json
{"type":"request","version":1,"id":"1","method":"hello","params":{}}
```

响应只包含 `result` 或 `error` 中的一个字段：

```json
{"type":"response","version":1,"id":"1","result":{"engineVersion":"0.1.0-dev.0","protocolVersion":1,"recordVersion":1,"capabilities":["demo","runs.list","runs.events"]}}
```

```json
{"type":"response","version":1,"id":"2","error":{"code":"NOT_FOUND","message":"Run not found"}}
```

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `hello` | `{}` | 引擎、协议、记录版本与能力列表 |
| `runs.list` | `search?`, `status?`, `offset?`, `limit?` | 匹配的运行分页 |
| `runs.events` | `runId`, `offset?`, `limit?` | 单条运行的事件分页 |
| `shutdown` | `{}` | `{ "ok": true }`，随后退出 |

分页返回 `{ items, total, nextOffset }`。`offset` 从 0 开始，最大 10 亿；默认 `limit=50`，范围 1–100。到达末页时 `nextOffset=null`，空结果 `items=[]`。搜索匹配标题、项目和来源，忽略英文大小写。状态支持 `all`、`completed`、`failed`、`unknown`。

错误码：`PARSE_ERROR`、`INVALID_REQUEST`、`PROTOCOL_MISMATCH`、`METHOD_NOT_FOUND`、`INVALID_PARAMS`、`NOT_FOUND`。无法解析的请求返回空 ID，其后的合法请求仍可处理。ID 是不超过 128 字节的非空字符串。

桌面请求默认超时 5 秒，最多 128 个未完成请求。连接损坏、子进程退出或输入管道错误时拒绝所有等待中的请求；界面提供显式重启。未完成操作不自动重放。关闭时发送 shutdown，最多等候 1.5 秒后终止子进程。

Electron 渲染层只能访问 `hello/listRuns/listEvents/restartEngine` 四个业务方法，不提供任意 IPC、路径访问或执行命令能力。主进程校验调用窗口与顶层 frame URL。启用 context isolation、sandbox 和 CSP，拒绝新窗口与页面跳转。依据：[Electron 安全指南](https://www.electronjs.org/docs/latest/tutorial/security)、[electron-vite 构建文档](https://electron-vite.org/guide/build)。
