import { useEffect, useState } from 'react'
import { Icon } from '../components/Icon'
import logo from '../../../resources/jeval.svg'
import type { Hello, Page, Run, RunEvent, RunStatus } from '../../../../../contracts/index'

const statusLabels: Record<RunStatus, string> = {
  completed: '已完成',
  failed: '失败',
  unknown: '状态未知'
}
const kindLabels: Record<RunEvent['kind'], string> = {
  message: '消息',
  tool_call: '工具调用',
  tool_result: '工具结果',
  verification: '验证',
  lifecycle: '回合状态',
  error: '错误'
}
const date = (value: string | null) =>
  value
    ? new Intl.DateTimeFormat('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
      }).format(new Date(value))
    : '时间未知'
const errorMessage = (error: unknown) => (error instanceof Error ? error.message : String(error))

function Status({ status }: { status: RunStatus }) {
  return (
    <span className={`status ${status}`}>
      <span aria-hidden="true" />
      {statusLabels[status]}
    </span>
  )
}

export function App() {
  const [hello, setHello] = useState<Hello>()
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<RunStatus | 'all'>('all')
  const [runs, setRuns] = useState<Page<Run>>()
  const [selected, setSelected] = useState<Run>()
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const [reconnecting, setReconnecting] = useState(false)
  const [source, setSource] = useState<'demo' | 'codex'>('demo')
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let active = true
    window.jeval
      .hello()
      .then((value) => {
        if (active) setHello(value)
      })
      .catch((e) => {
        if (active) {
          setError(errorMessage(e))
          setLoading(false)
        }
      })
    return () => {
      active = false
    }
  }, [revision])

  useEffect(() => {
    if (!hello) return
    let active = true
    setLoading(true)
    const timer = setTimeout(() => {
      window.jeval
        .listRuns({ search, status, source, limit: 100 })
        .then((page) => {
          if (!active) return
          setRuns(page)
          setSelected((old) => page.items.find((run) => run.id === old?.id) ?? page.items[0])
          setError('')
        })
        .catch((e) => {
          if (active) setError(errorMessage(e))
        })
        .finally(() => {
          if (active) setLoading(false)
        })
    }, 150)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [hello, search, status, source, revision])

  async function importCodex() {
    setImporting(true)
    setImportError('')
    try {
      const result = await window.jeval.importCodex()
      if (!result) return
      setSource('codex')
      setSearch('')
      setStatus('all')
      setSelected(result.run)
      setNotice(`${result.replaced ? '已更新' : '已导入'}记录 · ${result.run.eventCount} 个事件`)
      setRevision((n) => n + 1)
    } catch (e) {
      setImportError(errorMessage(e))
    } finally {
      setImporting(false)
    }
  }

  async function reconnect() {
    setReconnecting(true)
    setError('')
    try {
      setHello(await window.jeval.restartEngine())
      setNotice('引擎已重新连接。本次启动的导入记录已清除，请重新导入。')
      setRevision((n) => n + 1)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setReconnecting(false)
    }
  }

  return (
    <div className="workspace">
      <aside className="sidebar" aria-label="工作空间">
        <div className="brand">
          <img className="brand-logo" src={logo} alt="" width="28" height="28" />
          <strong>jeval</strong>
          <span className="alpha">预览版</span>
        </div>
        <div className="workspace-label">工作空间</div>
        <nav aria-label="主导航">
          <div className="nav-item active" aria-current="page">
            <Icon name="library" />
            <span>任务库</span>
          </div>
        </nav>
        <div className="sidebar-section">数据来源</div>
        <button
          className={'source-item ' + (source === 'demo' ? 'selected' : '')}
          aria-pressed={source === 'demo'}
          onClick={() => setSource('demo')}
        >
          <Icon name="folder" />
          <span>合成演示</span>
          <span className="source-count">3</span>
        </button>
        <button
          className={'source-item ' + (source === 'codex' ? 'selected' : '')}
          aria-pressed={source === 'codex'}
          onClick={() => setSource('codex')}
        >
          <Icon name="terminal" />
          <span>Codex</span>
          <span className="source-count">本地</span>
        </button>
        <div className="sidebar-bottom">
          <div className="local-label">
            <span className={hello && !error ? 'source-dot' : 'offline-dot'} />
            {hello && !error ? '本地引擎已连接' : '引擎连接待检查'}
          </div>
          <span className="version">{hello?.engineVersion ?? '正在连接'}</span>
        </div>
      </aside>
      <main>
        <header className="topbar">
          <div className="topbar-heading">
            <h1>任务库</h1>
            <span className="preview-label">{source === 'demo' ? '演示空间' : 'Codex 记录'}</span>
          </div>
          <div className="topbar-actions">
            <span className="local-pill">
              <Icon name="monitor" />
              本地
            </span>
            <button
              className="button"
              onClick={importCodex}
              disabled={!hello || importing || reconnecting}
            >
              <Icon name="folder" />
              {importing ? '正在导入…' : '导入 Codex 记录'}
            </button>
            <button className="button" onClick={() => setRevision((n) => n + 1)} disabled={loading}>
              <Icon name="refresh" />
              刷新记录
            </button>
          </div>
        </header>
        <div className="demo-banner">
          <Icon name="info" />
          <span>
            {source === 'demo'
              ? '当前为合成演示。可选择 Codex JSONL 文件，查看本地执行记录。'
              : '仅在本地读取所选文件。本次导入保留到应用或引擎退出；文件更新后需重新导入。'}
          </span>
        </div>
        {notice && (
          <div className="import-notice" role="status">
            <span>{notice}</span>
            <button className="text-button" onClick={() => setNotice('')}>
              关闭提示
            </button>
          </div>
        )}
        {importError && (
          <div className="error-banner" role="alert">
            <Icon name="alert" />
            <span>{importError}</span>
            <button className="text-button" onClick={() => setImportError('')}>
              关闭提示
            </button>
          </div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            <Icon name="alert" />
            <span>{error}</span>
            <button className="button" onClick={reconnect} disabled={reconnecting}>
              {reconnecting ? '连接中…' : '重新连接引擎'}
            </button>
          </div>
        )}
        <section className="library" aria-label="任务库">
          <div className="library-body" aria-busy={loading}>
            <section className="run-panel" aria-label="运行列表">
              <div className="library-toolbar">
                <div className="list-heading">
                  <h2>
                    最近执行 <span>{runs?.total ?? '—'}</span>
                  </h2>
                  <select
                    aria-label="筛选状态"
                    value={status}
                    onChange={(event) => setStatus(event.target.value as RunStatus | 'all')}
                  >
                    <option value="all">全部状态</option>
                    <option value="completed">已完成</option>
                    <option value="failed">失败</option>
                    <option value="unknown">状态未知</option>
                  </select>
                </div>
                <label className="search">
                  <Icon name="search" />
                  <input
                    aria-label="搜索任务"
                    placeholder="搜索任务或项目"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    maxLength={200}
                  />
                </label>
              </div>
              <div className="run-list">
                {loading && (
                  <div className="list-caption" role="status">
                    加载中…
                  </div>
                )}
                {!loading && runs?.items.length === 0 && (
                  <div className="empty">
                    <Icon name="search" />
                    <strong>
                      {source === 'codex' && !search && status === 'all'
                        ? '还没有 Codex 记录'
                        : '没有匹配的记录'}
                    </strong>
                    <p>
                      {source === 'codex' && !search && status === 'all'
                        ? '点击右上角导入，选择一个 rollout JSONL 文件。'
                        : '试试其他关键词或状态。'}
                    </p>
                    <button
                      className="text-button"
                      onClick={() => {
                        setSearch('')
                        setStatus('all')
                      }}
                    >
                      清除筛选
                    </button>
                  </div>
                )}
                {runs?.items.map((run) => (
                  <button
                    key={run.id}
                    className={'run-card ' + (run.id === selected?.id ? 'selected' : '')}
                    aria-pressed={run.id === selected?.id}
                    onClick={() => setSelected(run)}
                  >
                    <div className="run-card-meta">
                      <Icon name="folder" />
                      <span>{run.project}</span>
                    </div>
                    <h3>{run.title}</h3>
                    <div className="run-card-footer">
                      <Status status={run.status} />
                      <time>{date(run.startedAt)}</time>
                    </div>
                  </button>
                ))}
              </div>
              <div className="list-footer">
                <Icon name="monitor" />
                <span>本地浏览，无需 API key</span>
              </div>
            </section>
            {selected ? (
              <RunDetail
                key={selected.id + ':' + (selected.importInfo?.sha256 ?? '') + ':' + revision}
                run={selected}
              />
            ) : (
              <div className="detail-empty">
                <Icon name="library" />
                <h2>{loading ? '正在连接工作台' : '选择一条执行记录'}</h2>
                <p>{loading ? '正在加载记录…' : '查看任务的执行过程与来源证据。'}</p>
              </div>
            )}
          </div>
        </section>
      </main>
    </div>
  )
}

function RunDetail({ run }: { run: Run }) {
  const [events, setEvents] = useState<RunEvent[]>([])
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [evidence, setEvidence] = useState<RunEvent>()
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let active = true
    setLoading(true)
    window.jeval
      .listEvents({ runId: run.id, limit: 50 })
      .then((page) => {
        if (active) {
          setEvents(page.items)
          setNextOffset(page.nextOffset)
          setError('')
        }
      })
      .catch((e) => {
        if (active) setError(errorMessage(e))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [run.id, attempt])

  async function loadMore() {
    if (nextOffset === null || loading) return
    setLoading(true)
    try {
      const page = await window.jeval.listEvents({ runId: run.id, offset: nextOffset, limit: 50 })
      setEvents((old) => [...old, ...page.items])
      setNextOffset(page.nextOffset)
      setError('')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <article className="run-detail" aria-label="执行详情">
      <div className="detail-content">
        <div className="detail-heading">
          <div className="detail-kicker">
            <Icon name="folder" />
            <span>{run.project}</span>
          </div>
          <h2>{run.title}</h2>
          <div className="detail-subtitle">
            <Status status={run.status} />
            <span>{date(run.startedAt)}</span>
            <span>{run.demo ? '合成演示记录' : 'Codex 本地记录'}</span>
          </div>
        </div>
        {run.importInfo && (
          <details className="import-details">
            <summary>
              导入信息
              {run.importInfo.warningCount > 0 ? ` · ${run.importInfo.warningCount} 项提示` : ''}
            </summary>
            <dl>
              <dt>来源文件</dt>
              <dd>{run.importInfo.file}</dd>
              <dt>来源版本</dt>
              <dd>{run.importInfo.cliVersion || '未提供'}</dd>
              <dt>会话 ID</dt>
              <dd>{run.importInfo.sessionId}</dd>
              {run.importInfo.forkedFromId && (
                <>
                  <dt>分支来源</dt>
                  <dd>{run.importInfo.forkedFromId}</dd>
                </>
              )}
              <dt>SHA-256</dt>
              <dd>{run.importInfo.sha256}</dd>
            </dl>
            <p>按文件快照浏览；回合结束不代表整个会话成功。用量和执行耗时暂未映射。</p>
            {run.importInfo.warnings.length > 0 && (
              <ul>
                {run.importInfo.warnings.map((warning, index) => (
                  <li key={index}>
                    第 {warning.line} 行：{warning.message}
                  </li>
                ))}
              </ul>
            )}
            {run.importInfo.warningCount > run.importInfo.warnings.length && (
              <p>仅列出前 30 项提示。</p>
            )}
          </details>
        )}
        <div className="metrics">
          <div>
            <span>执行耗时</span>
            <strong>
              {run.durationMs === null ? '未知' : `${(run.durationMs / 1000).toFixed(0)} 秒`}
            </strong>
          </div>
          <div>
            <span>Token 用量</span>
            <strong>{run.tokens === null ? '未知' : run.tokens.toLocaleString('en-US')}</strong>
          </div>
          <div>
            <span>记录事件</span>
            <strong>
              {run.eventCount}
              <small> 条</small>
            </strong>
          </div>
          <div>
            <span>Jev 分析</span>
            <strong className="muted-metric">未运行</strong>
          </div>
        </div>
        <div className="timeline-heading">
          <h3>
            执行时间线 <span>{run.eventCount}</span>
          </h3>
          <span>保留来源顺序</span>
        </div>
        {error && (
          <div className="detail-error" role="alert">
            {error}
            <button
              className="text-button"
              onClick={() => (nextOffset !== null ? void loadMore() : setAttempt((n) => n + 1))}
            >
              重试加载
            </button>
          </div>
        )}
        <div className="timeline" aria-busy={loading}>
          {events.map((event) => (
            <div className={`event ${event.kind}`} key={event.id}>
              <span className="event-marker">
                <Icon
                  name={
                    event.kind === 'error'
                      ? 'alert'
                      : event.kind === 'lifecycle'
                        ? 'info'
                        : event.kind === 'verification'
                          ? 'check'
                          : event.role === 'user'
                            ? 'user'
                            : event.role === 'assistant'
                              ? 'agent'
                              : 'terminal'
                  }
                />
              </span>
              <div className="event-body">
                <div className="event-topline">
                  <span className="event-role">
                    {event.role === 'user'
                      ? '用户'
                      : event.role === 'assistant'
                        ? 'Agent'
                        : kindLabels[event.kind]}
                  </span>
                  <time>{event.timestamp ? event.timestamp.slice(11, 19) : '时间未知'}</time>
                  <span>#{event.sequence.toString().padStart(2, '0')}</span>
                </div>
                {event.kind === 'message' ? (
                  <>
                    <h4>{event.title}</h4>
                    <p className="message-content">{event.content}</p>
                  </>
                ) : (
                  <details>
                    <summary>
                      <Icon name="chevron" className="disclosure-icon" />
                      <span>{event.title}</span>
                      <span className="expand-label">展开输出</span>
                    </summary>
                    <pre>{event.content}</pre>
                  </details>
                )}
                <button
                  className="evidence-link"
                  onClick={() => setEvidence(evidence?.id === event.id ? undefined : event)}
                  aria-expanded={evidence?.id === event.id}
                >
                  <Icon name="link" />
                  查看来源证据
                </button>
                {evidence?.id === event.id && (
                  <div className="evidence-panel">
                    <strong>{run.demo ? '合成样本引用' : '来源文件引用'}</strong>
                    <dl>
                      <dt>事件 ID</dt>
                      <dd>{event.id}</dd>
                      <dt>位置</dt>
                      <dd>
                        {event.evidence.location} : {event.evidence.line}
                      </dd>
                      <dt>父事件</dt>
                      <dd>{event.parentId ?? '无'}</dd>
                    </dl>
                    <p>
                      {run.demo
                        ? '引用指向内置合成记录的事件序号，不对应真实文件行号。'
                        : '行号对应导入时的文件快照；文件之后可能变化，可用导入信息中的摘要核对。'}
                    </p>
                  </div>
                )}
              </div>
            </div>
          ))}
          {loading && (
            <p className="loading-label" role="status">
              正在加载事件…
            </p>
          )}
          {nextOffset !== null && (
            <button className="button load-more" onClick={loadMore} disabled={loading}>
              加载更多事件
            </button>
          )}
          {!loading && nextOffset === null && !error && (
            <div className="timeline-end">
              记录结束{run.status === 'unknown' ? ' · 完成状态未提供' : ''}
            </div>
          )}
        </div>
      </div>
    </article>
  )
}
