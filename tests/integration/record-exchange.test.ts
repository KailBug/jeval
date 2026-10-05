import assert from 'node:assert/strict'
import { mkdtemp, readFile, writeFile, rm, readdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type {
  ImportResult,
  NativeRecordEnvelope,
  Page,
  Run,
  RunEvent
} from '../../contracts/index.ts'

const executable = process.env.JEVAL_EXCHANGE_EXECUTABLE
  ? resolve(process.env.JEVAL_EXCHANGE_EXECUTABLE)
  : resolve('bin', process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine')

type Envelope = NativeRecordEnvelope

async function allEvents(client: EngineClient, runId: string): Promise<RunEvent[]> {
  const events: RunEvent[] = []
  let offset: number | null = 0
  while (offset !== null) {
    const page: Page<RunEvent> = await client.request('runs.events', { runId, offset, limit: 50 })
    events.push(...page.items)
    offset = page.nextOffset
  }
  return events
}

test('native snapshots round-trip through a blank library without original files', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-exchange-中文 '))
  const source = resolve(directory, 'source.jsonl')
  const fromDatabase = resolve(directory, 'from.sqlite')
  const toDatabase = resolve(directory, 'to.sqlite')
  const jsonPath = resolve(directory, '记录.jeval.json')
  const markdownPath = resolve(directory, '记录.md')
  const againPath = resolve(directory, 'again.jeval.json')
  const fixture = await readFile(resolve('fixtures/adapters/codex/classic.jsonl'), 'utf8')
  const messages = Array.from({ length: 121 }, (_, n) =>
    JSON.stringify({
      type: 'response_item',
      payload: {
        type: 'message',
        role: 'user',
        content: n === 120 ? 'Ω'.repeat(5000) : `exchange message ${n} Ω`
      }
    })
  ).join('\n')
  await writeFile(source, fixture + '\n' + messages + '\n')
  let from = new EngineClient(executable, 10000, ['--database', fromDatabase])
  let to = new EngineClient(executable, 10000, ['--database', toDatabase])
  try {
    const hello = await from.start()
    assert.ok(hello.capabilities.includes('records.export'))
    assert.ok(hello.capabilities.includes('records.import'))
    const imported = await from.request<ImportResult>('codex.import', { path: source })
    const before = await allEvents(from, imported.run.id)
    assert.ok(before.length > 100)
    assert.ok(before.some((event) => event.parentId !== null))
    assert.ok(before.some((event) => event.content.includes('内容预览已截断')))
    await from.stop()
    await rm(source)
    from = new EngineClient(executable, 10000, ['--database', fromDatabase])
    await from.start()
    const filtered = await from.request<Page<RunEvent>>('runs.events', {
      runId: imported.run.id,
      search: 'exchange message 119'
    })
    assert.equal(filtered.total, 1)
    const result = await from.request<{ path: string; snapshotId: string; eventCount: number }>(
      'records.export',
      { runId: imported.run.id, path: jsonPath, format: 'json' }
    )
    assert.equal(result.eventCount, before.length)
    assert.equal(result.snapshotId, imported.run.importInfo?.snapshotId)
    const raw = await readFile(jsonPath, 'utf8')
    const envelope: Envelope = JSON.parse(raw)
    assert.equal(envelope.format, 'jeval-record')
    assert.equal(envelope.formatVersion, 1)
    assert.equal(envelope.contentScope, 'normalized-preview')
    assert.equal(envelope.maxContentBytes, 8192)
    assert.equal(envelope.sourceFilesIncluded, false)
    assert.deepEqual(envelope.record.runs, [imported.run])
    assert.deepEqual(envelope.record.events, before)
    assert.equal(envelope.record.runs[0].durationMs, null)
    assert.equal(envelope.record.runs[0].tokens, null)
    await from.request('records.export', {
      runId: imported.run.id,
      path: markdownPath,
      format: 'markdown'
    })
    const markdown = await readFile(markdownPath, 'utf8')
    assert.ok(markdown.includes(imported.run.importInfo!.snapshotId))
    assert.ok(markdown.includes(source))
    assert.ok(markdown.includes(before[before.length - 1].id))
    assert.ok(markdown.includes('内容预览已截断'))
    await to.start()
    assert.equal((await to.request<Page<Run>>('runs.list', { source: 'codex' })).total, 0)
    const restored = await to.request<ImportResult>('records.import', { path: jsonPath })
    assert.equal(restored.replaced, false)
    assert.deepEqual(restored.run, { ...imported.run, readOnly: true })
    assert.deepEqual(await allEvents(to, imported.run.id), before)
    assert.deepEqual(await to.request('codex.directories.list'), { items: [] })
    await assert.rejects(to.request('codex.update', { runId: imported.run.id }), /IMPORT_FAILED/)
    const repeated = await to.request<ImportResult>('records.import', { path: jsonPath })
    assert.equal(repeated.replaced, true)
    assert.equal((await to.request<Page<Run>>('runs.list', { source: 'codex' })).total, 1)
    await to.stop()
    to = new EngineClient(executable, 10000, ['--database', toDatabase])
    await to.start()
    assert.equal((await to.request<Run>('runs.get', { runId: imported.run.id })).readOnly, true)
    await to.request('records.export', { runId: imported.run.id, path: againPath, format: 'json' })
    assert.equal(await readFile(againPath, 'utf8'), raw)
    const invalid = JSON.parse(raw)
    invalid.formatVersion = 999
    const invalidPath = resolve(directory, 'invalid.json')
    await writeFile(invalidPath, JSON.stringify(invalid))
    await assert.rejects(to.request('records.import', { path: invalidPath }), /IMPORT_FAILED/)
    assert.deepEqual(await allEvents(to, imported.run.id), before)
    // Returning a matching package to its original library must not remove the
    // independently granted local-source update permission.
    const localAgain = await from.request<ImportResult>('records.import', { path: jsonPath })
    assert.equal(localAgain.run.readOnly, undefined)
    // A different snapshot under the same source identity must not silently
    // replace the recipient's current version.
    await writeFile(source, fixture + '\n' + messages + '\n\n')
    const updated = await from.request<ImportResult>('codex.update', { runId: imported.run.id })
    assert.notEqual(updated.run.importInfo?.snapshotId, imported.run.importInfo?.snapshotId)
    await from.request('records.export', {
      runId: imported.run.id,
      path: againPath,
      format: 'json'
    })
    await assert.rejects(to.request('records.import', { path: againPath }), /RECORD_CONFLICT/)
    assert.equal(
      (await to.request<Run>('runs.get', { runId: imported.run.id })).importInfo?.snapshotId,
      imported.run.importInfo?.snapshotId
    )
  } finally {
    await from.stop()
    await to.stop()
    await rm(directory, { recursive: true, force: true })
  }
})

test('export protects source and database files and leaves no failed artifacts', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-export-failure-'))
  const source = resolve(directory, 'source.jsonl')
  const database = resolve(directory, 'library.sqlite')
  const original = await readFile(resolve('fixtures/adapters/codex/classic.jsonl'))
  await writeFile(source, original)
  const client = new EngineClient(executable, 10000, ['--database', database])
  try {
    await client.start()
    const { run } = await client.request<ImportResult>('codex.import', { path: source })
    for (const path of [source, database, database + '-wal', database + '-shm']) {
      await assert.rejects(
        client.request('records.export', { runId: run.id, path, format: 'json' }),
        /EXPORT_FAILED|INVALID_PARAMS/
      )
    }
    assert.deepEqual(await readFile(source), original)
    const blocked = resolve(directory, 'missing', 'record.json')
    await assert.rejects(
      client.request('records.export', {
        runId: run.id,
        path: blocked,
        format: 'json'
      }),
      /EXPORT_FAILED/
    )
    assert.ok(!(await readdir(directory)).some((name) => name.includes('.tmp')))
    assert.equal((await client.request<Run>('runs.get', { runId: run.id })).id, run.id)
    // Invalid IDs/formats fail before touching an existing destination.
    const untouched = resolve(directory, 'untouched.txt')
    await writeFile(untouched, 'keep existing bytes')
    for (const params of [
      { runId: 'missing', path: untouched, format: 'json' },
      { runId: run.id, path: untouched, format: 'html' }
    ])
      await assert.rejects(client.request('records.export', params), /NOT_FOUND|INVALID_PARAMS/)
    assert.equal(await readFile(untouched, 'utf8'), 'keep existing bytes')
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})

test('the published native fixture is interpreted consistently by TypeScript and Go', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-contract-'))
  const fixturePath = resolve('contracts/exchange/preview-v1.json')
  const envelope: Envelope = JSON.parse(await readFile(fixturePath, 'utf8'))
  const client = new EngineClient(executable, 10000, [
    '--database',
    resolve(directory, 'library.sqlite')
  ])
  try {
    await client.start()
    const result = await client.request<ImportResult>('records.import', { path: fixturePath })
    assert.deepEqual(result.run, { ...envelope.record.runs[0], readOnly: true })
    assert.deepEqual(await allEvents(client, result.run.id), envelope.record.events)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
