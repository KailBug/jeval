import { useEffect, useState } from 'react'
import type { EventContentPage, RunEvent } from '../../../../../contracts/index'

export function EventContent({ runId, event }: { runId: string; event: RunEvent }) {
  const [page, setPage] = useState<EventContentPage>()
  const [offset, setOffset] = useState(0)
  const [previous, setPrevious] = useState<number[]>([])
  const [attempt, setAttempt] = useState(0)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  useEffect(() => {
    let active = true
    setLoading(true)
    setPage(undefined)
    setError('')
    void window.jeval
      .getEventContent({
        runId,
        snapshotId: event.evidence.snapshotId!,
        eventId: event.id,
        offset
      })
      .then((result) => {
        if (active) setPage(result)
      })
      .catch((e: unknown) => {
        if (active) setError(e instanceof Error ? e.message : '正文读取失败')
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [runId, event.id, event.evidence.snapshotId, offset, attempt])

  return (
    <section className="full-content-panel" aria-label="完整正文" aria-busy={loading}>
      <p>已保存的标准化文本 · 不含图片、附件和未映射字段</p>
      <details>
        <summary>正文快照与事件</summary>
        <code>
          {event.evidence.snapshotId} / {event.id}
        </code>
      </details>
      {loading && <p role="status">正在读取正文…</p>}
      {error && (
        <p role="alert">
          {error}{' '}
          <button className="text-button" onClick={() => setAttempt((n) => n + 1)}>
            重试正文
          </button>
        </p>
      )}
      {page && !page.available && (
        <p role="status">
          此快照未保存完整正文。旧预览无法恢复截断内容；请在原文件可用时手动更新记录，或显式选择原文件重新导入。
        </p>
      )}
      {page?.available && (
        <>
          <p role="status">
            {page.totalBytes === 0
              ? '正文为空'
              : `字节 ${page.offset + 1}–${page.nextOffset ?? page.totalBytes} / ${page.totalBytes} · 每页最多 32 KiB`}
          </p>
          <pre className="full-content-text" tabIndex={0}>
            {page.content}
          </pre>
        </>
      )}
      <nav className="page-controls" aria-label="正文分页">
        <button
          className="button"
          disabled={loading || previous.length === 0}
          onClick={() => {
            setOffset(previous[previous.length - 1])
            setPrevious((p) => p.slice(0, -1))
            setLoading(true)
          }}
        >
          上一页正文
        </button>
        <button
          className="button"
          disabled={loading || page?.nextOffset == null}
          onClick={() => {
            if (page?.nextOffset == null) return
            setPrevious((p) => [...p, offset])
            setOffset(page.nextOffset)
            setLoading(true)
          }}
        >
          下一页正文
        </button>
      </nav>
    </section>
  )
}
