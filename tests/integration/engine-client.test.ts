import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { Page, Run, RunEvent } from '../../contracts/index.ts'

const executable = resolve(
  'bin',
  process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine'
)

test('desktop transport handshakes, matches concurrent responses, and preserves record evidence', async () => {
  const client = new EngineClient(executable)
  try {
    const hello = await client.start()
    assert.equal(hello.protocolVersion, 1)
    assert.equal(hello.recordVersion, 1)
    const [runs, unknown] = await Promise.all([
      client.request<Page<Run>>('runs.list', { limit: 1 }),
      client.request<Page<Run>>('runs.list', { status: 'unknown' })
    ])
    assert.equal(runs.total, 3)
    assert.equal(runs.nextOffset, 1)
    assert.equal(unknown.items[0].tokens, null)
    assert.equal(unknown.items[0].startedAt, null)
    const page = await client.request<Page<RunEvent>>('runs.events', {
      runId: runs.items[0].id,
      limit: 2
    })
    assert.equal(page.total, runs.items[0].eventCount)
    assert.equal(page.items.length, 2)
    assert.equal(page.nextOffset, 2)
    assert.equal(page.items[0].evidence.sourceId, 'demo')
    assert.equal(page.items[0].runId, runs.items[0].id)
    await assert.rejects(client.request('runs.events', { runId: 'missing' }), /NOT_FOUND/)
    assert.equal((await client.request<Page<Run>>('runs.list')).total, 3)
  } finally {
    await client.stop()
  }
  await assert.rejects(client.request('hello'), /已退出|已停止|未就绪/)
})

test('missing engine fails clearly and can be stopped', async () => {
  const client = new EngineClient(resolve('bin', 'missing-engine'))
  await assert.rejects(client.start(), /无法启动|输入已关闭/)
  await client.stop()
})

test('Go accepts a fragmented UTF-8 request and multiple requests in a single chunk', async () => {
  const child = spawn(executable, [], { windowsHide: true })
  let output = ''
  child.stdout.setEncoding('utf8')
  child.stdout.on('data', (chunk) => {
    output += chunk
  })
  const exited = new Promise<number | null>((resolve, reject) => {
    child.on('error', reject)
    child.on('close', resolve)
  })
  const search = Buffer.from(
    JSON.stringify({
      type: 'request',
      version: 1,
      id: 'split',
      method: 'runs.list',
      params: { search: '缓存' }
    }) + '\n'
  )
  const split = search.indexOf(Buffer.from('缓存')) + 1
  child.stdin.write(search.subarray(0, split))
  child.stdin.end(
    Buffer.concat([
      search.subarray(split),
      Buffer.from('{"type":"request","version":1,"id":"stop","method":"shutdown"}\n')
    ])
  )
  const timer = setTimeout(() => child.kill(), 5000)
  try {
    assert.equal(await exited, 0)
    const frames = output
      .trim()
      .split('\n')
      .map((line) => JSON.parse(line))
    assert.equal(frames.length, 2)
    assert.equal(frames[0].id, 'split')
    assert.equal(frames[0].result.items[0].id, 'demo-cache')
    assert.equal(frames[1].result.ok, true)
  } finally {
    clearTimeout(timer)
    child.kill()
  }
})
