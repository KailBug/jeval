import { useCallback, useState } from 'react'
import type { RunStatus } from '../../../../../contracts/index'

export interface BrowseLocation {
  source: 'demo' | 'codex'
  runId?: string
  search: string
  status: RunStatus | 'all'
  offset?: number
}

const initial: BrowseLocation = { source: 'demo', search: '', status: 'all' }

export function useBrowseHistory() {
  const [history, setHistory] = useState({ entries: [initial], index: 0 })
  const location = history.entries[history.index]
  const navigate = useCallback((next: BrowseLocation) => {
    setHistory((old) => {
      const current = old.entries[old.index]
      if (
        current.source === next.source &&
        current.runId === next.runId &&
        current.search === next.search &&
        current.status === next.status &&
        (current.offset ?? 0) === (next.offset ?? 0)
      )
        return old
      const entries = [...old.entries.slice(0, old.index + 1), next].slice(-100)
      return { entries, index: entries.length - 1 }
    })
  }, [])
  // Editing filters and resolving a default/missing selection update this visit;
  // they don't create one history entry per keystroke or asynchronous response.
  const update = useCallback((patch: Partial<BrowseLocation>) => {
    setHistory((old) => ({
      ...old,
      entries: old.entries.map((entry, i) => (i === old.index ? { ...entry, ...patch } : entry))
    }))
  }, [])
  const resolveSelection = useCallback((ids: string[]) => {
    setHistory((old) => {
      const current = old.entries[old.index]
      // The selected record may be on another page. Details resolve it by ID.
      if (current.runId) return old
      return {
        ...old,
        entries: old.entries.map((entry, i) =>
          i === old.index ? { ...entry, runId: ids[0] } : entry
        )
      }
    })
  }, [])
  const move = useCallback((delta: number) => {
    setHistory((old) => ({
      ...old,
      index: Math.max(0, Math.min(old.entries.length - 1, old.index + delta))
    }))
  }, [])
  const reset = useCallback(() => setHistory({ entries: [initial], index: 0 }), [])
  return {
    location,
    navigate,
    update,
    resolveSelection,
    move,
    reset,
    canBack: history.index > 0,
    canForward: history.index < history.entries.length - 1
  }
}
