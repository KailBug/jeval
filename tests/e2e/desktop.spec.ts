import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'
import { mkdir, mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import type { NativeRecordEnvelope, RunEvent } from '../../contracts/index'
import { createHash } from 'node:crypto'

test('Codex discovery previews without importing, supports selection/all, and cancels', async () => {
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-scan-fixtures-'))
  const root = resolve(directory, '中文 会话')
  await mkdir(resolve(root, 'nested'), { recursive: true })
  const fixture = await readFile(resolve('fixtures/adapters/codex/classic.jsonl'))
  await writeFile(resolve(root, 'nested/session.jsonl'), fixture)
  await writeFile(
    resolve(root, 'paginated.jsonl'),
    await readFile(resolve('fixtures/adapters/codex/paginated.jsonl'))
  )
  await writeFile(resolve(root, 'broken.jsonl'), 'invalid')
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const application = await electron.launch({
    ...(packaged ? { executablePath: resolve(packaged) } : {}),
    args: [
      ...(packaged ? [] : [resolve('apps/desktop')]),
      `--user-data-dir=${resolve('.local', `e2e-scan-${Date.now()}`)}`
    ],
    env
  })
  try {
    const page = await application.firstWindow()
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await application.evaluate(({ dialog }) => {
      dialog.showOpenDialog = async () => ({ canceled: true, filePaths: [] })
    })
    const discover = page.getByRole('button', { name: '发现本地任务', exact: true })
    await discover.click()
    await expect(discover).toBeEnabled()
    await expect(page.getByRole('region', { name: 'Codex 目录扫描' })).toHaveCount(0)
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async (_window, options) => {
        if (!options.properties?.includes('openDirectory'))
          throw new Error('directory selection required')
        return { canceled: false, filePaths: [path] }
      }
    }, root)
    await discover.click()
    const panel = page.getByRole('region', { name: 'Codex 目录扫描' })
    const picker = page.getByRole('dialog', { name: '选择要导入的记录' })
    await expect(picker).toBeVisible()
    await expect(picker.getByRole('button', { name: '导入所选（0）', exact: true })).toBeDisabled()
    await expect(picker.getByRole('checkbox', { name: '全选记录', exact: true })).not.toBeChecked()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(0)
    await picker.getByText('查看失败项', { exact: false }).click()
    await expect(picker).toContainText('broken.jsonl')
    await expect(picker.getByRole('region', { name: '候选记录概要' })).toBeVisible()
    expect(await picker.evaluate((el) => el.contains(document.activeElement))).toBe(true)
    await page.screenshot({ path: 'test-results/codex-scan-1440.png' })
    await application.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    )
    await page.screenshot({ path: 'test-results/codex-scan-1000.png' })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
    ).toBe(true)
    expect(await picker.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
    expect(
      await picker.locator('.scan-dialog-footer').evaluate((el) => {
        const box = el.getBoundingClientRect()
        return box.bottom <= window.innerHeight && box.top >= 0
      })
    ).toBe(true)
    await page.keyboard.press('Escape')
    await expect(picker).not.toBeVisible()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(0)
    await page.getByRole('button', { name: '浏览扫描结果', exact: true }).click()
    await picker.getByLabel('搜索待导入记录').fill('检查示例')
    await expect(picker.locator('.scan-candidate')).toHaveCount(1)
    await picker.getByRole('checkbox', { name: '选择 检查示例项目的测试结果', exact: true }).check()
    await picker.getByRole('button', { name: '导入所选（1）', exact: true }).click()
    await expect(picker.getByRole('button', { name: '关闭选择', exact: true })).toBeEnabled()
    await expect(
      picker.getByRole('checkbox', { name: '选择 检查示例项目的测试结果', exact: true })
    ).toBeDisabled()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(1)
    await picker.getByRole('button', { name: '关闭选择', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(1)
    await discover.click()
    await expect(picker).toBeVisible()
    await picker.getByRole('checkbox', { name: '全选记录', exact: true }).check()
    await picker.getByRole('button', { name: '导入所选（2）', exact: true }).click()
    await expect(picker.getByRole('button', { name: '关闭选择', exact: true })).toBeEnabled()
    await expect(picker.getByRole('button', { name: '导入所选（0）', exact: true })).toBeDisabled()
    await picker.getByRole('button', { name: '关闭选择', exact: true }).click()
    await expect(panel).toContainText('新增 1 · 更新 1 · 失败 0')
    await expect(page.locator('.run-card')).toHaveCount(2)

    const large = resolve(directory, 'large')
    await mkdir(large)
    const message = JSON.stringify({
      type: 'response_item',
      payload: { type: 'message', role: 'user', content: 'x'.repeat(7000) }
    })
    await writeFile(
      resolve(large, 'large.jsonl'),
      fixture.toString().split('\n')[0] + '\n' + (message + '\n').repeat(2000)
    )
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, large)
    await discover.click()
    await page.getByRole('button', { name: '取消扫描', exact: true }).click()
    await expect(picker).toBeVisible()
    await expect(picker).toContainText('扫描已取消；可查看已发现的记录')
    await picker.getByRole('button', { name: '关闭选择', exact: true }).click()
    await expect(discover).toBeEnabled()
    await expect(page.locator('.run-card')).toHaveCount(2)
    await page.getByRole('button', { name: '合成演示 3', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(3)
    expect(errors).toEqual([])
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})

test('native record export/import preserves full saved preview offline without authorizing source paths', async () => {
  test.setTimeout(60000)
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-exchange-'))
  const sourceFile = resolve(directory, 'synthetic-source.jsonl')
  const jsonFile = resolve(directory, 'round-trip.jeval.json')
  const markdownFile = resolve(directory, 'round-trip.md')
  const cancelledFile = resolve(directory, 'cancelled-export.jeval.json')
  const invalidFile = resolve(directory, 'invalid.jeval.json')
  const sourceLines = [
    JSON.stringify({
      type: 'session_meta',
      payload: { id: 'synthetic-exchange', cwd: 'D:\\synthetic-exchange', cli_version: 'synthetic' }
    }),
    JSON.stringify({
      type: 'response_item',
      payload: { type: 'message', role: 'user', content: '交换任务 · 导出全部快照' }
    }),
    JSON.stringify({
      type: 'response_item',
      payload: {
        type: 'function_call',
        name: 'synthetic.tool',
        arguments: '{}',
        call_id: 'exchange-call'
      }
    }),
    JSON.stringify({
      type: 'response_item',
      payload: { type: 'function_call_output', call_id: 'exchange-call', output: '合成工具结果' }
    })
  ]
  for (let index = 3; index < 120; index++)
    sourceLines.push(
      JSON.stringify({
        type: 'response_item',
        payload: {
          type: 'message',
          role: 'assistant',
          content: index === 4 ? '截断测试 ' + '字'.repeat(9000) : `交换事件 ${index}`
        }
      })
    )
  const sourceBytes = sourceLines.join('\n') + '\n'
  await writeFile(sourceFile, sourceBytes)
  await writeFile(cancelledFile, 'cancelled export must preserve these bytes')
  await writeFile(invalidFile, '{}')
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = (userData: string) =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [...(packaged ? [] : [resolve('apps/desktop')]), `--user-data-dir=${userData}`],
      env
    })
  let application = await launch(resolve(directory, 'origin-library'))
  try {
    let page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, sourceFile)
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    let detail = page.getByRole('article', { name: '执行详情' })
    await expect(
      detail.getByRole('heading', { name: '交换任务 · 导出全部快照', exact: true })
    ).toBeVisible()
    await expect(detail.locator('.event')).toHaveCount(50)
    await detail.getByRole('button', { name: '下一页事件', exact: true }).click()
    await expect(detail.getByText('#51', { exact: true })).toBeVisible()
    await detail.getByLabel('搜索记录内容').fill('交换事件 119')
    await expect(detail.locator('.event')).toHaveCount(1)
    const original = (await page.evaluate(() => window.jeval.listRuns({ source: 'codex' })))
      .items[0]
    expect(original.eventCount).toBe(120)
    await expect(detail.getByRole('region', { name: '导出已保存快照' })).toContainText(
      '全部 120 个事件'
    )
    await expect(detail.getByRole('region', { name: '导出已保存快照' })).toContainText(sourceFile)
    await application.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async (_window, options) => {
        if (!options.defaultPath?.endsWith('.jeval.json'))
          throw new Error('native filename required')
        return { canceled: false, filePath: path }
      }
    }, jsonFile)
    await detail.getByRole('button', { name: '导出 JSON', exact: true }).click()
    await expect(detail.locator('.export-success')).toContainText('已导出 JSON · 120 个事件')
    const envelope = JSON.parse(await readFile(jsonFile, 'utf8')) as NativeRecordEnvelope
    expect(envelope.format).toBe('jeval-record')
    expect(envelope.formatVersion).toBe(1)
    expect(envelope.contentScope).toBe('normalized-preview')
    expect(envelope.maxContentBytes).toBe(8192)
    expect(envelope.sourceFilesIncluded).toBe(false)
    expect(envelope.record.runs).toHaveLength(1)
    expect(envelope.record.events).toHaveLength(120)
    expect(envelope.record.runs[0].durationMs).toBeNull()
    expect(envelope.record.runs[0].tokens).toBeNull()
    expect(envelope.record.events[2].parentId).toBe(envelope.record.events[1].id)
    expect(envelope.record.events[119].evidence.line).toBe(121)
    expect(envelope.record.events[4].content).toContain('已截断')
    expect(envelope.record.events[119].content).toBe('交换事件 119')
    expect(envelope.record.runs[0]).not.toHaveProperty('readOnly')
    await application.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async (_window, options) => {
        if (!options.defaultPath?.endsWith('.md')) throw new Error('markdown filename required')
        return { canceled: false, filePath: path }
      }
    }, markdownFile)
    await detail.getByRole('button', { name: '导出 Markdown', exact: true }).click()
    await expect(detail.locator('.export-success')).toContainText('已导出 Markdown · 120 个事件')
    const markdown = await readFile(markdownFile, 'utf8')
    expect(markdown).toContain('交换事件 119')
    expect(markdown).toContain(sourceFile)
    expect(markdown).toContain('unknown (null)')
    expect(markdown).toContain('normalized-preview')
    expect(markdown).toContain('8192')
    await application.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: true, filePath: path })
    }, cancelledFile)
    await detail.getByRole('button', { name: '导出 JSON', exact: true }).click()
    await expect(detail.getByRole('button', { name: '导出 JSON', exact: true })).toBeEnabled()
    await expect(detail.locator('.export-success')).toHaveCount(0)
    expect(await readFile(cancelledFile, 'utf8')).toBe('cancelled export must preserve these bytes')
    await application.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: path })
    }, sourceFile)
    await detail.getByRole('button', { name: '导出 JSON', exact: true }).click()
    await expect(detail.getByRole('alert')).toBeVisible()
    expect(await readFile(sourceFile, 'utf8')).toBe(sourceBytes)
    await expect(detail.locator('.export-success')).toHaveCount(0)
    await page.getByRole('button', { name: '合成演示 3', exact: true }).click()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(detail.getByRole('alert')).toHaveCount(0)
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/record-export-1440.png' })
    await application.close()
    await rm(sourceFile)
    const recipientData = resolve(directory, 'recipient-library')
    application = await launch(recipientData)
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(0)
    await application.evaluate(({ dialog }) => {
      dialog.showOpenDialog = async () => ({ canceled: true, filePaths: [] })
    })
    const importRecord = page.getByRole('button', { name: '导入 jeval 记录', exact: true })
    await importRecord.click()
    await expect(importRecord).toBeEnabled()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(0)
    await expect(page.getByText('已导入 jeval 记录', { exact: false })).toHaveCount(0)
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, invalidFile)
    await importRecord.click()
    await expect(page.getByRole('alert')).toBeVisible()
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(0)
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async (_window, options) => {
        if (
          !options.properties?.includes('openFile') ||
          options.properties.includes('multiSelections')
        )
          throw new Error('exactly one native file required')
        return { canceled: false, filePaths: [path] }
      }
    }, jsonFile)
    await importRecord.click()
    await expect(page.getByText('已导入 jeval 记录 · 120 个事件', { exact: true })).toBeVisible()
    detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.getByText('只读交换快照', { exact: true })).toBeVisible()
    await expect(detail.getByRole('button', { name: '更新已登记记录', exact: true })).toBeDisabled()
    const restored = await page.evaluate((id) => window.jeval.getRun(id), original.id)
    expect(restored.readOnly).toBe(true)
    const { readOnly, ...restoredCanonical } = restored
    expect(restoredCanonical).toEqual(envelope.record.runs[0])
    const restoredEvents = await page.evaluate(async (id) => {
      const events: RunEvent[] = []
      let offset: number | null = 0
      while (offset !== null) {
        const result = await window.jeval.listEvents({ runId: id, offset, limit: 50 })
        events.push(...result.items)
        offset = result.nextOffset
      }
      return events
    }, original.id)
    expect(restoredEvents).toEqual(envelope.record.events)
    expect(
      await page.evaluate(async (id) => {
        try {
          await window.jeval.updateCodex(id)
          return 'accepted'
        } catch (error) {
          return String(error)
        }
      }, original.id)
    ).toContain('只读交换快照')
    await detail.getByLabel('搜索记录内容').fill('交换事件 119')
    await expect(detail.locator('.event')).toHaveCount(1)
    await detail.getByRole('button', { name: '查看来源证据', exact: true }).click()
    await expect(detail.locator('.evidence-panel')).toContainText(`${sourceFile} : 121`)
    await application.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    )
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/record-import-1000.png' })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
    ).toBe(true)
    await application.close()
    application = await launch(recipientData)
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.getByText('只读交换快照', { exact: true })).toBeVisible()
    await expect(detail.getByRole('button', { name: '更新已登记记录', exact: true })).toBeDisabled()
    await expect(detail.locator('.event')).toHaveCount(50)
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(1)
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})

test('task pages navigate actual offsets when native snapshot metadata shortens pages', async () => {
  test.setTimeout(60000)
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-metadata-pages-'))
  const hash = (text: string) => createHash('sha256').update(text).digest('hex')
  const seed: NativeRecordEnvelope = {
    format: 'jeval-record',
    formatVersion: 1,
    contentScope: 'normalized-preview',
    maxContentBytes: 8192,
    sourceFilesIncluded: false,
    record: {
      schemaVersion: 1,
      runs: [
        {
          id: '',
          title: '',
          project: 'synthetic-metadata-pages',
          source: 'Codex',
          demo: false,
          status: 'unknown',
          startedAt: null,
          durationMs: null,
          tokens: null,
          eventCount: 1,
          importInfo: {
            adapterVersion: 'codex-rollout-v1',
            snapshotId: '',
            file: '',
            sha256: '',
            sessionId: '',
            cliVersion: 'synthetic',
            historyMode: 'classic',
            warningCount: 0,
            warnings: []
          }
        }
      ],
      events: [
        {
          id: '',
          runId: '',
          sequence: 1,
          kind: 'message',
          role: 'user',
          title: '合成消息',
          content: '合成记录，未读取来源文件',
          timestamp: null,
          parentId: null,
          evidence: { snapshotId: '', sourceId: '', location: '', line: 1 }
        }
      ]
    }
  }
  const files: string[] = []
  for (let index = 0; index < 6; index++) {
    const envelope = structuredClone(seed)
    const run = envelope.record.runs[0]
    const info = run.importInfo!
    info.file = resolve(directory, `original-${index}.jsonl`)
    run.id = 'codex-' + hash(info.file)
    run.title = index === 0 ? '普通交换快照' : `大元数据任务 ${index}`
    info.sha256 = hash(`synthetic-metadata-source-${index}`)
    info.snapshotId =
      'snapshot-' + hash(JSON.stringify([1, run.id, info.sha256, info.adapterVersion]))
    info.sessionId = `synthetic-metadata-${index}`
    if (index > 0) {
      info.warningCount = 30
      info.warnings = Array.from({ length: 30 }, (_, warning) => ({
        line: warning + 1,
        message: `synthetic warning ${warning}: ` + 'w'.repeat(7000)
      }))
      expect(Buffer.byteLength(JSON.stringify(run))).toBeLessThan(256 * 1024)
      expect(Buffer.byteLength(JSON.stringify(run))).toBeGreaterThan(200 * 1024)
    }
    envelope.record.events[0] = {
      ...envelope.record.events[0],
      id: `${run.id}:1`,
      runId: run.id,
      evidence: { snapshotId: info.snapshotId, sourceId: run.id, location: info.file, line: 1 }
    }
    const path = resolve(directory, `snapshot-${index}.jeval.json`)
    await writeFile(path, JSON.stringify(envelope))
    files.push(path)
  }
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const application = await electron.launch({
    ...(packaged ? { executablePath: resolve(packaged) } : {}),
    args: [
      ...(packaged ? [] : [resolve('apps/desktop')]),
      `--user-data-dir=${resolve(directory, 'library')}`
    ],
    env
  })
  try {
    const page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    for (let index = 0; index < files.length; index++) {
      await application.evaluate(({ dialog }, path) => {
        dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
      }, files[index])
      await page.getByRole('button', { name: '导入 jeval 记录', exact: true }).click()
      await expect(page.getByRole('button', { name: '导入 jeval 记录', exact: true })).toBeEnabled()
      expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(
        index + 1
      )
    }
    await page.getByLabel('搜索任务').fill('大元数据任务')
    const cards = page.locator('.run-card h3')
    await expect(cards).toHaveText(['大元数据任务 1', '大元数据任务 2'])
    const previous = page.getByRole('button', { name: '上一页任务', exact: true })
    const next = page.getByRole('button', { name: '下一页任务', exact: true })
    await expect(previous).toBeDisabled()
    await next.click()
    await expect(cards).toHaveText(['大元数据任务 3', '大元数据任务 4'])
    await page.locator('.run-card').first().click()
    const detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.getByRole('heading', { name: '大元数据任务 3', exact: true })).toBeVisible()
    await next.click()
    await expect(cards).toHaveText(['大元数据任务 5'])
    await expect(next).toBeDisabled()
    await expect(detail.getByRole('heading', { name: '大元数据任务 3', exact: true })).toBeVisible()
    await previous.click()
    await expect(cards).toHaveText(['大元数据任务 3', '大元数据任务 4'])
    await previous.click()
    await expect(cards).toHaveText(['大元数据任务 1', '大元数据任务 2'])
    await expect(previous).toBeDisabled()
    await expect(detail.getByRole('heading', { name: '大元数据任务 3', exact: true })).toBeVisible()
    await next.click()
    await expect(cards).toHaveText(['大元数据任务 3', '大元数据任务 4'])
    await page.getByRole('button', { name: '刷新记录', exact: true }).click()
    await expect(cards).toHaveText(['大元数据任务 1', '大元数据任务 2'])
    await expect(previous).toBeDisabled()
    await expect(detail.getByRole('heading', { name: '大元数据任务 3', exact: true })).toBeVisible()
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})

test('desktop loads real Go data and supports browsing, filtering, evidence, and recovery', async () => {
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const application = await electron.launch({
    ...(packaged ? { executablePath: resolve(packaged) } : {}),
    args: [
      ...(packaged ? [] : [resolve('apps/desktop')]),
      `--user-data-dir=${resolve('.local', `e2e-${Date.now()}`)}`
    ],
    env
  })
  try {
    const page = await application.firstWindow()
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await expect(page.locator('.brand-logo')).toBeVisible()
    expect(
      await page
        .locator('.brand-logo')
        .evaluate((image: HTMLImageElement) => image.complete && image.naturalWidth > 0)
    ).toBe(true)
    await expect(page.getByRole('button', { name: /acme.*修复缓存/ })).toBeVisible()
    const detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.getByRole('heading', { name: '修复缓存失效后的重复请求' })).toBeVisible()
    await expect(detail.getByRole('heading', { name: '任务开始' })).toBeVisible()
    const back = page.getByRole('button', { name: '后退', exact: true })
    const forward = page.getByRole('button', { name: '前进', exact: true })
    await expect(back).toBeDisabled()
    await expect(forward).toBeDisabled()
    await page.getByRole('button', { name: /acme.*排查 CI/ }).click()
    await expect(detail.getByRole('heading', { name: '排查 CI 中偶发的连接超时' })).toBeVisible()
    await back.click()
    await expect(detail.getByRole('heading', { name: '修复缓存失效后的重复请求' })).toBeVisible()
    await forward.click()
    await expect(detail.getByRole('heading', { name: '排查 CI 中偶发的连接超时' })).toBeVisible()
    await back.click()
    await page.getByRole('button', { name: /jeval.*为事件解析器/ }).click()
    await expect(forward).toBeDisabled()
    await back.click()
    await expect(detail.getByRole('heading', { name: '修复缓存失效后的重复请求' })).toBeVisible()
    expect(
      await page
        .locator('.window-toolbar')
        .evaluate((el) => getComputedStyle(el).getPropertyValue('-webkit-app-region'))
    ).toBe('drag')
    expect(
      await back.evaluate((el) => getComputedStyle(el).getPropertyValue('-webkit-app-region'))
    ).toBe('no-drag')
    await page.screenshot({ path: 'test-results/desktop-1440.png' })
    await detail.getByText('回归验证通过', { exact: true }).click()
    await expect(detail.getByText('Tests: 3 passed', { exact: false })).toBeVisible()
    await detail.getByRole('button', { name: '查看来源证据' }).first().click()
    await expect(detail.getByText('embedded:demo-cache : 1')).toBeVisible()
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/desktop-evidence.png' })
    await detail.getByLabel('搜索记录内容').fill('PROMISE')
    await expect(detail.getByText('匹配 2 个事件 · 保留原始序号', { exact: true })).toBeVisible()
    await detail.getByLabel('筛选事件类型').selectOption('verification')
    await expect(detail.getByText('匹配 1 个事件 · 保留原始序号', { exact: true })).toBeVisible()
    await expect(detail.locator('.event')).toHaveCount(1)
    await expect(detail.getByText('#05', { exact: true })).toBeVisible()
    await detail.getByRole('button', { name: '查看来源证据', exact: true }).click()
    await expect(detail.getByText('embedded:demo-cache : 5', { exact: true })).toBeVisible()
    await detail.getByLabel('搜索记录内容').fill('不存在的事件')
    await expect(detail.getByText('没有匹配的事件', { exact: true })).toBeVisible()
    await detail.getByRole('button', { name: '清除事件筛选', exact: true }).click()
    await expect(detail.locator('.event')).toHaveCount(6)
    await page.getByLabel('搜索任务').fill('不存在的任务')
    await expect(page.getByText('没有匹配的记录')).toBeVisible()
    await page.screenshot({ path: 'test-results/desktop-empty.png' })
    await page.getByRole('button', { name: '清除筛选' }).click()
    await page.getByLabel('筛选状态').selectOption('unknown')
    await expect(detail.getByRole('heading', { name: '为事件解析器补充边界用例' })).toBeVisible()
    await expect(detail.getByText('未知', { exact: true })).toHaveCount(2)
    await expect(detail.getByText('记录结束 · 完成状态未提供')).toBeVisible()
    await page.getByLabel('筛选状态').selectOption('failed')
    await detail.getByText('连接测试失败', { exact: true }).click()
    await expect(detail.getByText('connection refused', { exact: false })).toBeVisible()
    await application.evaluate(({ BrowserWindow }) => {
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    })
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/desktop-1000.png' })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
    ).toBe(true)
    const hello = await page.evaluate(() => window.jeval.restartEngine())
    expect(hello.protocolVersion).toBe(1)
    await page.getByRole('button', { name: '刷新记录' }).click()
    await expect(detail.getByRole('heading', { name: '排查 CI 中偶发的连接超时' })).toBeVisible()
    expect(
      await page.evaluate(() => typeof (window as unknown as { require?: unknown }).require)
    ).toBe('undefined')
    expect(errors).toEqual([])
  } finally {
    await application.close()
  }
})

test('Codex file selection supports cancel, import, replacement, warnings, and local evidence', async () => {
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const application = await electron.launch({
    ...(packaged ? { executablePath: resolve(packaged) } : {}),
    args: [
      ...(packaged ? [] : [resolve('apps/desktop')]),
      `--user-data-dir=${resolve('.local', `e2e-import-${Date.now()}`)}`
    ],
    env
  })
  try {
    const page = await application.firstWindow()
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.getByText('还没有 Codex 记录')).toBeVisible()
    await application.evaluate(({ dialog }) => {
      dialog.showOpenDialog = async () => ({ canceled: true, filePaths: [] })
    })
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    await expect(page.getByRole('button', { name: '导入 Codex 记录', exact: true })).toBeEnabled()
    await expect(page.getByText('还没有 Codex 记录')).toBeVisible()
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, resolve('fixtures/adapters/codex/missing.jsonl'))
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('无法读取所选文件')
    await expect(page.getByRole('alert')).not.toContainText('Error invoking remote method')
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('alert').getByRole('button', { name: '关闭提示' }).click()
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, resolve('fixtures/adapters/codex/classic.jsonl'))
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    const detail = page.getByRole('article', { name: '执行详情' })
    await expect(
      detail.getByRole('heading', { name: '检查示例项目的测试结果', exact: true })
    ).toBeVisible()
    await expect(page.getByText('已导入记录 · 5 个事件')).toBeVisible()
    await detail.getByText('导入信息 · 2 项提示', { exact: true }).click()
    await expect(detail.getByText('synthetic-parent-001', { exact: true })).toBeVisible()
    await expect(detail.getByText('第 9 行：', { exact: false })).toBeVisible()
    await detail.locator('.event summary').filter({ hasText: '工具结果' }).last().click()
    await expect(
      detail.locator('pre').filter({ hasText: "<script>alert('never execute')</script>" })
    ).toBeVisible()
    await detail.getByRole('button', { name: '查看来源证据' }).first().click()
    await expect(detail.getByText('来源文件引用', { exact: true })).toBeVisible()
    await expect(detail.locator('.evidence-panel')).toContainText('classic.jsonl : 3')
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/codex-import-1440.png' })
    await application.evaluate(({ BrowserWindow }) => {
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    })
    await page.screenshot({ path: 'test-results/codex-import-1000.png' })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
    ).toBe(true)
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    await expect(page.getByText('已更新记录 · 5 个事件')).toBeVisible()
    await expect(page.locator('.run-card')).toHaveCount(1)
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, resolve('fixtures/adapters/codex/paginated.jsonl'))
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    await expect(detail.getByRole('heading', { name: '验证分页记录', exact: true })).toBeVisible()
    await expect(page.getByText('已导入记录 · 9 个事件')).toBeVisible()
    await expect(page.locator('.run-card')).toHaveCount(2)
    await page.getByRole('button', { name: '后退', exact: true }).click()
    await expect(
      detail.getByRole('heading', { name: '检查示例项目的测试结果', exact: true })
    ).toBeVisible()
    await page.getByRole('button', { name: '前进', exact: true }).click()
    await expect(detail.getByRole('heading', { name: '验证分页记录', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '合成演示 3', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(3)
    await page.getByRole('button', { name: '后退', exact: true }).click()
    await expect(detail.getByRole('heading', { name: '验证分页记录', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '前进', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(3)
    await page.evaluate(() => window.jeval.restartEngine())
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(2)
    await page.locator('.run-card').filter({ hasText: '检查示例项目的测试结果' }).click()
    await expect(
      detail.getByRole('heading', { name: '检查示例项目的测试结果', exact: true })
    ).toBeVisible()
    expect(errors).toEqual([])
  } finally {
    await application.close()
  }
})

test('persistent library restores offline snapshots and directories with bounded task/event pages', async () => {
  test.setTimeout(60000)
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-library-'))
  const sources = resolve(directory, 'sources')
  const userData = resolve(directory, 'user-data')
  await mkdir(sources)
  for (let index = 0; index < 12; index++) {
    const lines = [
      JSON.stringify({
        timestamp: `2026-10-04T01:${String(index).padStart(2, '0')}:00Z`,
        type: 'session_meta',
        payload: {
          id: `synthetic-persistent-${index}`,
          cwd: 'D:\\synthetic-library',
          cli_version: 'synthetic'
        }
      })
    ]
    for (let event = 0; event < 120; event++)
      lines.push(
        JSON.stringify({
          type: 'response_item',
          payload: {
            type: 'message',
            role: event === 0 ? 'user' : 'assistant',
            content: `持久化任务 ${index} · 事件 ${event}`
          }
        })
      )
    await writeFile(resolve(sources, `session-${index}.jsonl`), lines.join('\n') + '\n')
  }
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = () =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [...(packaged ? [] : [resolve('apps/desktop')]), `--user-data-dir=${userData}`],
      env
    })
  let application = await launch()
  try {
    let page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    expect(await application.evaluate(({ app }) => app.getPath('userData'))).toBe(userData)
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, sources)
    await page.getByRole('button', { name: '发现本地任务', exact: true }).click()
    const picker = page.getByRole('dialog', { name: '选择要导入的记录' })
    await expect(picker).toBeVisible()
    await picker.getByRole('checkbox', { name: '全选记录', exact: true }).check()
    await picker.getByRole('button', { name: '导入所选（12）', exact: true }).click()
    await expect(picker.getByRole('button', { name: '关闭选择', exact: true })).toBeEnabled()
    await picker.getByRole('button', { name: '关闭选择', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(10)
    await page.getByRole('button', { name: '下一页任务', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(2)
    const title = await page.locator('.run-card h3').first().innerText()
    await page.locator('.run-card').first().click()
    let detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.getByRole('heading', { name: title, exact: true })).toBeVisible()
    await page.getByRole('button', { name: '上一页任务', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(10)
    await expect(detail.getByRole('heading', { name: title, exact: true })).toBeVisible()
    await expect(detail.locator('.event')).toHaveCount(50)
    await detail.getByRole('button', { name: '下一页事件', exact: true }).click()
    await expect(detail.locator('.event')).toHaveCount(50)
    await expect(detail.getByText('#51', { exact: true })).toBeVisible()
    await expect(detail.getByText('#01', { exact: true })).toHaveCount(0)
    await detail.getByRole('button', { name: '下一页事件', exact: true }).click()
    await expect(detail.locator('.event')).toHaveCount(20)
    await detail.getByRole('button', { name: '上一页事件', exact: true }).click()
    await expect(detail.locator('.event')).toHaveCount(50)
    await expect(detail.getByText('#51', { exact: true })).toBeVisible()
    await detail.getByLabel('搜索记录内容').fill('事件 119')
    await expect(detail.locator('.event')).toHaveCount(1)
    await expect(detail.getByText('#120', { exact: true })).toBeVisible()
    await detail.getByRole('button', { name: '查看来源证据', exact: true }).click()
    await expect(detail.locator('.evidence-panel')).toContainText(' : 121')
    await expect(detail.locator('.snapshot-completeness')).toContainText('最多 8 KiB')
    await page.locator('.directory-panel summary').click()
    await expect(
      page.getByRole('button', { name: `重新发现 ${sources}`, exact: true })
    ).toBeVisible()
    expect(
      await page.evaluate(async (path) => {
        try {
          await window.jeval.scanCodex(path)
          return 'accepted'
        } catch (error) {
          return String(error)
        }
      }, sources)
    ).toContain('无效的记录或目录 ID')
    await page.getByRole('button', { name: `重新发现 ${sources}`, exact: true }).click()
    await expect(picker).toBeVisible()
    await expect(picker.getByRole('button', { name: '导入所选（0）', exact: true })).toBeDisabled()
    await picker.getByRole('button', { name: '关闭选择', exact: true }).click()
    const selectedRun = await page.evaluate(async (selectedTitle) => {
      const result = await window.jeval.listRuns({ source: 'codex', search: selectedTitle })
      return result.items[0]
    }, title)
    const sourceFile = selectedRun.importInfo!.file
    await writeFile(
      sourceFile,
      (await readFile(sourceFile, 'utf8')) +
        JSON.stringify({
          type: 'response_item',
          payload: { type: 'message', role: 'assistant', content: '手动更新新增事件' }
        }) +
        '\n'
    )
    await application.evaluate(({ dialog }) => {
      dialog.showOpenDialog = async () => {
        throw new Error('registered update must not open a chooser')
      }
    })
    await detail.getByRole('button', { name: '更新已登记记录', exact: true }).click()
    await expect(page.getByText('已更新记录 · 121 个事件', { exact: true })).toBeVisible()
    await expect(detail.locator('.event')).toHaveCount(50)
    await page.screenshot({ path: 'test-results/persistent-library-1440.png' })
    await application.close()
    await rm(sources, { recursive: true, force: true })
    application = await launch()
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    expect((await readFile(resolve(userData, 'library.sqlite'))).length).toBeGreaterThan(0)
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(10)
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(12)
    await expect(page.getByRole('region', { name: 'Codex 目录扫描' })).toHaveCount(0)
    await page.locator('.directory-panel summary').click()
    await expect(
      page.getByRole('button', { name: `重新发现 ${sources}`, exact: true })
    ).toBeVisible()
    detail = page.getByRole('article', { name: '执行详情' })
    await expect(detail.locator('.event')).toHaveCount(50)
    const restoredTitle = await detail.locator('h2').innerText()
    await detail.getByRole('button', { name: '更新已登记记录', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('已保存的快照保留')
    await expect(detail.getByRole('heading', { name: restoredTitle, exact: true })).toBeVisible()
    await expect(detail.locator('.event')).toHaveCount(50)
    await page.getByRole('alert').getByRole('button', { name: '关闭提示' }).click()
    await page.getByRole('button', { name: `移除目录 ${sources}`, exact: true }).click()
    await expect(page.locator('.directory-panel summary')).toHaveText('保存的 Codex 目录 · 0')
    expect((await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).total).toBe(12)
    await application.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    )
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/persistent-library-1000.png' })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
    ).toBe(true)
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})
