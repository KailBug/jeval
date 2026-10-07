import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises'
import { dirname, isAbsolute, relative, resolve } from 'node:path'
import { _electron as electron, expect } from '@playwright/test'
import { auditSources, type AuditSample } from './codex-source-audit.ts'

export async function auditDesktop(manifestPath: string) {
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  // Enforce byte/date qualification and evidence checks before showing private data.
  const engine = resolve(
    process.env.JEVAL_AUDIT_EXECUTABLE ??
      (packaged ? 'release/win-unpacked/resources/engine/jeval-engine.exe' : 'bin/jeval-engine.exe')
  )
  const sourceReport = await auditSources(manifestPath, engine)
  const manifest: { samples: AuditSample[] } = JSON.parse(await readFile(manifestPath, 'utf8'))
  const root = resolve('.local')
  await mkdir(root, { recursive: true })
  const work = await mkdtemp(resolve(root, 'desktop-source-audit-'))
  const env = Object.fromEntries(
    Object.entries(process.env).filter((entry): entry is [string, string] => entry[1] !== undefined)
  )
  delete env.ELECTRON_RUN_AS_NODE
  const launch = () =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [
        ...(packaged ? [] : [resolve('apps/desktop')]),
        `--user-data-dir=${resolve(work, 'profile')}`
      ],
      env
    })
  let application: Awaited<ReturnType<typeof launch>> | undefined
  let sampleID = 'manifest'
  let phase = 'launch'
  const results = []
  try {
    application = await launch()
    let page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    for (const sample of manifest.samples) {
      sampleID = sample.id
      phase = 'import'
      const source = resolve(dirname(manifestPath), sample.path)
      const bytes = await readFile(source)
      assert.equal(createHash('sha256').update(bytes).digest('hex'), sample.sha256)
      const copy = resolve(work, `${sample.id}.jsonl`)
      await writeFile(copy, bytes, { flag: 'wx' })
      await application.evaluate(({ dialog }, path) => {
        dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
      }, copy)
      const button = page.getByRole('button', { name: '导入 Codex 记录', exact: true })
      await button.click()
      await expect(button).toBeEnabled()
      const run = await page.evaluate(
        async (hash) =>
          (await window.jeval.listRuns({ source: 'codex' })).items.find(
            (run) => run.importInfo?.sha256 === hash
          ),
        sample.sha256
      )
      assert.ok(run)
      const expected = sourceReport.results.find((row) => row.id === sample.id)!
      assert.equal(run.eventCount, expected.eventCount)
      await expect(page.locator('.run-detail')).toContainText(run.title)
      phase = 'render-pages'
      const sequences: number[] = []
      if (run.eventCount > 0) {
        let next = 1
        while (next <= run.eventCount) {
          await expect(page.locator('.event-topline > span:last-child').first()).toHaveText(
            `#${String(next).padStart(2, '0')}`
          )
          const numbers = await page.locator('.event-topline > span:last-child').allTextContents()
          sequences.push(...numbers.map((value) => Number(value.slice(1))))
          next += numbers.length
          if (next <= run.eventCount)
            await page.getByRole('button', { name: '下一页事件', exact: true }).click()
        }
      } else await expect(page.getByText('这条记录没有可展示的事件')).toBeVisible()
      assert.deepEqual(
        sequences,
        Array.from({ length: run.eventCount }, (_, i) => i + 1)
      )
      phase = 'update'
      await page.getByRole('button', { name: '更新已登记记录', exact: true }).click()
      await expect(
        page.getByText(`来源未变化 · ${run.eventCount} 个事件`, { exact: true })
      ).toBeVisible()
      assert.equal(
        createHash('sha256')
          .update(await readFile(source))
          .digest('hex'),
        sample.sha256
      )
      results.push({
        id: sample.id,
        events: run.eventCount,
        renderedSequences: sequences.length,
        checks: ['import-button', 'all-event-pages', 'update-button', 'source-unchanged']
      })
    }
    phase = 'offline-restart'
    await application.close()
    application = undefined
    for (const sample of manifest.samples)
      await rename(resolve(work, `${sample.id}.jsonl`), resolve(work, `${sample.id}.offline`))
    application = await launch()
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(manifest.samples.length)
    const counts = await page.evaluate(async () => {
      const runs = (await window.jeval.listRuns({ source: 'codex' })).items
      return Promise.all(
        runs.map(async (run) => ({
          hash: run.importInfo!.sha256,
          total: (await window.jeval.listEvents({ runId: run.id })).total
        }))
      )
    })
    for (const sample of manifest.samples)
      assert.equal(
        counts.find((row) => row.hash === sample.sha256)?.total,
        sourceReport.results.find((row) => row.id === sample.id)!.eventCount
      )
    return { schemaVersion: 1, packaged: !!packaged, offlineRestart: true, results }
  } catch {
    throw new Error(
      `Desktop source audit failed: ${sampleID}/${phase}; no transcript details logged`
    )
  } finally {
    await application?.close()
    const child = relative(root, work)
    assert.ok(child && !child.startsWith('..') && !isAbsolute(child))
    await rm(work, { recursive: true, force: true })
  }
}
