import { useEffect, useState } from 'react'
import { Icon } from '../components/Icon'
import { useBrowseHistory } from './browse-history'
import { EventContent } from './EventContent'
import { ScanPicker } from './ScanPicker'
import { Annotations } from './Annotations'
import { SnapshotPicker } from './SnapshotPicker'
import { Comparison } from './Comparison'
import logo from '../../../resources/jeval.svg'
import type {
  Hello,
  CodexDirectory,
  ExportResult,
  RecordExportFormat,
  Page,
  Run,
  RunEvent,
  RunStatus,
  ScanStatus,
  UpdateStatus
} from '../../../../../contracts/index'

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
const errorMessage = (error: unknown) =>
  (error instanceof Error ? error.message : String(error))
    .replace(/^Error invoking remote method '[^']+': (?:Error: )?/, '')
    .replace(/^IMPORT_(?:FAILED|LIMIT):\s*/, '')

function Status({ status }: { status: RunStatus }) {
  return (
    <span className={`status ${status}`}>
      <span aria-hidden="true" />
      {statusLabels[status]}
    </span>
  )
}

export function App() {
  const [comparisonOpen, setComparisonOpen] = useState(false)
  const [comparisonInitial, setComparisonInitial] = useState<Run>()
  const [hello, setHello] = useState<Hello>()
  const [error, setError] = useState('')
  const history = useBrowseHistory()
  const { source, search, status, runId, offset = 0 } = history.location
  const { resolveSelection } = history
  const [runs, setRuns] = useState<Page<Run>>()
  const [runPageOffsets, setRunPageOffsets] = useState<number[]>([0])
  const runPageIndex = runPageOffsets.indexOf(offset)
  const previousRunOffset = runPageIndex > 0 ? runPageOffsets[runPageIndex - 1] : null
  const [selected, setSelected] = useState<Run>()
  const [loading, setLoading] = useState(true)
  const [revision, setRevision] = useState(0)
  const [reconnecting, setReconnecting] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importingRecord, setImportingRecord] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [importError, setImportError] = useState('')
  const [notice, setNotice] = useState('')
  const [scan, setScan] = useState<ScanStatus>()
  const [choosingDirectory, setChoosingDirectory] = useState(false)
  const [scanError, setScanError] = useState('')
  const [pickerOpen, setPickerOpen] = useState(false)
  const [directories, setDirectories] = useState<CodexDirectory[]>([])
  const [update, setUpdate] = useState<UpdateStatus>()
  const [updateError, setUpdateError] = useState('')
  const updating = update?.state === 'running' || update?.state === 'cancelling'
  const scanning = scan?.state === 'running' || scan?.state === 'cancelling'
  const busy = importing || exporting || choosingDirectory || reconnecting || updating

  useEffect(() => {
    if (!update || !updating) return
    let active = true
    let timer: ReturnType<typeof setTimeout>
    const id = update.id
    async function poll() {
      try {
        const next = await window.jeval.updateStatus(id)
        if (!active) return
        setUpdate(next)
        setUpdateError('')
        if (next.state === 'running' || next.state === 'cancelling') timer = setTimeout(poll, 200)
        else {
          if (next.state === 'failed') setImportError(next.message)
          else setNotice(next.message)
          setRevision((n) => n + 1)
        }
      } catch (e) {
        if (active) {
          setUpdateError(`${errorMessage(e)}；更新结果尚未确认，可重试取消或重新连接引擎。`)
          timer = setTimeout(poll, 1500)
        }
      }
    }
    void poll()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [update?.id, updating])

  async function cancelUpdate() {
    if (!update) return
    try {
      const next = await window.jeval.cancelUpdate(update.id)
      setUpdateError('')
      setUpdate(next)
      if (next.state !== 'running' && next.state !== 'cancelling') {
        if (next.state === 'failed') setImportError(next.message)
        else setNotice(next.message)
        setRevision((n) => n + 1)
      }
    } catch (e) {
      setUpdateError(errorMessage(e))
    }
  }

  useEffect(() => {
    setRunPageOffsets([0])
    history.update({ offset: 0 })
  }, [source, search, status, revision, history.update])

  useEffect(() => {
    if (!scan || !scanning) return
    let active = true
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const next = await window.jeval.scanStatus(scan.id)
        if (!active) return
        setScan(next)
        setScanError('')
        if (next.state === 'running' || next.state === 'cancelling') {
          timer = setTimeout(poll, 300)
        } else {
          if (next.phase === 'discovery') setPickerOpen(true)
          else setRevision((n) => n + 1)
        }
      } catch (e) {
        if (active) {
          setScanError(errorMessage(e))
          // Preserve the last known state; a timeout doesn't mean the job stopped.
          timer = setTimeout(poll, 1500)
        }
      }
    }
    void poll()
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [scan?.id, scan?.phase, scanning])

  async function scanCodex(directoryId?: string) {
    setChoosingDirectory(true)
    setImportError('')
    try {
      const result = await window.jeval.scanCodex(directoryId)
      if (!result) return
      setScan(result)
      setPickerOpen(false)
      setScanError('')
      setNotice('')
      void window.jeval
        .listCodexDirectories()
        .then((value) => setDirectories(value.items))
        .catch((e) => setImportError(errorMessage(e)))
    } catch (e) {
      setImportError(errorMessage(e))
    } finally {
      setChoosingDirectory(false)
    }
  }

  async function cancelScan() {
    if (!scan) return
    try {
      const next = await window.jeval.cancelScan(scan.id)
      setScan(next)
      if (next.state !== 'running' && next.state !== 'cancelling') {
        if (next.phase === 'discovery') setPickerOpen(true)
        else setRevision((n) => n + 1)
      }
    } catch (e) {
      setScanError(errorMessage(e))
    }
  }

  async function removeDirectory(id: string) {
    setImportError('')
    setChoosingDirectory(true)
    try {
      await window.jeval.removeCodexDirectory(id)
      setDirectories((await window.jeval.listCodexDirectories()).items)
      setNotice('已移除保存的目录；已导入的快照仍可浏览。')
    } catch (e) {
      setImportError(errorMessage(e))
    } finally {
      setChoosingDirectory(false)
    }
  }

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
    void window.jeval
      .listCodexDirectories()
      .then((value) => {
        if (active) setDirectories(value.items)
      })
      .catch((e) => {
        if (active) setImportError(errorMessage(e))
      })
    return () => {
      active = false
    }
  }, [hello, revision])

  useEffect(() => {
    setSelected(undefined)
    if (!hello || !runId) return
    let active = true
    void window.jeval
      .getRun(runId)
      .then((run) => {
        if (active) setSelected(run)
      })
      .catch((e) => {
        if (active) setError(errorMessage(e))
      })
    return () => {
      active = false
    }
  }, [hello, runId, revision])

  useEffect(() => {
    if (!hello) return
    let active = true
    setLoading(true)
    setRuns(undefined)
    const timer = setTimeout(() => {
      window.jeval
        .listRuns({ search, status, source, offset, limit: 10 })
        .then((page) => {
          if (!active) return
          // Encoded metadata can shorten a requested page. Keep actual page
          // boundaries for back navigation instead of subtracting the limit.
          setRunPageOffsets((old) =>
            [
              ...new Set([...old, offset, ...(page.nextOffset === null ? [] : [page.nextOffset])])
            ].sort((a, b) => a - b)
          )
          setRuns(page)
          resolveSelection(page.items.map((run) => run.id))
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
  }, [hello, search, status, source, offset, revision, resolveSelection])

  async function importCodex() {
    setImporting(true)
    setImportError('')
    try {
      const result = await window.jeval.importCodex()
      if (!result) return
      history.navigate({ source: 'codex', runId: result.run.id, search: '', status: 'all' })
      setNotice(`${result.replaced ? '已更新' : '已导入'}记录 · ${result.run.eventCount} 个事件`)
      setRevision((n) => n + 1)
    } catch (e) {
      setImportError(errorMessage(e))
    } finally {
      setImporting(false)
    }
  }

  async function updateCodex() {
    if (!selected || selected.demo || selected.readOnly) return
    setImporting(true)
    setImportError('')
    setNotice('')
    setUpdateError('')
    try {
      setUpdate(await window.jeval.updateCodex(selected.id))
    } catch (e) {
      setImportError(`${errorMessage(e)}；已保存的快照保留。`)
    } finally {
      setImporting(false)
    }
  }

  async function importRecord() {
    setImporting(true)
    setImportingRecord(true)
    setImportError('')
    setNotice('')
    try {
      const result = await window.jeval.importRecord()
      if (!result) return
      history.navigate({ source: 'codex', runId: result.run.id, search: '', status: 'all' })
      setNotice(`已导入 jeval 记录 · ${result.run.eventCount} 个事件`)
      setRevision((n) => n + 1)
    } catch (e) {
      setImportError(errorMessage(e))
    } finally {
      setImporting(false)
      setImportingRecord(false)
    }
  }

  async function exportRecord(format: RecordExportFormat): Promise<ExportResult | null> {
    if (!selected || selected.demo) return null
    setExporting(true)
    try {
      return await window.jeval.exportRecord(selected.id, format)
    } finally {
      setExporting(false)
    }
  }

  async function reconnect() {
    setReconnecting(true)
    setError('')
    try {
      setHello(await window.jeval.restartEngine())
      setScan(undefined)
      setUpdate(undefined)
      setUpdateError('')
      setPickerOpen(false)
      setScanError('')
      setNotice('引擎已重新连接，已导入的快照和保存的目录已恢复。')
      setRevision((n) => n + 1)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setReconnecting(false)
    }
  }

  return (
    <>
      {pickerOpen && scan && (
        <ScanPicker
          scan={scan}
          onStatus={(next) => {
            setScan(next)
            history.navigate({ source: 'codex', search: '', status: 'all' })
          }}
          onClose={() => setPickerOpen(false)}
          onCancel={cancelScan}
          statusError={scanError}
          onReconnect={reconnect}
          reconnecting={reconnecting}
        />
      )}
      <header
        className={`window-toolbar ${window.jeval.platform === 'darwin' ? 'mac-toolbar' : ''}`}
        aria-label="窗口导航"
      >
        <nav className="history-controls" aria-label="浏览历史">
          <button
            className="icon-button"
            aria-label="后退"
            title="后退"
            disabled={!history.canBack || loading || busy || comparisonOpen}
            onClick={() => history.move(-1)}
          >
            <Icon name="arrow-left" />
          </button>
          <button
            className="icon-button"
            aria-label="前进"
            title="前进"
            disabled={!history.canForward || loading || busy || comparisonOpen}
            onClick={() => history.move(1)}
          >
            <Icon name="arrow-right" />
          </button>
        </nav>
      </header>
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
            disabled={busy || comparisonOpen}
            onClick={() =>
              source !== 'demo' && history.navigate({ source: 'demo', search: '', status: 'all' })
            }
          >
            <Icon name="folder" />
            <span>合成演示</span>
            <span className="source-count">3</span>
          </button>
          <button
            className={'source-item ' + (source === 'codex' ? 'selected' : '')}
            aria-pressed={source === 'codex'}
            disabled={busy || comparisonOpen}
            onClick={() =>
              source !== 'codex' && history.navigate({ source: 'codex', search: '', status: 'all' })
            }
          >
            <Icon name="terminal" />
            <span>Codex</span>
            <span className="source-count">本地</span>
          </button>
          <button
            className="button sidebar-import"
            onClick={importRecord}
            disabled={!hello || busy || scanning || comparisonOpen}
          >
            <Icon name="folder" />
            {importingRecord ? '正在导入 jeval…' : '导入 jeval 记录'}
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
              <h1>{comparisonOpen ? '手动对比' : '任务库'}</h1>
              <span className="preview-label">
                {comparisonOpen ? '已保存快照' : source === 'demo' ? '演示空间' : 'Codex 记录'}
              </span>
            </div>
            <div className="topbar-actions">
              {!comparisonOpen && (
                <>
                  <button
                    className="button"
                    onClick={() => void scanCodex()}
                    disabled={!hello || busy || scanning}
                  >
                    <Icon name="search" />
                    {choosingDirectory ? '正在选择…' : '发现本地任务'}
                  </button>
                  <button
                    className="button"
                    onClick={importCodex}
                    disabled={!hello || busy || scanning}
                  >
                    <Icon name="folder" />
                    {importing ? '正在导入…' : '导入 Codex 记录'}
                  </button>
                  <button
                    className="button"
                    onClick={() => setRevision((n) => n + 1)}
                    disabled={loading || busy}
                  >
                    <Icon name="refresh" />
                    刷新记录
                  </button>
                  <button
                    className="button"
                    disabled={!hello || busy || scanning}
                    onClick={() => {
                      setComparisonInitial(selected && !selected.demo ? selected : undefined)
                      setComparisonOpen(true)
                    }}
                  >
                    手动对比
                  </button>
                </>
              )}
            </div>
          </header>
          {comparisonOpen ? (
            <Comparison initial={comparisonInitial} onClose={() => setComparisonOpen(false)} />
          ) : (
            <>
              <div className="demo-banner">
                <Icon name="info" />
                <span>
                  {source === 'demo'
                    ? '当前为合成演示。可选择 Codex 文件或目录，查看本地执行记录。'
                    : selected?.readOnly
                      ? '交换快照保存在本地，不会读取其中包含的来源路径；显式重新导入原始 Codex 文件后可更新。'
                      : '已导入快照保存在本地，重启或源文件移走后仍可浏览；手动更新会重读已登记的来源文件。'}
                </span>
              </div>
              {source === 'codex' && (
                <details className="directory-panel">
                  <summary>保存的 Codex 目录 · {directories.length}</summary>
                  <p>
                    启动时不会自动扫描。重新发现后，选择要导入的记录；移除目录保留已导入的快照。
                  </p>
                  {directories.length === 0 && (
                    <p>通过“发现本地任务”选择的有效目录会保存在这里。</p>
                  )}
                  <ul>
                    {directories.map((directory) => (
                      <li key={directory.id}>
                        <span title={directory.path}>{directory.path}</span>
                        <button
                          className="text-button"
                          onClick={() => void scanCodex(directory.id)}
                          disabled={!hello || busy || scanning}
                          aria-label={`重新发现 ${directory.path}`}
                        >
                          重新发现
                        </button>
                        <button
                          className="text-button"
                          onClick={() => void removeDirectory(directory.id)}
                          disabled={!hello || busy || scanning}
                          aria-label={`移除目录 ${directory.path}`}
                        >
                          移除目录
                        </button>
                      </li>
                    ))}
                  </ul>
                </details>
              )}
              {scan && (
                <section className="scan-panel" aria-label="Codex 目录扫描">
                  <div className="scan-summary">
                    <strong role="status">{scan.message}</strong>
                    {scanning ? (
                      <button
                        className="text-button"
                        onClick={cancelScan}
                        disabled={scan.state === 'cancelling'}
                      >
                        {scan.state === 'cancelling'
                          ? '正在取消…'
                          : scan.phase === 'import'
                            ? '取消导入'
                            : '取消扫描'}
                      </button>
                    ) : (
                      <div>
                        <button className="text-button" onClick={() => setPickerOpen(true)}>
                          浏览扫描结果
                        </button>
                        <button className="text-button" onClick={() => setScan(undefined)}>
                          关闭扫描结果
                        </button>
                      </div>
                    )}
                  </div>
                  <p className="scan-root" title={scan.root}>
                    {scan.root}
                  </p>
                  <p>
                    已检查 {scan.visited} 项 · 可选 {scan.ready} 条 · 新增 {scan.imported} · 更新{' '}
                    {scan.updated} · 失败 {scan.failed} · 跳过链接/特殊文件 {scan.skipped}
                  </p>
                  {scanError && (
                    <div role="alert">
                      无法更新扫描状态：{scanError}
                      <button className="text-button" onClick={reconnect} disabled={reconnecting}>
                        重新连接引擎
                      </button>
                    </div>
                  )}
                  {scan.issues.length > 0 && (
                    <details>
                      <summary>
                        查看失败项（显示 {scan.issues.length} / {scan.failed}）
                      </summary>
                      <ul>
                        {scan.issues.map((issue, index) => (
                          <li key={index}>
                            <span>{issue.path}</span>
                            <br />
                            {issue.message}
                          </li>
                        ))}
                      </ul>
                    </details>
                  )}
                </section>
              )}
              {(updating || updateError) && (
                <section className="import-notice" aria-label="记录更新">
                  <span role="status">{update?.message}</span>
                  <button
                    className="text-button"
                    onClick={() => void cancelUpdate()}
                    disabled={update?.state === 'cancelling'}
                  >
                    {update?.state === 'cancelling' ? '正在取消更新…' : '取消更新'}
                  </button>
                  {updateError && <span role="alert">{updateError}</span>}
                  {updateError && (
                    <button
                      className="text-button"
                      onClick={() => void reconnect()}
                      disabled={reconnecting}
                    >
                      重新连接引擎
                    </button>
                  )}
                </section>
              )}
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
                          disabled={busy}
                          onChange={(event) =>
                            history.update({
                              status: event.target.value as RunStatus | 'all',
                              offset: 0,
                              runId: undefined
                            })
                          }
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
                          disabled={busy}
                          onChange={(event) =>
                            history.update({
                              search: event.target.value,
                              offset: 0,
                              runId: undefined
                            })
                          }
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
                              history.update({
                                search: '',
                                status: 'all',
                                offset: 0,
                                runId: undefined
                              })
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
                          disabled={busy}
                          onClick={() => history.navigate({ ...history.location, runId: run.id })}
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
                    <nav className="page-controls run-pages" aria-label="任务分页">
                      <button
                        className="button"
                        disabled={loading || busy || previousRunOffset === null}
                        onClick={() => history.update({ offset: previousRunOffset ?? 0 })}
                      >
                        上一页任务
                      </button>
                      <span>
                        {runs?.total
                          ? `${offset + 1}–${offset + runs.items.length} / ${runs.total}`
                          : '0 条'}
                      </span>
                      <button
                        className="button"
                        disabled={loading || busy || runs?.nextOffset == null}
                        onClick={() => history.update({ offset: runs?.nextOffset ?? offset })}
                      >
                        下一页任务
                      </button>
                    </nav>
                    <div className="list-footer">
                      <Icon name="monitor" />
                      <span>本地浏览，无需 API key</span>
                    </div>
                  </section>
                  {selected ? (
                    <SnapshotDetail
                      key={selected.id + ':' + (selected.importInfo?.sha256 ?? '') + ':' + revision}
                      run={selected}
                      onUpdate={updateCodex}
                      updating={importing || !!updating}
                      updateDisabled={choosingDirectory || !!scanning || reconnecting || exporting}
                      onExport={exportRecord}
                      exportDisabled={busy || !!scanning}
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
            </>
          )}
        </main>
      </div>
    </>
  )
}

function SnapshotDetail(props: Parameters<typeof RunDetail>[0]) {
  const [snapshot, setSnapshot] = useState(props.run)
  const [dirty, setDirty] = useState(false)
  const choose = (run: Run) => {
    if (!dirty || window.confirm('切换快照会丢弃未保存的标注草稿。继续吗？')) {
      setDirty(false)
      setSnapshot(run)
    }
  }
  if (props.run.demo || !props.run.importInfo) return <RunDetail {...props} />
  const historical = snapshot.importInfo!.snapshotId !== props.run.importInfo.snapshotId
  return (
    <div className="snapshot-detail">
      <SnapshotPicker current={props.run} selected={snapshot} onSelect={choose} />
      {historical && (
        <p className="historical-notice">正在浏览历史快照，事件和标注保留该版本的证据。</p>
      )}
      <RunDetail
        {...props}
        key={snapshot.importInfo!.snapshotId}
        run={snapshot}
        updateDisabled={props.updateDisabled || historical}
        exportDisabled={props.exportDisabled || historical}
        onAnnotationDirty={setDirty}
      />
    </div>
  )
}

export function RunDetail({
  run,
  onUpdate,
  updating,
  updateDisabled,
  onExport,
  exportDisabled,
  onAnnotationDirty,
  reviewMode = false
}: {
  run: Run
  onUpdate(): Promise<void>
  updating: boolean
  updateDisabled: boolean
  onExport(format: RecordExportFormat): Promise<ExportResult | null>
  exportDisabled: boolean
  onAnnotationDirty?: (dirty: boolean) => void
  reviewMode?: boolean
}) {
  const [annotationEvent, setAnnotationEvent] = useState<RunEvent | null>(null)
  const [annotationDirty, setAnnotationDirty] = useState(false)
  const chooseAnnotation = (event: RunEvent | null) => {
    if (!annotationDirty || window.confirm('切换标注目标会丢弃未保存的草稿。继续吗？')) {
      setAnnotationDirty(false)
      setAnnotationEvent(event)
    }
  }
  const [events, setEvents] = useState<RunEvent[]>([])
  const [nextOffset, setNextOffset] = useState<number | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [evidence, setEvidence] = useState<RunEvent>()
  const [fullContent, setFullContent] = useState<string>()
  const [attempt, setAttempt] = useState(0)
  const [eventSearch, setEventSearch] = useState('')
  const [eventKind, setEventKind] = useState<RunEvent['kind'] | 'all'>('all')
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [previousOffsets, setPreviousOffsets] = useState<number[]>([])
  const [exportFormat, setExportFormat] = useState<RecordExportFormat>()
  const [exportMessage, setExportMessage] = useState('')
  const [exportError, setExportError] = useState('')
  const filtered = eventSearch.trim() !== '' || eventKind !== 'all'

  useEffect(() => {
    let active = true
    setLoading(true)
    setEvents([])
    setNextOffset(null)
    setEvidence(undefined)
    setFullContent(undefined)
    setError('')
    const timer = setTimeout(() => {
      void window.jeval
        .listEvents({
          runId: run.id,
          ...(run.importInfo ? { snapshotId: run.importInfo.snapshotId } : {}),
          search: eventSearch,
          kind: eventKind,
          offset,
          limit: 50
        })
        .then((page) => {
          if (active) {
            setEvents(page.items)
            setNextOffset(page.nextOffset)
            setTotal(page.total)
            setError('')
          }
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
  }, [run.id, eventSearch, eventKind, offset, attempt])

  function nextPage() {
    if (nextOffset === null || loading) return
    setPreviousOffsets((old) => [...old, offset])
    setOffset(nextOffset)
    setLoading(true)
  }

  function firstPage() {
    setOffset(0)
    setPreviousOffsets([])
    setLoading(true)
    setNextOffset(null)
  }

  async function exportSnapshot(format: RecordExportFormat) {
    setExportFormat(format)
    setExportMessage('')
    setExportError('')
    try {
      const result = await onExport(format)
      if (result)
        setExportMessage(
          `已导出 ${result.format === 'json' ? 'JSON' : 'Markdown'} · ${result.eventCount} 个事件 · ${result.path}`
        )
    } catch (e) {
      setExportError(errorMessage(e))
    } finally {
      setExportFormat(undefined)
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
            <span>
              {run.demo ? '合成演示记录' : run.readOnly ? '只读交换快照' : 'Codex 本地记录'}
            </span>
            {!run.demo && !reviewMode && (
              <button
                className="button"
                onClick={onUpdate}
                disabled={run.readOnly || updating || updateDisabled}
                title={
                  run.readOnly
                    ? '交换快照未授权读取原始路径；显式导入 Codex 原始文件后可更新'
                    : undefined
                }
              >
                <Icon name="refresh" />
                {updating ? '正在更新…' : '更新已登记记录'}
              </button>
            )}
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
              <dt>记录模式</dt>
              <dd>{run.importInfo.historyMode}</dd>
              <dt>会话 ID</dt>
              <dd>{run.importInfo.sessionId}</dd>
              {run.importInfo.parentThreadId && (
                <>
                  <dt>父线程</dt>
                  <dd>{run.importInfo.parentThreadId}</dd>
                </>
              )}
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
        {!run.demo && (
          <p className="snapshot-completeness">
            时间线和搜索仅使用每条正文最多 8 KiB
            的预览。点击“查看完整正文”可读取已保存的标准化文本；旧记录或交换文件可能缺少正文。未保存原始文件和附件。
          </p>
        )}
        {!run.demo && run.importInfo && !reviewMode && (
          <section className="record-export" aria-label="导出已保存快照">
            <h3>导出已保存快照</h3>
            <p>
              包含当前快照的全部 {run.eventCount} 个事件，包括其他页面及筛选外的事件。JSON 可再导入
              jeval，Markdown 便于阅读。
            </p>
            <p>
              每条正文最多 8
              KiB，不含另存的完整正文、原始文件备份、分析或标注。导出保留来源路径，分享前请检查。
            </p>
            <p className="export-source-path">
              来源路径：<code>{run.importInfo.file}</code>
            </p>
            <div className="record-export-actions">
              <button
                className="button"
                disabled={exportDisabled || !!exportFormat}
                onClick={() => void exportSnapshot('json')}
              >
                {exportFormat === 'json' ? '正在导出 JSON…' : '导出 JSON'}
              </button>
              <button
                className="button"
                disabled={exportDisabled || !!exportFormat}
                onClick={() => void exportSnapshot('markdown')}
              >
                {exportFormat === 'markdown' ? '正在导出 Markdown…' : '导出 Markdown'}
              </button>
            </div>
            {exportMessage && (
              <p className="export-success" role="status">
                {exportMessage}
              </p>
            )}
            {exportError && (
              <p className="detail-error" role="alert">
                {exportError}
              </p>
            )}
          </section>
        )}
        {!run.demo && run.importInfo && (
          <>
            <button className="button" onClick={() => chooseAnnotation(null)}>
              标注整个快照
            </button>
            <Annotations
              key={annotationEvent?.id ?? 'snapshot'}
              run={run}
              event={annotationEvent}
              onSelect={chooseAnnotation}
              onDirty={(dirty) => {
                setAnnotationDirty(dirty)
                onAnnotationDirty?.(dirty)
              }}
            />
          </>
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
        <div className="event-filters">
          <input
            aria-label="搜索记录内容"
            placeholder="搜索消息、工具或输出"
            value={eventSearch}
            maxLength={200}
            onChange={(event) => {
              setEventSearch(event.target.value)
              firstPage()
            }}
          />
          <select
            aria-label="筛选事件类型"
            value={eventKind}
            onChange={(event) => {
              setEventKind(event.target.value as RunEvent['kind'] | 'all')
              firstPage()
            }}
          >
            <option value="all">全部事件</option>
            {Object.entries(kindLabels).map(([kind, label]) => (
              <option key={kind} value={kind}>
                {label}
              </option>
            ))}
          </select>
        </div>
        <p className="event-search-hint">
          搜索范围为当前快照中的正文预览；已保存的截断部分可按需查看，不参与搜索。
        </p>
        {filtered && !loading && !error && (
          <p className="event-match-count" role="status">
            匹配 {total} 个事件 · 保留原始序号
          </p>
        )}
        {error && (
          <div className="detail-error" role="alert">
            {error}
            <button className="text-button" onClick={() => setAttempt((n) => n + 1)}>
              重试加载
            </button>
          </div>
        )}
        <div className="timeline" aria-busy={loading}>
          {!loading && !error && events.length === 0 && (
            <div className="event-search-empty">
              <p>{filtered ? '没有匹配的事件' : '这条记录没有可展示的事件'}</p>
              {filtered && (
                <button
                  className="text-button"
                  onClick={() => {
                    setEventSearch('')
                    setEventKind('all')
                    firstPage()
                  }}
                >
                  清除事件筛选
                </button>
              )}
            </div>
          )}
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
                {!run.demo && event.evidence.snapshotId && (
                  <>
                    <button
                      className="evidence-link"
                      aria-expanded={fullContent === event.id}
                      onClick={() =>
                        setFullContent(fullContent === event.id ? undefined : event.id)
                      }
                    >
                      {fullContent === event.id ? '收起完整正文' : '查看完整正文'}
                    </button>
                    {fullContent === event.id && (
                      <EventContent key={event.id} runId={run.id} event={event} />
                    )}
                  </>
                )}
                {!run.demo && (
                  <button className="evidence-link" onClick={() => chooseAnnotation(event)}>
                    标注此事件
                  </button>
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
          <nav className="page-controls event-pages" aria-label="事件分页">
            <button
              className="button"
              disabled={loading || previousOffsets.length === 0}
              onClick={() => {
                setOffset(previousOffsets[previousOffsets.length - 1])
                setPreviousOffsets((old) => old.slice(0, -1))
                setLoading(true)
              }}
            >
              上一页事件
            </button>
            <span>{total ? `${offset + 1}–${offset + events.length} / ${total}` : '0 条'}</span>
            <button className="button" onClick={nextPage} disabled={loading || nextOffset === null}>
              下一页事件
            </button>
          </nav>
          {!loading && nextOffset === null && !error && (
            <div className="timeline-end">
              {filtered
                ? '筛选结果结束'
                : `记录结束${run.status === 'unknown' ? ' · 完成状态未提供' : ''}`}
            </div>
          )}
        </div>
      </div>
    </article>
  )
}
