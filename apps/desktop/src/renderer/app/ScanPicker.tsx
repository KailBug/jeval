import { useEffect, useRef, useState } from 'react'
import type { ScanCandidate, ScanStatus } from '../../../../../contracts/index'

export function ScanPicker({
  scan,
  onStatus,
  onClose,
  onCancel,
  statusError,
  onReconnect,
  reconnecting
}: {
  scan: ScanStatus
  onStatus: (scan: ScanStatus) => void
  onClose: () => void
  onCancel: () => void
  statusError: string
  onReconnect: () => void
  reconnecting: boolean
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [candidates, setCandidates] = useState<ScanCandidate[]>([])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [focused, setFocused] = useState<string>()
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const active = scan.state === 'running' || scan.state === 'cancelling'
  const busy = active || submitting

  useEffect(() => {
    const element = dialog.current!
    element.showModal()
    return () => element.close()
  }, [])

  useEffect(() => {
    if (active) return
    let current = true
    setLoading(true)
    const load = async () => {
      const items: ScanCandidate[] = []
      let offset: number | null = 0
      while (offset !== null) {
        const page = await window.jeval.scanCandidates(scan.id, offset)
        if (!current) return
        items.push(...page.items)
        offset = page.nextOffset
      }
      setCandidates(items)
      setFocused((id) => (items.some((item) => item.id === id) ? id : items[0]?.id))
      setSelected(
        (ids) =>
          new Set(items.filter((item) => ids.has(item.id) && !item.imported).map((item) => item.id))
      )
      setError('')
    }
    void load()
      .catch((e) => {
        if (current) setError(cleanError(e))
      })
      .finally(() => {
        if (current) setLoading(false)
      })
    return () => {
      current = false
    }
  }, [scan.id, scan.phase, active, revision])

  const eligible = candidates.filter((item) => !item.imported)
  const allSelected = eligible.length > 0 && eligible.every((item) => selected.has(item.id))
  const visible = candidates.filter((item) =>
    `${item.title} ${item.project} ${item.path}`.toLowerCase().includes(search.trim().toLowerCase())
  )
  const preview = candidates.find((item) => item.id === focused)

  async function importSelected() {
    setSubmitting(true)
    setError('')
    try {
      onStatus(await window.jeval.importScanSelection(scan.id, [...selected]))
    } catch (e) {
      setError(cleanError(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <dialog
      ref={dialog}
      className="scan-dialog"
      aria-labelledby="scan-picker-title"
      onCancel={(event) => {
        event.preventDefault()
        if (!busy) onClose()
      }}
    >
      <header className="scan-dialog-header">
        <div>
          <h2 id="scan-picker-title">选择要导入的记录</h2>
          <p>
            {scan.phase === 'discovery'
              ? '扫描结果尚未加入任务库。浏览后勾选需要的记录，或全选导入。'
              : scan.message}
          </p>
        </div>
        <button className="text-button" onClick={onClose} disabled={busy}>
          关闭选择
        </button>
      </header>
      <p className="scan-dialog-root">{scan.root}</p>
      {scan.phase === 'discovery' && scan.state !== 'completed' && (
        <p className="scan-dialog-message">{scan.message}</p>
      )}
      <div className="scan-picker-tools">
        <input
          aria-label="搜索待导入记录"
          placeholder="搜索标题、项目或路径"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          maxLength={200}
        />
        <label>
          <input
            type="checkbox"
            aria-label="全选记录"
            checked={allSelected}
            disabled={busy || loading || eligible.length === 0}
            onChange={(event) =>
              setSelected(new Set(event.target.checked ? eligible.map((item) => item.id) : []))
            }
          />
          全选
        </label>
        <span>
          {visible.length} / {candidates.length} 条
        </span>
      </div>
      {error && (
        <div className="scan-dialog-message" role="alert">
          {error}{' '}
          <button className="text-button" disabled={busy} onClick={() => setRevision((n) => n + 1)}>
            重新加载候选
          </button>
        </div>
      )}
      {statusError && (
        <div className="scan-dialog-message" role="alert">
          无法更新导入状态：{statusError}
          <button className="text-button" onClick={onReconnect} disabled={reconnecting}>
            重新连接引擎
          </button>
        </div>
      )}
      <div className="scan-picker-body" aria-busy={loading}>
        <div className="scan-candidate-list" aria-label="待导入记录">
          {loading ? (
            <p className="loading-label">正在加载候选记录…</p>
          ) : visible.length === 0 ? (
            <p className="loading-label">
              {candidates.length ? '没有匹配的候选记录' : '没有发现可导入的记录'}
            </p>
          ) : (
            visible.map((item) => (
              <div
                className={`scan-candidate ${focused === item.id ? 'focused' : ''}`}
                key={item.id}
              >
                <input
                  type="checkbox"
                  aria-label={`选择 ${item.title}`}
                  checked={selected.has(item.id)}
                  disabled={busy || item.imported}
                  onChange={(event) =>
                    setSelected((ids) => {
                      const next = new Set(ids)
                      if (event.target.checked) next.add(item.id)
                      else next.delete(item.id)
                      return next
                    })
                  }
                />
                <button
                  className="candidate-preview"
                  onClick={() => setFocused(item.id)}
                  aria-pressed={focused === item.id}
                >
                  <strong>{item.title}</strong>
                  <span>{item.project}</span>
                  <small>
                    {item.imported ? '已导入' : item.existing ? '将更新已有记录' : '新记录'} ·{' '}
                    {item.eventCount} 个事件
                    {item.warningCount > 0 ? ` · ${item.warningCount} 项解析提示` : ''}
                  </small>
                </button>
              </div>
            ))
          )}
        </div>
        <section className="scan-candidate-detail" aria-label="候选记录概要">
          {preview ? (
            <>
              <h3>{preview.title}</h3>
              <dl>
                <dt>项目</dt>
                <dd>{preview.project}</dd>
                <dt>记录时间</dt>
                <dd>
                  {preview.startedAt
                    ? new Date(preview.startedAt).toLocaleString('zh-CN')
                    : '时间未知'}
                </dd>
                <dt>来源文件</dt>
                <dd>{preview.path}</dd>
              </dl>
              <h4>首条消息预览</h4>
              <pre>{preview.preview || '没有可预览的消息'}</pre>
              {preview.error && <p role="alert">{preview.error}</p>}
            </>
          ) : (
            <p>选择一条记录查看概要</p>
          )}
        </section>
      </div>
      {scan.issues.length > 0 && (
        <details className="scan-picker-issues">
          <summary>查看失败项（{scan.failed}）</summary>
          <ul>
            {scan.issues.map((issue, i) => (
              <li key={i}>
                {issue.path}
                <br />
                {issue.message}
              </li>
            ))}
          </ul>
        </details>
      )}
      <footer className="scan-dialog-footer">
        <span>已选 {selected.size} 条 · 本地任务库最多保存 20 条记录 / 50,000 个事件</span>
        {busy ? (
          <>
            <span role="status">
              {scan.message} · 新增 {scan.imported} · 更新 {scan.updated}
            </span>
            <button
              className="button"
              disabled={submitting || scan.state === 'cancelling'}
              onClick={onCancel}
            >
              取消导入
            </button>
          </>
        ) : (
          <button
            className="button"
            disabled={loading || selected.size === 0}
            onClick={importSelected}
          >
            导入所选（{selected.size}）
          </button>
        )}
      </footer>
    </dialog>
  )
}

function cleanError(error: unknown) {
  return (error instanceof Error ? error.message : String(error))
    .replace(/^Error invoking remote method '[^']+': (?:Error: )?/, '')
    .replace(/^[A-Z_]+:\s*/, '')
}
