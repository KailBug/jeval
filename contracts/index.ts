export const PROTOCOL_VERSION = 1 as const
export const RECORD_VERSION = 1 as const

export type RunStatus = 'completed' | 'failed' | 'unknown'
export type EventKind =
  | 'message'
  | 'tool_call'
  | 'tool_result'
  | 'verification'
  | 'lifecycle'
  | 'error'
export interface EvidenceRef {
  sourceId: string
  location: string
  line: number
}
export interface Run {
  id: string
  title: string
  project: string
  source: string
  demo: boolean
  status: RunStatus
  startedAt: string | null
  durationMs: number | null
  tokens: number | null
  eventCount: number
  importInfo?: {
    file: string
    sha256: string
    sessionId: string
    cliVersion: string
    historyMode: 'classic' | 'paginated'
    parentThreadId?: string
    forkedFromId?: string
    warningCount: number
    warnings: { line: number; message: string }[]
  }
}
export interface ImportResult {
  run: Run
  replaced: boolean
}
export interface ScanStatus {
  id: string
  root: string
  state: 'running' | 'cancelling' | 'completed' | 'cancelled' | 'limited'
  phase: 'discovery' | 'import'
  ready: number
  visited: number
  discovered: number
  imported: number
  updated: number
  failed: number
  skipped: number
  issues: { path: string; message: string }[]
  message: string
}
export interface ScanCandidate {
  id: string
  title: string
  project: string
  path: string
  startedAt: string | null
  eventCount: number
  warningCount: number
  preview: string
  existing: boolean
  imported: boolean
  error: string
}
export interface RunEvent {
  id: string
  runId: string
  sequence: number
  kind: EventKind
  role: string
  title: string
  content: string
  timestamp: string | null
  parentId: string | null
  evidence: EvidenceRef
}
export interface Page<T> {
  items: T[]
  total: number
  nextOffset: number | null
}
export interface Hello {
  engineVersion: string
  protocolVersion: number
  recordVersion: number
  capabilities: string[]
}
export interface RunQuery {
  source?: 'all' | 'demo' | 'codex'
  search?: string
  status?: RunStatus | 'all'
  offset?: number
  limit?: number
}
export interface EventQuery {
  runId: string
  search?: string
  kind?: EventKind | 'all'
  offset?: number
  limit?: number
}
export interface DesktopAPI {
  readonly platform: string
  importCodex(): Promise<ImportResult | null>
  scanCodex(): Promise<ScanStatus | null>
  scanStatus(id: string): Promise<ScanStatus>
  cancelScan(id: string): Promise<ScanStatus>
  scanCandidates(id: string, offset?: number): Promise<Page<ScanCandidate>>
  importScanSelection(id: string, ids: string[]): Promise<ScanStatus>
  hello(): Promise<Hello>
  listRuns(query: RunQuery): Promise<Page<Run>>
  listEvents(query: EventQuery): Promise<Page<RunEvent>>
  restartEngine(): Promise<Hello>
}
