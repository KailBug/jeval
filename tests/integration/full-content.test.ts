import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { EventContentPage, ImportResult, Page, RunEvent } from '../../contracts/index.ts'

const executable = resolve(
  process.env.JEVAL_CONTENT_EXECUTABLE ??
    `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
)
const row = (type: string, payload: object) => JSON.stringify({ type, payload }) + '\n'

test('full bodies survive checkpoint append, process restart and offline historical reads without widening search or exchange', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-content-'))
  const path = resolve(directory, 'source.jsonl')
  const body = '中🙂\u0000\n'.repeat(14000) + 'ONLY_FULL_TAIL'
  const prefix =
    row('session_meta', { id: 'body' }) +
    row('response_item', { type: 'message', role: 'user', content: body })
  const create = () =>
    new EngineClient(executable, 5000, ['--database', resolve(directory, 'library.sqlite')])
  let client = create()
  const read = async (snapshotId: string, runId: string, eventId: string) => {
    let content = '',
      offset = 0
    for (;;) {
      const page = await client.request<EventContentPage>('runs.eventContent', {
        runId,
        snapshotId,
        eventId,
        offset
      })
      assert.equal(page.available, true)
      assert.equal(page.snapshotId, snapshotId)
      assert.ok(Buffer.byteLength(page.content) <= 32768)
      assert.ok(Buffer.byteLength(JSON.stringify(page)) < 1024 * 1024)
      content += page.content
      if (page.nextOffset === null) return content
      assert.ok(page.nextOffset > offset)
      offset = page.nextOffset
    }
  }
  try {
    await writeFile(path, prefix)
    assert.ok((await client.start()).capabilities.includes('runs.eventContent'))
    const first = await client.request<ImportResult>('codex.import', { path })
    const event = (await client.request<Page<RunEvent>>('runs.events', { runId: first.run.id }))
      .items[0]
    assert.ok(!event.content.includes('ONLY_FULL_TAIL'))
    assert.equal(await read(first.run.importInfo!.snapshotId, first.run.id, event.id), body)
    await client.stop()
    client = create()
    await client.start()
    await writeFile(
      path,
      prefix + row('response_item', { type: 'message', role: 'assistant', content: 'new text' })
    )
    const next = await client.request<ImportResult & { update: { mode: string } }>('codex.update', {
      runId: first.run.id
    })
    assert.equal(next.update.mode, 'incremental')
    assert.notEqual(next.run.importInfo!.snapshotId, first.run.importInfo!.snapshotId)
    await rm(path)
    await client.stop()
    client = create()
    await client.start()
    for (const snapshotId of [first.run.importInfo!.snapshotId, next.run.importInfo!.snapshotId]) {
      assert.equal(await read(snapshotId, first.run.id, event.id), body)
    }
    const search = await client.request<Page<RunEvent>>('runs.events', {
      runId: first.run.id,
      search: 'ONLY_FULL_TAIL'
    })
    assert.equal(search.total, 0)
    await assert.rejects(
      client.request('runs.eventContent', {
        runId: 'other',
        snapshotId: first.run.importInfo!.snapshotId,
        eventId: event.id
      }),
      /NOT_FOUND/
    )
    const bundle = resolve(directory, 'preview.json')
    await client.request('records.export', { runId: first.run.id, path: bundle, format: 'json' })
    const detached = new EngineClient(executable, 5000, [
      '--database',
      resolve(directory, 'detached.sqlite')
    ])
    try {
      await detached.start()
      await detached.request('records.import', { path: bundle })
      const missing = await detached.request<EventContentPage>('runs.eventContent', {
        runId: first.run.id,
        snapshotId: next.run.importInfo!.snapshotId,
        eventId: event.id
      })
      assert.equal(missing.available, false)
    } finally {
      await detached.stop()
    }
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
