import assert from 'node:assert/strict'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { ImportResult, Page, RunEvent, UpdateStatus } from '../../contracts/index.ts'

const executable = resolve(
  process.env.JEVAL_CHECKPOINT_EXECUTABLE ??
    `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
)
const row = (type: string, payload: object) => JSON.stringify({ type, payload }) + '\n'

test('persistent checkpoint updates equal fresh full imports across process restarts and rewrites', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-checkpoint-'))
  const path = resolve(directory, '中文 source.jsonl')
  const create = (name: string) =>
    new EngineClient(executable, 5000, ['--database', resolve(directory, name)])
  let client = create('library.sqlite')
  const prefix =
    row('session_meta', { id: 'checkpoint', history_mode: 'paginated' }) +
    row('response_item', { type: 'message', role: 'user', content: 'checkpoint question' }) +
    row('response_item', { type: 'function_call', call_id: 'call', name: 'test', arguments: '{}' })
  const output = row('response_item', {
    type: 'function_call_output',
    call_id: 'call',
    output: 'ok'
  })
  const projection = row('event_msg', {
    type: 'item_completed',
    item: { type: 'AgentMessage', id: 'answer', content: 'answer' }
  })
  const canonical = row('response_item', {
    type: 'message',
    id: 'answer',
    role: 'assistant',
    content: 'answer'
  })
  const cases: [string, string][] = [
    [prefix + output, 'incremental'],
    [prefix + output, 'unchanged'],
    [prefix + output + '{"type":', 'incremental'],
    [prefix + output + projection, 'full'],
    [prefix + output + projection + canonical, 'full'],
    [prefix, 'full'],
    [prefix.replace('question', 'rewritten'), 'full']
  ]
  try {
    await writeFile(path, prefix)
    await client.start()
    const first = await client.request<ImportResult>('codex.import', { path })
    for (const [index, [contents, mode]] of cases.entries()) {
      await client.stop()
      client = create('library.sqlite')
      await client.start()
      await writeFile(path, contents)
      let status = await client.request<UpdateStatus>('codex.update.start', { runId: first.run.id })
      for (let n = 0; n < 300 && ['running', 'cancelling'].includes(status.state); n++) {
        await new Promise((done) => setTimeout(done, 10))
        status = await client.request<UpdateStatus>('codex.update.status', { id: status.id })
      }
      assert.equal(status.state, 'completed', status.message)
      assert.equal(status.report?.mode, mode)
      if (mode === 'unchanged') assert.equal(status.report.parsedLines, 0)
      const baseline = create(`baseline-${index}.sqlite`)
      try {
        await baseline.start()
        const full = await baseline.request<ImportResult>('codex.import', { path })
        assert.deepEqual(status.run, full.run)
        const params = { runId: first.run.id }
        assert.deepEqual(
          await client.request<Page<RunEvent>>('runs.events', params),
          await baseline.request<Page<RunEvent>>('runs.events', params)
        )
      } finally {
        await baseline.stop()
      }
      assert.equal(await readFile(path, 'utf8'), contents)
    }
    const before = await client.request('runs.get', { runId: first.run.id })
    await rm(path)
    let failed = await client.request<UpdateStatus>('codex.update.start', { runId: first.run.id })
    for (let n = 0; n < 300 && failed.state === 'running'; n++) {
      await new Promise((done) => setTimeout(done, 10))
      failed = await client.request<UpdateStatus>('codex.update.status', { id: failed.id })
    }
    assert.equal(failed.state, 'failed')
    assert.deepEqual(await client.request('runs.get', { runId: first.run.id }), before)
  } finally {
    await client.stop()
    await rm(directory, { recursive: true, force: true })
  }
})
