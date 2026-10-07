import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises'
import { dirname, isAbsolute, relative, resolve } from 'node:path'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { ImportResult, Page, RunEvent, UpdateStatus } from '../../contracts/index.ts'

export interface AuditSample {
  id: string
  path: string
  sha256: string
  provenance: 'local-historical' | 'public-sanitized'
  notAfter?: string
  expectedEvents?: number
  expectedWarnings?: number
  parentSample?: string
}
type Row = { timestamp?: string; type: string; payload: Record<string, any> }
const digest = (value: Buffer | string) => createHash('sha256').update(value).digest('hex')
const count = (values: string[]) =>
  Object.fromEntries(
    [...new Set(values)].sort().map((value) => [value, values.filter((x) => x === value).length])
  )
const label = (value: unknown) =>
  typeof value === 'string' && /^[a-zA-Z0-9_.-]{1,64}$/.test(value) ? value : 'unknown'

function sourceText(value: unknown): string {
  if (typeof value === 'string') return value
  if (value == null) return ''
  if (Array.isArray(value) && value.length)
    return value
      .map((part) =>
        ['input_text', 'output_text', 'text', 'Text'].includes(part.type)
          ? part.text
          : `[未展开的内容块：${part.type}]`
      )
      .join('\n')
  return JSON.stringify(value)
}
function preview(value: string): string {
  const bytes = Buffer.from(value)
  if (bytes.length <= 8192) return value
  let end = 8192
  while ((bytes[end] & 0xc0) === 0x80) end--
  return bytes.subarray(0, end).toString('utf8') + '\n[内容预览已截断，请按来源行号查看原文件]'
}
async function events(client: EngineClient, runId: string) {
  const result: RunEvent[] = []
  let offset: number | null = 0
  let pages = 0
  while (offset !== null) {
    const page: Page<RunEvent> = await client.request('runs.events', { runId, offset, limit: 50 })
    result.push(...page.items)
    pages++
    assert.ok(page.nextOffset === null || page.nextOffset > offset)
    if (page.nextOffset === null) assert.equal(result.length, page.total)
    offset = page.nextOffset
  }
  return { items: result, pages }
}
function verifyEvidence(
  rows: Row[],
  items: RunEvent[],
  imported: ImportResult,
  progress: (step: string) => void
) {
  const byLine = new Map(items.map((event) => [event.evidence.line, event]))
  assert.equal(byLine.size, items.length)
  const calls = new Map(
    rows.flatMap((row, index) =>
      row?.type === 'response_item' &&
      ['function_call', 'custom_tool_call'].includes(row.payload.type) &&
      row.payload.call_id
        ? [[row.payload.call_id, index + 1] as const]
        : []
    )
  )
  let verifiedFailures = 0
  for (const [index, event] of items.entries()) {
    progress(`source-evidence-line-${event.evidence.line}-${event.kind}`)
    assert.equal(event.sequence, index + 1)
    assert.equal(event.evidence.snapshotId, imported.run.importInfo!.snapshotId)
    assert.equal(event.evidence.sourceId, imported.run.id)
    assert.equal(event.evidence.location, imported.run.importInfo!.file)
    const row = rows[event.evidence.line - 1]
    assert.ok(row)
    assert.equal(event.timestamp, row.timestamp ?? null)
    const p = row.payload
    if (event.kind === 'lifecycle') {
      const serialized = JSON.stringify(p)
      if (Buffer.byteLength(serialized) > 8192) assert.equal(event.content, preview(serialized))
      else assert.deepEqual(JSON.parse(event.content), p)
    } else {
      let body: string
      if (row.type === 'response_item' && p.type === 'message') body = sourceText(p.content)
      else if (row.type === 'event_msg' && p.type === 'item_completed')
        body = sourceText(p.item.content)
      else if (event.kind === 'tool_call')
        body = p.type === 'custom_tool_call' ? p.input : p.arguments
      else if (event.kind === 'tool_result') {
        body = sourceText(p.output)
        const parent = calls.get(p.call_id)
        assert.equal(event.parentId, parent ? byLine.get(parent)?.id : null)
        if (/(?:exit(?:ed)? (?:with )?code[: ]+|exit_code["\s:]+)[1-9]/i.test(body))
          verifiedFailures++
      } else if (event.kind === 'error') body = p.message
      else throw new Error('Unexpected event mapping')
      assert.equal(event.content, preview(body ?? ''))
    }
  }
  // Every canonical supported row must appear exactly once, even when the source
  // also contains projected messages. This oracle starts from source lines.
  rows.forEach((row, index) => {
    progress(`canonical-coverage-line-${index + 1}`)
    if (!row) return
    const p = row.payload
    if (
      row.type === 'response_item' &&
      ((p.type === 'message' && ['user', 'assistant', 'system', 'developer'].includes(p.role)) ||
        [
          'function_call',
          'function_call_output',
          'custom_tool_call',
          'custom_tool_call_output'
        ].includes(p.type))
    ) {
      assert.ok(byLine.has(index + 1), `Canonical source line ${index + 1} is missing`)
    }
  })
  return verifiedFailures
}

// No discovery, network requests, screenshots or transcript logging. The caller
// supplies exact files; returned reports contain only allowlisted statistics.
export async function auditSources(manifestPath: string, executable: string) {
  const manifest: { samples: AuditSample[] } = JSON.parse(await readFile(manifestPath, 'utf8'))
  assert.ok(manifest.samples.length > 0 && manifest.samples.length <= 20)
  const root = resolve('.local')
  await mkdir(root, { recursive: true })
  const work = await mkdtemp(resolve(root, 'source-audit-'))
  const results = []
  const sessionIDs = new Map<string, string>()
  let currentSample = 'manifest'
  let phase = 'qualification'
  try {
    for (const sample of manifest.samples) {
      assert.match(sample.id, /^[A-Z][0-9]{2}$/)
      currentSample = sample.id
      phase = 'qualification'
      assert.ok(!sessionIDs.has(sample.id), 'Duplicate sample ID')
      assert.ok(['local-historical', 'public-sanitized'].includes(sample.provenance))
      const source = resolve(dirname(manifestPath), sample.path)
      const bytes = await readFile(source)
      assert.equal(digest(bytes), sample.sha256, 'Source digest mismatch')
      assert.ok(bytes.length <= 16 * 1024 * 1024, 'Source exceeds current adapter limit')
      const rows: Row[] = bytes
        .toString('utf8')
        .split('\n')
        .map((line) => (line.trim() ? JSON.parse(line) : null))
      const timestamps = rows
        .filter(Boolean)
        .flatMap(
          (row) =>
            [row.timestamp, row.type === 'session_meta' ? row.payload.timestamp : undefined].filter(
              Boolean
            ) as string[]
        )
      assert.ok(timestamps.length > 0)
      assert.ok(timestamps.every((date) => Number.isFinite(Date.parse(date))))
      if (sample.provenance === 'local-historical') {
        assert.ok(sample.notAfter && Number.isFinite(Date.parse(sample.notAfter)))
        assert.ok(
          rows.filter(Boolean).every((row) => row.timestamp),
          'Undated local source row'
        )
        assert.ok(
          timestamps.every((date) => Date.parse(date) < Date.parse(sample.notAfter!)),
          'Local source is newer than the allowed cutoff'
        )
      }
      const meta = rows.find((row) => row?.type === 'session_meta')!.payload
      const copy = resolve(work, `${sample.id}.jsonl`)
      await writeFile(copy, bytes, { flag: 'wx' })
      const db = resolve(work, `${sample.id}.sqlite`)
      const make = (path = db) => new EngineClient(executable, 30000, ['--database', path])
      let client = make()
      let recipient: EngineClient | undefined
      try {
        phase = 'import'
        await client.start()
        const imported = await client.request<ImportResult>('codex.import', { path: copy })
        const run = imported.run
        phase = 'metadata'
        assert.equal(run.importInfo?.adapterVersion, 'codex-rollout-v1')
        assert.equal(run.importInfo?.sha256, sample.sha256)
        assert.equal(run.importInfo?.sessionId, meta.id)
        assert.equal(run.importInfo?.forkedFromId ?? '', meta.forked_from_id ?? '')
        assert.equal(run.importInfo?.parentThreadId ?? '', meta.parent_thread_id ?? '')
        assert.equal(run.status, 'unknown')
        assert.equal(run.durationMs, null)
        assert.equal(run.tokens, null)
        const page = await events(client, run.id)
        phase = 'source-evidence'
        const failures = verifyEvidence(rows, page.items, imported, (step) => {
          phase = step
        })
        if (sample.expectedEvents !== undefined)
          assert.equal(page.items.length, sample.expectedEvents)
        if (sample.expectedWarnings !== undefined)
          assert.equal(run.importInfo?.warningCount, sample.expectedWarnings)
        sessionIDs.set(sample.id, meta.id)
        if (sample.parentSample)
          assert.equal(meta.forked_from_id, sessionIDs.get(sample.parentSample))
        phase = 'restart'
        await client.stop()
        client = make()
        await client.start()
        assert.deepEqual(await events(client, run.id), page)
        phase = 'update'
        let job = await client.request<UpdateStatus>('codex.update.start', { runId: run.id })
        for (let i = 0; i < 600 && ['running', 'cancelling'].includes(job.state); i++) {
          await new Promise((done) => setTimeout(done, 10))
          job = await client.request('codex.update.status', { id: job.id })
        }
        assert.equal(job.state, 'completed')
        assert.equal(job.report?.mode, 'unchanged')
        assert.deepEqual(await events(client, run.id), page)
        phase = 'historical-append-replay'
        let replay:
          | { prefixBytes: number; mode: string | undefined; reason: string | undefined }
          | undefined
        if (page.items.length > 0) {
          let boundary = 0
          for (let i = 0; i < Math.floor(rows.filter(Boolean).length / 2); i++)
            boundary = bytes.indexOf(10, boundary) + 1
          assert.ok(boundary > 0 && boundary < bytes.length)
          await writeFile(copy, bytes.subarray(0, boundary))
          const prefix = await client.request<ImportResult>('codex.import', { path: copy })
          assert.equal(prefix.run.id, run.id)
          await client.stop()
          client = make()
          await client.start()
          await writeFile(copy, bytes)
          let replayJob = await client.request<UpdateStatus>('codex.update.start', {
            runId: run.id
          })
          for (let i = 0; i < 600 && ['running', 'cancelling'].includes(replayJob.state); i++) {
            await new Promise((done) => setTimeout(done, 10))
            replayJob = await client.request('codex.update.status', { id: replayJob.id })
          }
          assert.equal(replayJob.state, 'completed')
          assert.deepEqual(replayJob.run, run)
          assert.deepEqual(await events(client, run.id), page)
          replay = {
            prefixBytes: boundary,
            mode: replayJob.report?.mode,
            reason: replayJob.report?.reason
          }
        }
        phase = 'offline-exchange'
        await rename(copy, `${copy}.offline`)
        const exported = resolve(work, `${sample.id}.jeval.json`)
        await client.request('records.export', { runId: run.id, path: exported, format: 'json' })
        recipient = make(resolve(work, `${sample.id}-recipient.sqlite`))
        await recipient.start()
        const received = await recipient.request<ImportResult>('records.import', { path: exported })
        assert.equal(received.run.readOnly, true)
        assert.deepEqual(await events(recipient, run.id), page)
        assert.equal(digest(await readFile(source)), sample.sha256, 'Original source changed')
        results.push({
          id: sample.id,
          provenance: sample.provenance,
          sha256: sample.sha256,
          bytes: bytes.length,
          rows: rows.filter(Boolean).length,
          cliVersion: label(meta.cli_version),
          sourceHistoryMode: label(meta.history_mode),
          adapterHistoryMode: run.importInfo?.historyMode,
          eventCount: page.items.length,
          pages: page.pages,
          eventKinds: count(page.items.map((event) => event.kind)),
          verifiedNonzeroExitResults: failures,
          warningCount: run.importInfo?.warningCount,
          warningSamples: count((run.importInfo?.warnings ?? []).map((w) => w.message)),
          truncatedEvents: page.items.filter((event) =>
            event.content.endsWith('[内容预览已截断，请按来源行号查看原文件]')
          ).length,
          sourceTypes: count(rows.filter(Boolean).map((row) => label(row.type))),
          responseTypes: count(
            rows
              .filter((row) => row?.type === 'response_item')
              .map((row) => label(row.payload.type))
          ),
          projectedTypes: count(
            rows
              .filter((row) => row?.type === 'event_msg' && row.payload.type === 'item_completed')
              .map((row) => label(row.payload.item?.type))
          ),
          lifecycle: count(
            rows
              .filter(
                (row) =>
                  row?.type === 'event_msg' &&
                  ['task_started', 'task_complete', 'turn_aborted'].includes(row.payload.type)
              )
              .map((row) => row.payload.type)
          ),
          hasFork: !!meta.forked_from_id,
          hasParent: !!meta.parent_thread_id,
          historicalAppendReplay: replay ?? null,
          earliest: new Date(Math.min(...timestamps.map(Date.parse))).toISOString(),
          latest: new Date(Math.max(...timestamps.map(Date.parse))).toISOString(),
          checks: [
            'source-evidence',
            'canonical-coverage',
            'tool-parents',
            'unknown-metrics',
            'restart',
            'unchanged-update',
            'offline-exchange',
            'source-unchanged'
          ]
        })
      } finally {
        await recipient?.stop()
        await client.stop()
      }
    }
    return { schemaVersion: 1, adapterVersion: 'codex-rollout-v1', results }
  } catch {
    throw new Error(`Source audit failed: ${currentSample}/${phase}; no transcript details logged`)
  } finally {
    const child = relative(root, work)
    assert.ok(child && !child.startsWith('..') && !isAbsolute(child))
    await rm(work, { recursive: true, force: true })
  }
}
