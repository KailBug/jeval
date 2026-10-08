import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'
import { mkdir, mkdtemp, writeFile, readFile, rm } from 'node:fs/promises'
import type { ComparisonReport } from '../../contracts/index'

test('manual comparison keeps independent pages and pinned offline snapshots and exports all saved evidence', async () => {
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-comparison-')),
    profile = resolve(directory, 'profile')
  const source = resolve(directory, 'source.jsonl'),
    second = resolve(directory, 'second.jsonl'),
    output = resolve(directory, 'report.json'),
    md = resolve(directory, 'report.md')
  const row = (content: string) =>
    JSON.stringify({ type: 'response_item', payload: { type: 'message', role: 'user', content } }) +
    '\n'
  const leftText =
    '{"type":"session_meta","payload":{"id":"left-ui"}}\n' +
    Array.from({ length: 65 }, (_, i) => row(`左侧消息 ${i + 1}`)).join('')
  await writeFile(source, leftText)
  await writeFile(
    second,
    '{"type":"session_meta","payload":{"id":"right-ui"}}\n' + row('右侧独立消息')
  )
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = () =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [...(packaged ? [] : [resolve('apps/desktop')]), `--user-data-dir=${profile}`],
      env
    })
  let app = await launch()
  try {
    let page = await app.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    for (const path of [source, second]) {
      await app.evaluate(({ dialog }, path) => {
        dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
      }, path)
      await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
      await expect(page.getByText(/已导入记录/)).toBeVisible()
      await expect(page.locator('.run-card')).toHaveCount(path === source ? 1 : 2)
    }
    const refs = await page.evaluate(async () => {
      const runs = (await window.jeval.listRuns({ source: 'codex' })).items
      return runs.map((r) => ({
        runId: r.id,
        snapshotId: r.importInfo!.snapshotId,
        title: r.title
      }))
    })
    const leftRef = refs.find((r) => r.title === '左侧消息 1')!,
      rightRef = refs.find((r) => r.title === '右侧独立消息')!
    await page.evaluate(
      async (ref) => {
        await window.jeval.saveAnnotation({
          ...ref,
          eventId: null,
          expectedRevision: 0,
          judgement: 'accepted',
          note: '对比用快照标注'
        })
      },
      { runId: leftRef.runId, snapshotId: leftRef.snapshotId }
    )
    await writeFile(source, leftText + row('新快照新增消息'))
    await page.evaluate(async (id) => {
      const task = await window.jeval.updateCodex(id)
      for (;;) {
        const p = await window.jeval.updateStatus(task.id)
        if (p.state === 'completed') return
        if (p.state === 'failed') throw new Error(p.message)
        await new Promise((r) => setTimeout(r, 20))
      }
    }, leftRef.runId)
    await app.close()
    await rm(source)
    await rm(second)
    app = await launch()
    page = await app.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: '手动对比', exact: true }).click()
    const left = page.getByRole('region', { name: '左侧记录', exact: true }),
      right = page.getByRole('region', { name: '右侧记录', exact: true })
    await left
      .getByRole('region', { name: '选择左侧记录', exact: true })
      .getByRole('button', { name: /左侧消息 1/ })
      .click()
    await left.getByRole('combobox', { name: '选择记录快照' }).selectOption(leftRef.snapshotId)
    await right
      .getByRole('region', { name: '选择右侧记录', exact: true })
      .getByRole('button', { name: /右侧独立消息/ })
      .click()
    await expect(left.locator('.event')).toHaveCount(50)
    await expect(right.locator('.event')).toHaveCount(1)
    const metrics = page.getByRole('table', { name: '指标比较' })
    await expect(
      metrics.getByRole('row').filter({ hasText: '执行耗时' }).getByRole('cell')
    ).toHaveText(['未知', '未知', '未知'])
    await expect(
      metrics.getByRole('row').filter({ hasText: '事件数量' }).getByRole('cell')
    ).toHaveText(['65', '1', '-64'])
    await left.getByRole('button', { name: '下一页事件', exact: true }).click()
    await expect(left.locator('.event')).toHaveCount(15)
    await expect(right.locator('.event')).toHaveCount(1)
    await expect(right.locator('.event .message-content')).toHaveText('右侧独立消息')
    await left.getByRole('textbox', { name: '搜索记录内容' }).fill('左侧消息 65')
    await expect(left.locator('.event')).toHaveCount(1)
    await expect(left.locator('.event .message-content')).toHaveText('左侧消息 65')
    await left.getByRole('combobox', { name: '筛选事件类型' }).selectOption('error')
    await expect(left.locator('.event')).toHaveCount(0)
    await expect(right.locator('.event')).toHaveCount(1)
    await left.getByRole('combobox', { name: '筛选事件类型' }).selectOption('all')
    await expect(left.locator('.event')).toHaveCount(1)
    await right.getByRole('textbox', { name: '搜索记录内容' }).fill('没有匹配')
    await expect(right.locator('.event')).toHaveCount(0)
    await expect(left.locator('.event')).toHaveCount(1)
    await right.getByRole('textbox', { name: '搜索记录内容' }).fill('')
    await expect(right.locator('.event')).toHaveCount(1)
    await left.locator('.event').getByRole('button', { name: '标注此事件' }).click()
    const notes = left.getByRole('region', { name: '人工标注' })
    await expect(notes).toHaveAttribute('aria-busy', 'false')
    await notes.getByRole('textbox', { name: '标注备注' }).fill('最后一条事件的意见')
    await notes.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(notes.getByText('标注已保存', { exact: true })).toBeVisible()
    await notes.getByRole('textbox', { name: '标注备注' }).fill('只在编辑器中的未保存草稿')
    page.once('dialog', (dialog) => dialog.dismiss())
    await left.getByRole('button', { name: '查看当前快照', exact: true }).click()
    await expect(left.getByRole('combobox', { name: '选择记录快照' })).toHaveValue(
      leftRef.snapshotId
    )
    await expect(notes.getByRole('textbox', { name: '标注备注' })).toHaveValue(
      '只在编辑器中的未保存草稿'
    )
    const rightScroll = await right.locator('.run-detail').evaluate((el) => el.scrollTop)
    await left.locator('.run-detail').evaluate((el) => {
      el.scrollTop = el.scrollHeight
    })
    expect(await right.locator('.run-detail').evaluate((el) => el.scrollTop)).toBe(rightScroll)
    for (const width of [1440, 1000]) {
      await app.evaluate(
        ({ BrowserWindow }, width) => BrowserWindow.getAllWindows()[0].setSize(width, 940),
        width
      )
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true
      )
      await page.screenshot({ path: `test-results/comparison-${width}.png` })
    }
    await app.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: path })
    }, output)
    await page.getByRole('button', { name: '导出比较 JSON' }).click()
    await expect(page.getByText('已导出比较报告 · 65 / 1 个事件', { exact: true })).toBeVisible()
    const report: ComparisonReport = JSON.parse(await readFile(output, 'utf8'))
    expect(report.left.record.events).toHaveLength(65)
    expect(report.left.record.runs[0].importInfo!.snapshotId).toBe(leftRef.snapshotId)
    expect(report.left.annotations).toHaveLength(2)
    expect(report.left.annotations.some((a) => a.note === '最后一条事件的意见')).toBe(true)
    expect(JSON.stringify(report)).not.toContain('只在编辑器中的未保存草稿')
    expect(report.right.record.events).toHaveLength(1)
    await app.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: path })
    }, md)
    await page.getByRole('button', { name: '导出比较 Markdown' }).click()
    await expect(page.getByText('已导出比较报告 · 65 / 1 个事件', { exact: true })).toBeVisible()
    expect(await readFile(md, 'utf8')).toContain('最后一条事件的意见')
    await app.evaluate(({ dialog }) => {
      dialog.showSaveDialog = async () => ({ canceled: true, filePath: undefined })
    })
    await page.getByRole('button', { name: '导出比较 JSON' }).click()
    await expect(page.getByText('已取消比较报告导出', { exact: true })).toBeVisible()
    await app.evaluate(
      ({ dialog }, path) => {
        dialog.showSaveDialog = async () => ({ canceled: false, filePath: path })
      },
      resolve(profile, 'library.sqlite')
    )
    await page.getByRole('button', { name: '导出比较 JSON' }).click()
    await expect(page.getByRole('alert')).toContainText('EXPORT_FAILED')
    await expect(left.locator('.event .message-content')).toHaveText('左侧消息 65')
    page.once('dialog', (dialog) => dialog.dismiss())
    await page.getByRole('button', { name: '返回任务库' }).click()
    await expect(page.getByRole('region', { name: '手动并排对比', exact: true })).toBeVisible()
    page.once('dialog', (dialog) => dialog.accept())
    await page.getByRole('button', { name: '返回任务库' }).click()
    await expect(page.getByRole('heading', { name: '任务库', exact: true, level: 1 })).toBeVisible()
  } finally {
    await app.close()
    await rm(directory, { recursive: true, force: true })
  }
})
