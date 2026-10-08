import assert from 'node:assert/strict'
import { mkdtemp, writeFile, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { ComparisonReport, ImportResult, Page, RunEvent } from '../../contracts/index.ts'

test('comparison exports exact historical previews and saved notes offline with protected destinations', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-comparison-'))
  const executable = resolve(
      process.env.JEVAL_COMPARISON_EXECUTABLE ??
        `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
    ),
    db = resolve(directory, 'library.sqlite'),
    firstPath = resolve(directory, 'left.jsonl'),
    secondPath = resolve(directory, 'right.jsonl'),
    output = resolve(directory, 'comparison.json')
  const create = () => new EngineClient(executable, 30000, ['--database', db])
  let client = create()
  const row = (content: string) =>
    JSON.stringify({ type: 'response_item', payload: { type: 'message', role: 'user', content } }) +
    '\n'
  const leftText =
    '{"type":"session_meta","payload":{"id":"left"}}\n' +
    Array.from({ length: 65 }, (_, i) => row(`left event ${i}`)).join('')
  const rightText =
    '{"type":"session_meta","payload":{"id":"right"}}\n' +
    row('中🙂'.repeat(9000) + 'full body tail')
  try {
    await writeFile(firstPath, leftText)
    await writeFile(secondPath, rightText)
    await client.start()
    const { run: left } = await client.request<ImportResult>('codex.import', { path: firstPath }),
      { run: right } = await client.request<ImportResult>('codex.import', { path: secondPath })
    const ref = (r: typeof left) => ({ runId: r.id, snapshotId: r.importInfo!.snapshotId })
    const events = await client.request<Page<RunEvent>>('runs.events', {
      runId: left.id,
      offset: 50
    })
    const target = { ...ref(left), eventId: events.items[0].id }
    await client.request('annotations.save', {
      ...target,
      expectedRevision: 0,
      judgement: 'rejected',
      note: '```\n# injected heading\n<script>plain</script>'
    })
    await client.request('annotations.save', {
      ...ref(right),
      eventId: null,
      expectedRevision: 0,
      judgement: 'accepted',
      note: 'right snapshot'
    })
    await writeFile(firstPath, leftText + row('new current event'))
    await client.request('codex.update', { runId: left.id })
    await client.stop()
    await rm(firstPath)
    await rm(secondPath)
    client = create()
    await client.start()
    const p = { left: ref(left), right: ref(right), path: output, format: 'json' }
    await client.request('comparisons.export', p)
    const report: ComparisonReport = JSON.parse(await readFile(output, 'utf8'))
    assert.equal(report.format, 'jeval-comparison')
    assert.equal(report.fullContentIncluded, false)
    assert.equal(report.left.record.events.length, 65)
    assert.equal(report.right.record.events.length, 1)
    assert.equal(report.left.record.runs[0].importInfo!.snapshotId, left.importInfo!.snapshotId)
    assert.equal(report.left.annotations[0].eventId, target.eventId)
    assert.equal(report.left.annotations[0].event, null)
    assert.equal(report.metrics.eventCount.delta, -64)
    assert.equal(report.metrics.durationMs.delta, null)
    assert.equal(report.metrics.tokens.left, null)
    assert.ok(!JSON.stringify(report).includes('full body tail'))
    assert.equal(report.left.record.events[50].evidence.line, 52)
    const saved = await readFile(output)
    await assert.rejects(
      client.request('comparisons.export', { ...p, right: { ...ref(right), runId: 'wrong' } }),
      /NOT_FOUND/
    )
    assert.deepEqual(await readFile(output), saved)
    await assert.rejects(client.request('comparisons.export', { ...p, path: db }), /EXPORT_FAILED/)
    await assert.rejects(
      client.request('comparisons.export', { ...p, path: firstPath }),
      /EXPORT_FAILED/
    )
    await client.request('comparisons.export', {
      ...p,
      path: resolve(directory, 'comparison.md'),
      format: 'markdown'
    })
    const md = await readFile(resolve(directory, 'comparison.md'), 'utf8')
    assert.ok(md.includes('Human judgement: rejected'))
    assert.ok(md.includes('````text\nRun:'))
    assert.ok(md.includes('unknown (null)'))
    assert.ok(!md.includes('new current event'))
    await client.request('comparisons.export', { ...p, right: ref(left) })
    const same: ComparisonReport = JSON.parse(await readFile(output, 'utf8'))
    assert.equal(same.metrics.eventCount.delta, 0)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
