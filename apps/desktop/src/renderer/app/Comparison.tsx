import { useEffect, useState } from 'react'
import type { Page, Run } from '../../../../../contracts/index'
import { RunDetail } from './App'
import { SnapshotPicker } from './SnapshotPicker'

function RecordChooser({
  side,
  onSelect,
  disabled
}: {
  side: '左' | '右'
  onSelect(run: Run): void
  disabled: boolean
}) {
  const [search, setSearch] = useState(''),
    [offset, setOffset] = useState(0),
    [previous, setPrevious] = useState<number[]>([]),
    [page, setPage] = useState<Page<Run>>(),
    [error, setError] = useState(''),
    [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let active = true
    setPage(undefined)
    setError('')
    const timer = setTimeout(() => {
      window.jeval
        .listRuns({ source: 'codex', search, offset, limit: 10 })
        .then((p) => {
          if (active) setPage(p)
        })
        .catch((e) => {
          if (active) setError(String(e))
        })
    }, 150)
    return () => {
      active = false
      clearTimeout(timer)
    }
  }, [search, offset, attempt])
  return (
    <section className="comparison-chooser" aria-label={`选择${side}侧记录`}>
      <input
        aria-label={`搜索${side}侧记录`}
        placeholder="搜索已保存记录"
        value={search}
        maxLength={200}
        disabled={disabled}
        onChange={(e) => {
          setSearch(e.target.value)
          setOffset(0)
          setPrevious([])
        }}
      />
      {error && (
        <p role="alert">
          {error}
          <button className="text-button" onClick={() => setAttempt((n) => n + 1)}>
            重试记录列表
          </button>
        </p>
      )}
      {!page && !error && <p role="status">正在加载记录…</p>}
      {page?.items.map((run) => (
        <button
          className="comparison-record"
          key={run.id}
          disabled={disabled}
          onClick={() => onSelect(run)}
        >
          <strong>{run.title}</strong>
          <span>
            {run.project} · {run.eventCount} 个事件
          </span>
        </button>
      ))}
      {page?.total === 0 && <p>没有匹配的已保存记录，请先导入。</p>}
      <nav className="annotation-actions" aria-label={`${side}侧记录分页`}>
        <button
          className="button"
          disabled={disabled || !page || !previous.length}
          onClick={() => {
            setOffset(previous[previous.length - 1])
            setPrevious((p) => p.slice(0, -1))
          }}
        >
          上一页记录
        </button>
        <span>{page?.total ?? '…'} 条记录</span>
        <button
          className="button"
          disabled={disabled || !page || page.nextOffset === null}
          onClick={() => {
            setPrevious((p) => [...p, offset])
            setOffset(page!.nextOffset!)
          }}
        >
          下一页记录
        </button>
      </nav>
    </section>
  )
}

function Side({
  side,
  initial,
  onChange,
  onDirty,
  disabled
}: {
  side: '左' | '右'
  initial?: Run
  onChange(run: Run | undefined): void
  onDirty(dirty: boolean): void
  disabled: boolean
}) {
  const [current, setCurrent] = useState(initial),
    [snapshot, setSnapshot] = useState(initial),
    [choosing, setChoosing] = useState(!initial),
    [dirty, setDirty] = useState(false)
  const choose = (run: Run, record = false) => {
    if (!dirty || window.confirm('更换记录或快照会丢弃该侧未保存的标注草稿。继续吗？')) {
      setDirty(false)
      onDirty(false)
      setSnapshot(run)
      if (record) {
        setCurrent(run)
        setChoosing(false)
      }
      onChange(run)
    }
  }
  return (
    <section className="comparison-pane" aria-label={`${side}侧记录`}>
      <header className="comparison-side-header">
        <strong>{side}侧</strong>
        <button className="button" disabled={disabled} onClick={() => setChoosing((v) => !v)}>
          {choosing ? '收起记录选择' : `选择${side}侧记录`}
        </button>
      </header>
      {choosing && (
        <RecordChooser side={side} disabled={disabled} onSelect={(run) => choose(run, true)} />
      )}
      {current && snapshot && (
        <>
          <SnapshotPicker
            current={current}
            selected={snapshot}
            onSelect={(run) => {
              if (!disabled) choose(run)
            }}
          />
          <p className="comparison-snapshot">
            已固定快照 <code>{snapshot.importInfo!.snapshotId}</code>
          </p>
          <RunDetail
            key={snapshot.importInfo!.snapshotId}
            run={snapshot}
            onUpdate={async () => {}}
            updating={false}
            updateDisabled
            exportDisabled
            onExport={async () => null}
            reviewMode
            onAnnotationDirty={(value) => {
              setDirty(value)
              onDirty(value)
            }}
          />
        </>
      )}
      {!snapshot && (
        <div className="comparison-empty">选择一份已保存记录开始浏览。两侧独立翻页与滚动。</div>
      )}
    </section>
  )
}

export function Comparison({ initial, onClose }: { initial?: Run; onClose(): void }) {
  const [left, setLeft] = useState(initial),
    [right, setRight] = useState<Run>(),
    [leftDirty, setLeftDirty] = useState(false),
    [rightDirty, setRightDirty] = useState(false),
    [exporting, setExporting] = useState(false),
    [notice, setNotice] = useState(''),
    [error, setError] = useState('')
  async function save(format: 'json' | 'markdown') {
    if (!left?.importInfo || !right?.importInfo) return
    setExporting(true)
    setNotice('')
    setError('')
    try {
      const result = await window.jeval.exportComparison({
        left: { runId: left.id, snapshotId: left.importInfo.snapshotId },
        right: { runId: right.id, snapshotId: right.importInfo.snapshotId },
        format
      })
      setNotice(
        result
          ? `已导出比较报告 · ${result.eventCounts[0]} / ${result.eventCounts[1]} 个事件`
          : '已取消比较报告导出'
      )
    } catch (e) {
      setError(String(e))
    } finally {
      setExporting(false)
    }
  }
  const metric = (l: number | null | undefined, r: number | null | undefined) => ({
    left: l ?? null,
    right: r ?? null,
    delta: l == null || r == null ? null : r - l
  })
  const rows = [
    { label: '执行耗时（毫秒）', ...metric(left?.durationMs, right?.durationMs) },
    { label: 'Token 用量', ...metric(left?.tokens, right?.tokens) },
    { label: '事件数量', ...metric(left?.eventCount, right?.eventCount) }
  ]
  const display = (n: number | null) => (n === null ? '未知' : n.toLocaleString('en-US'))
  return (
    <section className="comparison-view" aria-label="手动并排对比">
      <div className="comparison-toolbar">
        <div>
          <h2>手动并排对比</h2>
          <p>各自浏览所选快照；按证据手动定位，不自动对齐步骤。</p>
        </div>
        <div className="annotation-actions">
          <button
            className="button"
            disabled={exporting || !left || !right}
            onClick={() => void save('json')}
          >
            导出比较 JSON
          </button>
          <button
            className="button"
            disabled={exporting || !left || !right}
            onClick={() => void save('markdown')}
          >
            导出比较 Markdown
          </button>
          <button
            className="button"
            disabled={exporting}
            onClick={() => {
              if (
                !(leftDirty || rightDirty) ||
                window.confirm('返回任务库会丢弃未保存的标注草稿。继续吗？')
              )
                onClose()
            }}
          >
            返回任务库
          </button>
        </div>
      </div>
      <div className="comparison-summary">
        <table aria-label="指标比较">
          <thead>
            <tr>
              <th>指标</th>
              <th>左侧</th>
              <th>右侧</th>
              <th>右减左</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.label}>
                <th>{row.label}</th>
                <td>{display(row.left)}</td>
                <td>{display(row.right)}</td>
                <td>{display(row.delta)}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p>
          缺失指标保留未知，不推断性能收益。导出包含所选两份快照全部事件预览及已保存标注，过滤条件和页码不限制导出；不含另存全文、原始文件或未保存草稿。比较
          JSON 是报告格式。
        </p>
      </div>
      {notice && (
        <p className="comparison-notice" role="status">
          {notice}
        </p>
      )}
      {error && (
        <p className="comparison-notice detail-error" role="alert">
          {error}
        </p>
      )}
      <div className="comparison-columns">
        <Side
          side="左"
          initial={initial}
          onChange={setLeft}
          onDirty={setLeftDirty}
          disabled={exporting}
        />
        <Side side="右" onChange={setRight} onDirty={setRightDirty} disabled={exporting} />
      </div>
    </section>
  )
}
