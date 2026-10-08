import { useEffect, useState } from 'react'
import type {
  Annotation,
  AnnotationJudgement,
  AnnotationTarget,
  Page,
  Run,
  RunEvent
} from '../../../../../contracts/index'

export const judgementLabels: Record<AnnotationJudgement, string> = {
  accepted: '接受',
  rejected: '否定',
  uncertain: '待确认'
}
export function Annotations({
  run,
  event,
  onSelect,
  onDirty
}: {
  run: Run
  event: RunEvent | null
  onSelect(event: RunEvent | null): void
  onDirty(dirty: boolean): void
}) {
  const snapshotId = run.importInfo!.snapshotId
  const target: AnnotationTarget = { runId: run.id, snapshotId, eventId: event?.id ?? null }
  const [saved, setSaved] = useState<Annotation | null>(null)
  const [note, setNote] = useState(''),
    [judgement, setJudgement] = useState<AnnotationJudgement>('uncertain')
  const [loading, setLoading] = useState(true),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('')
  const [attempt, setAttempt] = useState(0)
  const [listRevision, setListRevision] = useState(0)
  const [loaded, setLoaded] = useState(false),
    [list, setList] = useState<Page<Annotation>>(),
    [offset, setOffset] = useState(0),
    [previous, setPrevious] = useState<number[]>([])
  const [listError, setListError] = useState(''),
    [confirmDelete, setConfirmDelete] = useState(false)
  const dirty =
    note !== (saved?.deleted ? '' : (saved?.note ?? '')) ||
    judgement !== (saved?.deleted ? 'uncertain' : (saved?.judgement ?? 'uncertain'))
  useEffect(() => onDirty(dirty), [dirty, onDirty])
  useEffect(() => {
    let active = true
    setLoading(true)
    setLoaded(false)
    setError('')
    setNotice('')
    setConfirmDelete(false)
    window.jeval
      .getAnnotation(target)
      .then(({ annotation }) => {
        if (active) {
          setSaved(annotation)
          setLoaded(true)
          setNote(annotation?.deleted ? '' : (annotation?.note ?? ''))
          setJudgement(annotation?.deleted ? 'uncertain' : (annotation?.judgement ?? 'uncertain'))
        }
      })
      .catch((e) => {
        if (active) setError(String(e))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [run.id, snapshotId, event?.id, attempt])
  useEffect(() => {
    let active = true
    setList(undefined)
    setListError('')
    window.jeval
      .listAnnotations(run.id, snapshotId, offset)
      .then((p) => {
        if (active) setList(p)
      })
      .catch((e) => {
        if (active) setListError(String(e))
      })
    return () => {
      active = false
    }
  }, [run.id, snapshotId, offset, attempt, listRevision])
  async function write(deleted = false) {
    setBusy(true)
    setError('')
    setNotice('')
    try {
      const result = deleted
        ? await window.jeval.deleteAnnotation({ ...target, expectedRevision: saved!.revision })
        : await window.jeval.saveAnnotation({
            ...target,
            judgement,
            note,
            expectedRevision: saved?.revision ?? 0
          })
      setSaved(result)
      setNote(result.deleted ? '' : result.note)
      setJudgement(result.deleted ? 'uncertain' : result.judgement)
      setConfirmDelete(false)
      setNotice(deleted ? '标注已删除' : '标注已保存')
      setListRevision((n) => n + 1)
    } catch (e) {
      setError(String(e))
    } finally {
      setBusy(false)
    }
  }
  const bytes = new TextEncoder().encode(note).length
  return (
    <section className="annotations" aria-label="人工标注" aria-busy={loading || busy}>
      <h3>{event ? `事件 #${event.sequence} 标注` : '快照标注'}</h3>
      <p className="annotation-scope">
        人工意见 · 绑定快照 <code>{snapshotId}</code>
      </p>
      {event && (
        <details className="annotation-evidence">
          <summary>标注对应的事件与证据</summary>
          <p>{event.title}</p>
          <pre>{event.content}</pre>
          <p>
            {event.evidence.location} : {event.evidence.line}
          </p>
        </details>
      )}
      <label>
        判断
        <select
          aria-label="标注判断"
          value={judgement}
          disabled={loading || busy}
          onChange={(e) => setJudgement(e.target.value as AnnotationJudgement)}
        >
          {Object.entries(judgementLabels).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </label>
      <label>
        备注
        <textarea
          aria-label="标注备注"
          value={note}
          maxLength={4096}
          disabled={loading || busy}
          onChange={(e) => setNote(e.target.value)}
          placeholder="写下判断依据或需要复查的内容"
        />
      </label>
      <p className={bytes > 4096 ? 'detail-error' : 'annotation-counter'}>
        {bytes} / 4096 UTF-8 字节{dirty ? ' · 尚未保存' : ''}
      </p>
      <div className="annotation-actions">
        <button
          className="button"
          disabled={loading || busy || bytes > 4096 || note.includes('\0') || !loaded}
          onClick={() => void write()}
        >
          保存标注
        </button>
        <button
          className="button"
          disabled={loading || busy}
          onClick={() => {
            if (!dirty || window.confirm('重新读取会丢弃未保存的标注草稿。继续吗？'))
              setAttempt((n) => n + 1)
          }}
        >
          重新读取标注
        </button>
        {saved && !saved.deleted && (
          <button
            className="button"
            disabled={loading || busy}
            onClick={() => setConfirmDelete(true)}
          >
            删除该标注
          </button>
        )}
      </div>
      {confirmDelete && (
        <div className="annotation-actions">
          <span>确认删除此人工标注？</span>
          <button className="button" disabled={busy} onClick={() => void write(true)}>
            确认删除标注
          </button>
          <button className="button" disabled={busy} onClick={() => setConfirmDelete(false)}>
            保留标注
          </button>
        </div>
      )}
      {notice && <p role="status">{notice}</p>}
      {error && (
        <p className="detail-error" role="alert">
          {error} · 草稿保留；冲突时请重新读取后再保存。
        </p>
      )}
      <div className="annotation-list">
        <h4>此快照的已保存标注 {list ? `(${list.total})` : ''}</h4>
        {listError && (
          <p role="alert">
            {listError}
            <button className="text-button" onClick={() => setAttempt((n) => n + 1)}>
              重试标注列表
            </button>
          </p>
        )}
        {list?.items.length === 0 && <p>此快照还没有标注。</p>}
        {list?.items.map((a) => (
          <button
            className="annotation-item"
            key={a.eventId ?? 'snapshot'}
            disabled={busy || loading}
            onClick={() => onSelect(a.event)}
          >
            <strong>
              {a.event ? `事件 #${a.event.sequence}` : '整个快照'} · {judgementLabels[a.judgement]}
            </strong>
            <span>{a.note || '无备注'}</span>
          </button>
        ))}
        <nav className="annotation-actions" aria-label="标注分页">
          <button
            className="button"
            disabled={!previous.length || !list}
            onClick={() => {
              setOffset(previous[previous.length - 1])
              setPrevious((p) => p.slice(0, -1))
            }}
          >
            上一页标注
          </button>
          <button
            className="button"
            disabled={!list || list.nextOffset === null}
            onClick={() => {
              setPrevious((p) => [...p, offset])
              setOffset(list!.nextOffset!)
            }}
          >
            下一页标注
          </button>
        </nav>
      </div>
    </section>
  )
}
