import { useEffect, useState } from 'react'
import type { Page, Run } from '../../../../../contracts/index'

export function SnapshotPicker({
  current,
  selected,
  onSelect
}: {
  current: Run
  selected: Run
  onSelect(run: Run): void
}) {
  const [page, setPage] = useState<Page<Run>>(),
    [offset, setOffset] = useState(0),
    [previous, setPrevious] = useState<number[]>([]),
    [error, setError] = useState(''),
    [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let active = true
    setPage(undefined)
    setError('')
    window.jeval
      .listSnapshots(current.id, offset)
      .then((p) => {
        if (active) setPage(p)
      })
      .catch((e) => {
        if (active) setError(String(e))
      })
    return () => {
      active = false
    }
  }, [current.id, offset, attempt])
  const snapshotId = selected.importInfo!.snapshotId
  return (
    <section className="snapshot-picker" aria-label="快照历史">
      <div>
        <strong>快照历史</strong>
        <span>{page ? ` · ${page.total} 个已保存版本` : ' · 正在加载'}</span>
      </div>
      <select
        aria-label="选择记录快照"
        value={snapshotId}
        disabled={!page}
        onChange={(e) => {
          const run = page?.items.find((r) => r.importInfo!.snapshotId === e.target.value)
          if (run)
            onSelect(run.importInfo!.snapshotId === current.importInfo!.snapshotId ? current : run)
        }}
      >
        {!page?.items.some((r) => r.importInfo!.snapshotId === snapshotId) && (
          <option value={snapshotId}>当前选定 · {snapshotId.slice(-12)}</option>
        )}
        {page?.items.map((r) => (
          <option key={r.importInfo!.snapshotId} value={r.importInfo!.snapshotId}>
            {r.importInfo!.snapshotId === current.importInfo!.snapshotId ? '当前' : '历史'} ·{' '}
            {r.eventCount} 个事件 · {r.importInfo!.sha256.slice(0, 12)}
          </option>
        ))}
      </select>
      <div className="annotation-actions">
        <button
          className="button"
          disabled={!previous.length || !page}
          onClick={() => {
            setOffset(previous[previous.length - 1])
            setPrevious((p) => p.slice(0, -1))
          }}
        >
          上一页快照
        </button>
        <button
          className="button"
          disabled={!page || page.nextOffset === null}
          onClick={() => {
            setPrevious((p) => [...p, offset])
            setOffset(page!.nextOffset!)
          }}
        >
          下一页快照
        </button>
        <button
          className="button"
          disabled={snapshotId === current.importInfo!.snapshotId}
          onClick={() => onSelect(current)}
        >
          查看当前快照
        </button>
      </div>
      {error && (
        <p role="alert">
          {error}
          <button className="text-button" onClick={() => setAttempt((n) => n + 1)}>
            重试快照历史
          </button>
        </p>
      )}
    </section>
  )
}
