import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm, readFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { Annotation, ImportResult, Page, Run, RunEvent } from '../../contracts/index.ts'

test('human notes retain exact offline history and revisions across replacement and restart', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-annotations-'))
  const source = resolve(directory, 'source.jsonl'),
    db = resolve(directory, 'library.sqlite')
  const executable = resolve(
    process.env.JEVAL_ANNOTATION_EXECUTABLE ??
      `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
  )
  const create = () => new EngineClient(executable, 5000, ['--database', db])
  let client = create()
  const row = (content: string) =>
    JSON.stringify({ type: 'response_item', payload: { type: 'message', role: 'user', content } }) +
    '\n'
  const prefix = '{"type":"session_meta","payload":{"id":"annotations"}}\n'
  try {
    await writeFile(
      source,
      prefix + Array.from({ length: 25 }, (_, i) => row(`original ${i}`)).join('')
    )
    const original = await readFile(source)
    await client.start()
    const { run } = await client.request<ImportResult>('codex.import', { path: source })
    const events = await client.request<Page<RunEvent>>('runs.events', { runId: run.id })
    const base = { runId: run.id, snapshotId: run.importInfo!.snapshotId }
    for (const event of events.items)
      await client.request('annotations.save', {
        ...base,
        eventId: event.id,
        judgement: 'rejected',
        note: `review ${event.sequence}`,
        expectedRevision: 0
      })
    await client.request('annotations.save', {
      ...base,
      eventId: null,
      judgement: 'accepted',
      note: 'snapshot note',
      expectedRevision: 0
    })
    assert.deepEqual(await readFile(source), original)
    const notes = await client.request<Page<Annotation>>('annotations.list', { ...base, limit: 20 })
    assert.equal(notes.total, 26)
    assert.equal(notes.items.length, 20)
    const rest = await client.request<Page<Annotation>>('annotations.list', {
      ...base,
      offset: notes.nextOffset!,
      limit: 20
    })
    assert.equal(rest.items.length, 6)
    const target = { ...base, eventId: events.items[0].id }
    await assert.rejects(
      client.request('annotations.save', {
        ...target,
        judgement: 'accepted',
        note: 'stale',
        expectedRevision: 0
      }),
      /ANNOTATION_CONFLICT/
    )
    await assert.rejects(
      client.request('annotations.save', {
        ...target,
        judgement: 'accepted',
        note: 'a',
        expectedRevision: null
      }),
      /INVALID_PARAMS/
    )
    await writeFile(source, prefix + row('replacement text'))
    const updated = await client.request<ImportResult>('codex.update', { runId: run.id })
    await rm(source)
    await client.stop()
    client = create()
    await client.start()
    const old = await client.request<Page<RunEvent>>('runs.events', base)
    assert.deepEqual(old.items, events.items)
    const saved = await client.request<{ annotation: Annotation }>('annotations.get', target)
    assert.equal(saved.annotation.note, 'review 1')
    assert.deepEqual(saved.annotation.event, events.items[0])
    const history = await client.request<Page<Run>>('runs.snapshots', { runId: run.id })
    assert.equal(history.total, 2)
    const fresh = await client.request<Page<Annotation>>('annotations.list', {
      runId: run.id,
      snapshotId: updated.run.importInfo!.snapshotId
    })
    assert.equal(fresh.total, 0)
    await assert.rejects(
      client.request('annotations.get', { ...target, runId: 'wrong' }),
      /NOT_FOUND/
    )
    await client.request('annotations.delete', { ...target, expectedRevision: 1 })
    const tombstone = await client.request<{ annotation: Annotation }>('annotations.get', target)
    assert.equal(tombstone.annotation.deleted, true)
    assert.equal(tombstone.annotation.revision, 2)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
