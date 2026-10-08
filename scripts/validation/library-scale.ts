import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rename, rm, stat, writeFile } from 'node:fs/promises'
import { cpus, release, totalmem } from 'node:os'
import { dirname, isAbsolute, relative, resolve } from 'node:path'
import { promisify } from 'node:util'
import { _electron as electron, expect } from '@playwright/test'
import { EngineClient } from '../../apps/desktop/src/main/engine-client.ts'
import type { ImportResult, Page, Run, RunEvent } from '../../contracts/index.ts'

const exec = promisify(execFile)
const hash = (data: Buffer | string) => createHash('sha256').update(data).digest('hex')
const row = (type: string, payload: object, timestamp = '2026-01-01T00:00:00Z') =>
  JSON.stringify({ timestamp, type, payload }) + '\n'
const p95 = (values: number[]) =>
  [...values].sort((a, b) => a - b)[Math.ceil(values.length * 0.95) - 1]
const summarize = (values: number[]) => ({
  samples: values.map((n) => Math.round(n * 100) / 100),
  p95: p95(values),
  max: Math.max(...values)
})

function fixture(task: number, count: number) {
  let text = row(
    'session_meta',
    { id: `scale-${task}`, cwd: 'synthetic-scale', cli_version: 'synthetic' },
    `2026-01-${task === 0 ? '02' : '01'}T00:00:00Z`
  )
  for (let n = 1; n <= count; n++) {
    const content = `scale task ${String(task).padStart(4, '0')} event ${n} ` + '中Ω'.repeat(40)
    const payload =
      n === count
        ? {
            type: 'message',
            role: 'assistant',
            content: content + '末尾证据' + '长正文🙂'.repeat(2500)
          }
        : n % 3 === 2
          ? {
              type: 'function_call',
              name: 'synthetic_test',
              call_id: `call-${n}`,
              arguments: JSON.stringify({ text: content })
            }
          : n % 3 === 0
            ? { type: 'function_call_output', call_id: `call-${n - 1}`, output: content }
            : {
                type: 'message',
                role: n === 1 ? 'user' : 'assistant',
                content
              }
    text += row('response_item', payload, `2026-01-${task === 0 ? '02' : '01'}T00:00:00Z`)
  }
  return text
}

// Read only our own test app/engine processes. No process command lines are logged.
async function engineMemory(parent: number) {
  assert.ok(Number.isInteger(parent) && parent > 0)
  const { stdout } = await exec(
    'powershell.exe',
    [
      '-NoProfile',
      '-Command',
      `$p=Get-CimInstance Win32_Process -Filter 'ParentProcessId=${parent}' | Where-Object Name -eq 'jeval-engine.exe'; if (@($p).Count -ne 1) { throw ('Expected one test engine under ${parent}; found ' + @($p).Count) }; $q=Get-Process -Id $p.ProcessId; @{workingSetBytes=$q.WorkingSet64;peakWorkingSetBytes=$q.PeakWorkingSet64} | ConvertTo-Json -Compress`
    ],
    { windowsHide: true }
  )
  return JSON.parse(stdout) as { workingSetBytes: number; peakWorkingSetBytes: number }
}

export async function measureLibraryScale(mode: 'baseline' | 'target') {
  assert.equal(process.platform, 'win32', 'Windows measurement only')
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  assert.ok(packaged, 'Use an actual packaged desktop for scale acceptance')
  const desktop = resolve(packaged)
  const engine = resolve(
    process.env.JEVAL_SCALE_ENGINE ?? resolve(dirname(desktop), 'resources/engine/jeval-engine.exe')
  )
  const root = resolve('.local')
  await mkdir(root, { recursive: true })
  const work = await mkdtemp(resolve(root, 'scale-'))
  const profile = resolve(work, 'profile')
  await mkdir(profile)
  const sourceRoot = resolve(work, 'sources')
  await mkdir(sourceRoot)
  const database = resolve(profile, 'library.sqlite')
  const tasks = mode === 'baseline' ? 20 : 1000
  const largeCount = mode === 'baseline' ? 5000 : 10000
  const smallCount = mode === 'baseline' ? 2000 : 40
  const totalEvents = largeCount + (tasks - 1) * smallCount
  const seed = new EngineClient(engine, 30000, ['--database', database])
  const env = Object.fromEntries(
    Object.entries(process.env).filter((entry): entry is [string, string] => entry[1] !== undefined)
  )
  delete env.ELECTRON_RUN_AS_NODE
  const launch = () =>
    electron.launch({ executablePath: desktop, args: [`--user-data-dir=${profile}`], env })
  let application: Awaited<ReturnType<typeof launch>> | undefined
  const sourceHashes: string[] = []
  let sourceBytes = 0
  let largest!: Run
  const indexedAt = performance.now()
  try {
    await seed.start()
    for (let i = 0; i < tasks; i++) {
      const text = fixture(i, i === 0 ? largeCount : smallCount)
      const path = resolve(sourceRoot, `${i}.jsonl`)
      await writeFile(path, text, { flag: 'wx' })
      sourceHashes.push(hash(text))
      sourceBytes += Buffer.byteLength(text)
      const imported = await seed.request<ImportResult>('codex.import', { path })
      assert.equal(imported.run.eventCount, i === 0 ? largeCount : smallCount)
      if (i === 0) largest = imported.run
    }
    const importMs = performance.now() - indexedAt
    console.log('scale: seeded', tasks, 'parent', process.pid)
    const importMemory = await engineMemory(process.pid)
    const list = await seed.request<Page<Run>>('runs.list', { source: 'codex', limit: 100 })
    assert.equal(list.total, tasks)
    const taskIDs = new Set<string>()
    for (let offset = 0; ; ) {
      const page = await seed.request<Page<Run>>('runs.list', {
        source: 'codex',
        offset,
        limit: 100
      })
      for (const run of page.items) {
        assert.ok(!taskIDs.has(run.id))
        taskIDs.add(run.id)
      }
      if (page.nextOffset === null) break
      offset = page.nextOffset
    }
    assert.equal(taskIDs.size, tasks)
    let seen = 0
    for (let offset = 0; ; ) {
      const page = await seed.request<Page<RunEvent>>('runs.events', {
        runId: largest.id,
        offset,
        limit: 100
      })
      assert.equal(page.total, largeCount)
      for (const e of page.items) {
        assert.equal(e.sequence, ++seen)
        assert.equal(e.evidence.line, e.sequence + 1)
        assert.equal(e.evidence.snapshotId, largest.importInfo!.snapshotId)
        if (e.kind === 'tool_result') assert.ok(e.parentId)
      }
      if (page.nextOffset === null) break
      offset = page.nextOffset
    }
    assert.equal(seen, largeCount)
    const bundle = resolve(work, 'large.jeval.json')
    await seed.request('records.export', { runId: largest.id, path: bundle, format: 'json' })
    const restore = new EngineClient(engine, 30000, [
      '--database',
      resolve(work, 'exchange.sqlite')
    ])
    try {
      await restore.start()
      const imported = await restore.request<ImportResult>('records.import', { path: bundle })
      assert.equal(imported.run.eventCount, largeCount)
      assert.equal(imported.run.importInfo!.snapshotId, largest.importInfo!.snapshotId)
    } finally {
      await restore.stop()
    }
    const stopStart = performance.now()
    await seed.stop()
    const seedStopMs = performance.now() - stopStart
    const databaseBytes = (await stat(database)).size
    // Startup and browsing must not require the original source files.
    await rename(sourceRoot, resolve(work, 'offline'))
    const startup: number[] = [],
      taskScreen: number[] = [],
      eventScreen: number[] = [],
      query: number[] = [],
      close: number[] = []
    for (let n = 0; n < 10; n++) {
      const begin = performance.now()
      application = await launch()
      const page = await application.firstWindow()
      await expect(page.getByText('本地引擎已连接')).toBeVisible()
      await expect(page.getByRole('button', { name: 'Codex 本地', exact: true })).toBeEnabled()
      startup.push(performance.now() - begin)
      if (n < 9) {
        const startClose = performance.now()
        await application.close()
        application = undefined
        close.push(performance.now() - startClose)
      }
    }
    const page = await application!.firstWindow()
    const memory = async () => ({
      electron: await application!.evaluate(({ app }) =>
        app.getAppMetrics().map((p) => ({
          type: p.type,
          workingSetKiB: p.memory.workingSetSize,
          peakWorkingSetKiB: p.memory.peakWorkingSetSize
        }))
      ),
      engine: await engineMemory(await application!.evaluate(() => process.pid))
    })
    console.log('scale: startup measured')
    const memoryBefore = await memory()
    for (let n = 0; n < 20; n++) {
      await page.getByRole('button', { name: /^合成演示/ }).click()
      await expect(page.locator('.run-card')).toHaveCount(3)
      const begin = performance.now()
      await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
      await expect(page.locator('.run-card')).toHaveCount(10)
      await page.evaluate(
        () =>
          new Promise<void>((done) =>
            requestAnimationFrame(() => requestAnimationFrame(() => done()))
          )
      )
      taskScreen.push(performance.now() - begin)
      const start = performance.now()
      await page.locator('.run-card').first().click()
      await expect(page.locator('.event')).toHaveCount(50)
      await page.evaluate(
        () =>
          new Promise<void>((done) =>
            requestAnimationFrame(() => requestAnimationFrame(() => done()))
          )
      )
      eventScreen.push(performance.now() - start)
      query.push(
        await page.evaluate(async (runId) => {
          const t = performance.now()
          const result = await window.jeval.listEvents({ runId, limit: 50 })
          if (result.items.length !== 50) throw new Error('short first page')
          return performance.now() - t
        }, largest.id)
      )
    }
    const paging: number[] = []
    for (let n = 0; n < 20; n++) {
      const begin = performance.now()
      await page.getByRole('button', { name: '下一页事件', exact: true }).click()
      await expect(page.locator('.event-topline > span:last-child').first()).toHaveText(
        `#${(51 + n * 50).toString().padStart(2, '0')}`
      )
      assert.equal(await page.locator('.event').count(), 50)
      await page.locator('.event').last().scrollIntoViewIfNeeded()
      paging.push(performance.now() - begin)
    }
    const memoryAfter = await memory()
    await page.getByRole('textbox', { name: '搜索记录内容' }).fill('末尾证据')
    await expect(page.locator('.event')).toHaveCount(1)
    await expect(page.locator('.event-topline > span:last-child')).toHaveText(`#${largeCount}`)
    await page.getByRole('button', { name: '查看完整正文', exact: true }).click()
    await expect(page.locator('.full-content-text')).toContainText('末尾证据')
    // Restore only our generated source, then race a real UI cancel against bounded parsing.
    await rename(resolve(work, 'offline'), sourceRoot)
    const source = resolve(sourceRoot, '0.jsonl')
    const original = await readFile(source)
    const padding = row('turn_context', { padding: 'x'.repeat(220) }).repeat(35000)
    assert.ok(original.length + Buffer.byteLength(padding) < 16 * 1024 * 1024)
    await writeFile(source, Buffer.concat([original, Buffer.from(padding)]))
    const cancel = await page.evaluate(
      () =>
        new Promise<{ feedbackMs: number; completedMs: number }>((resolve, reject) => {
          let clicked = 0,
            feedback = 0
          const timeout = setTimeout(() => {
            observer.disconnect()
            reject(new Error('cancel did not complete'))
          }, 10000)
          const observer = new MutationObserver(() => {
            const button = [...document.querySelectorAll('button')].find(
              (b) => b.textContent?.trim() === '取消更新'
            )
            if (!clicked && button) {
              clicked = performance.now()
              button.click()
            }
            const text = document.body.innerText
            if (
              clicked &&
              !feedback &&
              (text.includes('正在取消更新') || text.includes('更新已取消'))
            )
              feedback = performance.now() - clicked
            if (clicked && text.includes('更新已取消')) {
              clearTimeout(timeout)
              observer.disconnect()
              resolve({ feedbackMs: feedback, completedMs: performance.now() - clicked })
            }
          })
          observer.observe(document.body, { subtree: true, childList: true, characterData: true })
          const update = [...document.querySelectorAll('button')].find(
            (b) => b.textContent?.trim() === '更新已登记记录'
          )
          if (!update) {
            clearTimeout(timeout)
            observer.disconnect()
            reject(new Error('update button missing'))
            return
          }
          update.click()
        })
    )
    assert.equal(
      (await page.evaluate((id) => window.jeval.getRun(id), largest.id)).importInfo!.snapshotId,
      largest.importInfo!.snapshotId
    )
    await writeFile(source, original)
    for (let i = 0; i < tasks; i++)
      assert.equal(hash(await readFile(resolve(sourceRoot, `${i}.jsonl`))), sourceHashes[i])
    const cancelMemory = await memory()
    await page
      .getByRole('textbox', { name: '搜索任务', exact: true })
      .fill(`scale task ${String(tasks - 1).padStart(4, '0')}`)
    await expect(page.locator('.run-card')).toHaveCount(1)
    await page.locator('.run-card').click()
    await expect(page.locator('.event')).toHaveCount(Math.min(50, smallCount))
    const closeStart = performance.now()
    await application!.close()
    application = undefined
    close.push(performance.now() - closeStart)
    const verify = new EngineClient(engine, 30000, ['--database', database])
    try {
      await verify.start()
      const current = await verify.request<Run>('runs.get', { runId: largest.id })
      assert.equal(current.importInfo!.snapshotId, largest.importInfo!.snapshotId)
    } finally {
      await verify.stop()
    }
    const { stdout: commit } = await exec('git', ['rev-parse', 'HEAD'])
    const { stdout: dirty } = await exec('git', ['status', '--porcelain'])
    const { stdout: productDiff } = await exec('git', [
      'diff',
      'HEAD',
      '--',
      'engine',
      'apps',
      'contracts'
    ])
    const appPackage = JSON.parse(await readFile(resolve('apps/desktop/package.json'), 'utf8'))
    const report = {
      schemaVersion: 1,
      generatedAt: new Date().toISOString(),
      mode,
      generator: 'library-scale-v1',
      synthetic: true,
      environment: {
        platform: process.platform,
        osRelease: release(),
        cpu: cpus()[0].model,
        logicalCPUs: cpus().length,
        ramBytes: totalmem(),
        node: process.version,
        electron: appPackage.devDependencies.electron,
        appVersion: appPackage.version
      },
      revision: commit.trim(),
      workingTreeDirty: dirty.trim() !== '',
      productDiffSHA256: hash(productDiff),
      measurementScriptSHA256: hash(await readFile(resolve('scripts/validation/library-scale.ts'))),
      artifacts: {
        engineSHA256: hash(await readFile(engine)),
        desktopSHA256: hash(await readFile(desktop)),
        asarSHA256: hash(await readFile(resolve(dirname(desktop), 'resources/app.asar')))
      },
      corpus: {
        tasks,
        largeCount,
        totalEvents,
        sourceBytes,
        aggregateSHA256: hash(sourceHashes.join('\n')),
        databaseBytes
      },
      measurements: {
        importMs,
        seedStopMs,
        processColdStartupMs: summarize(startup),
        taskScreenMs: summarize(taskScreen),
        eventScreenMs: summarize(eventScreen),
        hotQueryMs: summarize(query),
        pageAndScrollMs: summarize(paging),
        cancel,
        closeMs: summarize(close),
        importMemory,
        memoryBefore,
        memoryAfter,
        cancelMemory
      },
      thresholds: { startupMaxMs: 3000, firstScreenP95Ms: 1000, cancelFeedbackMs: 1000 },
      passed:
        Math.max(...startup) <= 3000 &&
        p95(taskScreen) <= 1000 &&
        p95(eventScreen) <= 1000 &&
        cancel.feedbackMs <= 1000,
      caveats: [
        'Fresh app processes; OS filesystem caches are not flushed. First and repeated launches are retained.',
        'revision identifies the measurement checkout; artifact hashes identify the actual compiled binaries.',
        'Memory samples include Electron processes and the owned engine; per-process lifetime peaks are not a simultaneous aggregate peak.',
        'Synthetic scale evidence does not extend source compatibility or clean-machine acceptance.'
      ]
    }
    return report
  } finally {
    await seed.stop()
    await application?.close()
    const child = relative(root, work)
    assert.ok(child && !child.startsWith('..') && !isAbsolute(child))
    await rm(work, { recursive: true, force: true })
  }
}

async function main() {
  const [mode, output] = process.argv.slice(2)
  assert.ok(mode === 'baseline' || mode === 'target')
  assert.ok(output)
  const report = await measureLibraryScale(mode)
  await mkdir(dirname(resolve(output)), { recursive: true })
  await writeFile(output, JSON.stringify(report, null, 2) + '\n')
  console.log(
    JSON.stringify({
      mode,
      passed: report.passed,
      corpus: report.corpus,
      startup: report.measurements.processColdStartupMs,
      task: report.measurements.taskScreenMs.p95,
      event: report.measurements.eventScreenMs.p95,
      cancel: report.measurements.cancel
    })
  )
  if (!report.passed) process.exitCode = 1
}

if (process.argv[1]?.endsWith('library-scale.ts'))
  void main().catch((e) => {
    console.error(e)
    process.exitCode = 1
  })
