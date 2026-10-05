import assert from 'node:assert/strict'
import { mkdtemp, readFile, writeFile, mkdir, rm, symlink } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { ImportResult, Page, Run, RunEvent, ScanStatus } from '../../contracts/index.ts'

const executable = resolve(
  'bin',
  process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine'
)

test('persistent library survives process restart, offline sources and failed updates', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-library-中文 '))
  const source = resolve(directory, '来源 目录')
  await mkdir(source)
  const path = resolve(source, 'session.jsonl')
  const database = resolve(directory, '任务库.sqlite')
  const original = await readFile(resolve('fixtures/adapters/codex/classic.jsonl'))
  await writeFile(path, original)
  let client = new EngineClient(executable, 5000, ['--database', database])
  const waitForScan = async (id: string) => {
    for (let i = 0; i < 200; i++) {
      const status = await client.request<ScanStatus>('codex.scan.status', { id })
      if (status.state !== 'running' && status.state !== 'cancelling') return status
      await new Promise((resolve) => setTimeout(resolve, 10))
    }
    throw new Error('scan did not finish')
  }
  try {
    assert.ok((await client.start()).capabilities.includes('persistent-library'))
    const scan = await client.request<ScanStatus>('codex.scan.start', { path: source })
    assert.equal((await waitForScan(scan.id)).ready, 1)
    assert.equal((await client.request<Page<Run>>('runs.list', { source: 'codex' })).total, 0)
    const dirs = await client.request<{ items: { id: string; path: string }[] }>(
      'codex.directories.list'
    )
    assert.equal(dirs.items.length, 1)
    const first = await client.request<ImportResult>('codex.import', { path })
    const before = await client.request<Page<RunEvent>>('runs.events', { runId: first.run.id })
    await client.stop()
    client = new EngineClient(executable, 5000, ['--database', database])
    await client.start()
    assert.deepEqual(await client.request<Run>('runs.get', { runId: first.run.id }), first.run)
    assert.deepEqual(await client.request('codex.directories.list'), dirs)
    const again = await client.request<ScanStatus>('codex.scan.start', {
      directoryId: dirs.items[0].id
    })
    assert.equal((await waitForScan(again.id)).ready, 1)
    const candidates = await client.request<Page<{ id: string; existing: boolean }>>(
      'codex.scan.candidates',
      { id: again.id }
    )
    assert.equal(candidates.items[0].existing, true)
    await client.request('codex.scan.import', { id: again.id, ids: [first.run.id] })
    assert.equal((await waitForScan(again.id)).updated, 1)
    assert.deepEqual(await readFile(path), original)

    const messages = Array.from({ length: 121 }, (_, n) =>
      JSON.stringify({
        type: 'response_item',
        payload: { type: 'message', role: 'user', content: `stored message ${n} Ω` }
      })
    ).join('\n')
    await writeFile(path, original.toString().split('\n')[0] + '\n' + messages + '\n')
    const updated = await client.request<ImportResult>('codex.update', { runId: first.run.id })
    assert.equal(updated.replaced, true)
    assert.notEqual(updated.run.importInfo?.snapshotId, first.run.importInfo?.snapshotId)
    assert.equal(updated.run.eventCount, 121)
    await writeFile(path, 'invalid source')
    await assert.rejects(client.request('codex.update', { runId: first.run.id }), /IMPORT_FAILED/)
    assert.deepEqual(await client.request<Run>('runs.get', { runId: first.run.id }), updated.run)
    await rm(path)
    await client.stop()
    client = new EngineClient(executable, 5000, ['--database', database])
    await client.start()
    const page = await client.request<Page<RunEvent>>('runs.events', {
      runId: first.run.id,
      offset: 100,
      limit: 10
    })
    assert.equal(page.total, 121)
    assert.equal(page.items.length, 10)
    assert.equal(page.nextOffset, 110)
    assert.equal(page.items[0].sequence, 101)
    assert.equal(page.items[0].evidence.snapshotId, updated.run.importInfo?.snapshotId)
    const match = await client.request<Page<RunEvent>>('runs.events', {
      runId: first.run.id,
      search: '120 ω',
      kind: 'message'
    })
    assert.equal(match.total, 1)
    assert.equal(match.items[0].sequence, 121)
    assert.equal(match.items[0].evidence.line, 122)
    assert.notDeepEqual(match.items, before.items)
    await assert.rejects(client.request('codex.update', { runId: first.run.id }), /IMPORT_FAILED/)
    // Saved roots must not silently follow a later link into another directory.
    const outside = resolve(directory, 'other-root')
    await mkdir(outside)
    await writeFile(resolve(outside, 'other.jsonl'), original)
    await rm(source, { recursive: true, force: true })
    await symlink(outside, source, process.platform === 'win32' ? 'junction' : 'dir')
    await assert.rejects(
      client.request('codex.scan.start', { directoryId: dirs.items[0].id }),
      /SCAN_FAILED.*路径身份发生变化/
    )
    assert.deepEqual(await client.request('codex.directories.list'), dirs)
    await client.request('codex.directories.remove', { id: dirs.items[0].id })
    await assert.rejects(
      client.request('codex.scan.start', { directoryId: dirs.items[0].id }),
      /NOT_FOUND/
    )
    assert.equal((await client.request<Page<Run>>('runs.list', { source: 'codex' })).total, 1)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})

test('invalid database fails startup without deleting or replacing user bytes', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-invalid-library-'))
  const database = resolve(directory, 'library.sqlite')
  const invalid = Buffer.from('this database must be preserved')
  await writeFile(database, invalid)
  const client = new EngineClient(executable, 5000, ['--database', database])
  try {
    await assert.rejects(client.start(), /已退出|输入已关闭/)
    assert.deepEqual(await readFile(database), invalid)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
