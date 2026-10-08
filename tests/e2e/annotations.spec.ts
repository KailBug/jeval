import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'
import { mkdir, mkdtemp, writeFile, readFile, rm } from 'node:fs/promises'

test('snapshot and event notes survive replacement offline; conflicts and failures retain drafts', async () => {
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-annotations-'))
  const source = resolve(directory, 'source.jsonl'),
    profile = resolve(directory, 'profile')
  const row = (content: string) =>
    JSON.stringify({ type: 'response_item', payload: { type: 'message', role: 'user', content } }) +
    '\n'
  const prefix = '{"type":"session_meta","payload":{"id":"annotation-ui"}}\n'
  await writeFile(source, prefix + row('标注原始消息') + row('第二条消息'))
  const bytes = await readFile(source)
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = () =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [...(packaged ? [] : [resolve('apps/desktop')]), `--user-data-dir=${profile}`],
      env
    })
  let application = await launch()
  try {
    let page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, source)
    await page.getByRole('button', { name: '导入 Codex 记录', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(1)
    await page.locator('.run-card').click()
    const review = page.getByRole('region', { name: '人工标注', exact: true })
    await expect(review).toHaveAttribute('aria-busy', 'false')
    await review.getByRole('combobox', { name: '标注判断' }).selectOption('accepted')
    await review.getByRole('textbox', { name: '标注备注' }).fill('快照整体接受')
    await review.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(review.getByText('标注已保存', { exact: true })).toBeVisible()
    await page.locator('.event').first().getByRole('button', { name: '标注此事件' }).click()
    await expect(review.getByRole('heading', { name: '事件 #1 标注' })).toBeVisible()
    await expect(review).toHaveAttribute('aria-busy', 'false')
    await review.getByRole('combobox', { name: '标注判断' }).selectOption('rejected')
    await review
      .getByRole('textbox', { name: '标注备注' })
      .fill('事件需要复查 <script>window.noteExecuted=true</script>')
    await review.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(review.getByText('标注已保存', { exact: true })).toBeVisible()
    const old = await page.evaluate(async () => {
      const r = (await window.jeval.listRuns({ source: 'codex' })).items[0]
      const e = (await window.jeval.listEvents({ runId: r.id })).items[0]
      return { runId: r.id, snapshotId: r.importInfo!.snapshotId, eventId: e.id }
    })
    expect(await readFile(source)).toEqual(bytes)
    await writeFile(source, prefix + row('替换后的消息'))
    await page.getByRole('button', { name: '更新已登记记录', exact: true }).click()
    await expect(page.getByText(/已更新记录/)).toBeVisible()
    await expect(page.locator('.event .message-content')).toContainText('替换后的消息')
    await application.close()
    await rm(source)
    application = await launch()
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(1)
    await page.locator('.run-card').click()
    const currentReview = page.getByRole('region', { name: '人工标注', exact: true })
    await expect(currentReview).toContainText('此快照还没有标注')
    await page.getByRole('combobox', { name: '选择记录快照' }).selectOption(old.snapshotId)
    await expect(page.locator('.event').first().locator('.message-content')).toHaveText(
      '标注原始消息'
    )
    await expect(currentReview.getByRole('textbox', { name: '标注备注' })).toHaveValue(
      '快照整体接受'
    )
    await currentReview.getByRole('button', { name: /事件 #1 · 否定/ }).click()
    await expect(currentReview.getByRole('textbox', { name: '标注备注' })).toContainText(
      '事件需要复查'
    )
    expect(await page.evaluate(() => 'noteExecuted' in window)).toBe(false)
    await page.evaluate(async (target) => {
      await window.jeval.saveAnnotation({
        ...target,
        expectedRevision: 1,
        judgement: 'uncertain',
        note: '另一个编辑器的意见'
      })
    }, old)
    await currentReview.getByRole('textbox', { name: '标注备注' }).fill('当前草稿')
    await currentReview.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(currentReview.getByRole('alert')).toContainText('ANNOTATION_CONFLICT')
    await expect(currentReview.getByRole('textbox', { name: '标注备注' })).toHaveValue('当前草稿')
    page.once('dialog', (dialog) => dialog.accept())
    await currentReview.getByRole('button', { name: '重新读取标注' }).click()
    await expect(currentReview.getByRole('textbox', { name: '标注备注' })).toHaveValue(
      '另一个编辑器的意见'
    )
    await currentReview.getByRole('button', { name: '删除该标注' }).click()
    await currentReview.getByRole('button', { name: '确认删除标注' }).click()
    await expect(currentReview.getByText('标注已删除', { exact: true })).toBeVisible()
    await currentReview.getByRole('textbox', { name: '标注备注' }).fill('删除后重建')
    await currentReview.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(currentReview.getByText('标注已保存', { exact: true })).toBeVisible()
    for (const width of [1440, 1000]) {
      await application.evaluate(
        ({ BrowserWindow }, width) => BrowserWindow.getAllWindows()[0].setSize(width, 860),
        width
      )
      await currentReview.scrollIntoViewIfNeeded()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true
      )
      await page.screenshot({ path: `test-results/annotations-${width}.png` })
    }
    await application.evaluate(({ ipcMain }) => {
      ipcMain.removeHandler('jeval:annotations.save')
      ipcMain.handle('jeval:annotations.save', () => {
        throw new Error('Synthetic save failure')
      })
    })
    await currentReview.getByRole('textbox', { name: '标注备注' }).fill('失败后仍在编辑器里的草稿')
    await currentReview.getByRole('button', { name: '保存标注', exact: true }).click()
    await expect(currentReview.getByRole('alert')).toContainText('Synthetic save failure')
    await expect(currentReview.getByRole('textbox', { name: '标注备注' })).toHaveValue(
      '失败后仍在编辑器里的草稿'
    )
    const stored = await page.evaluate((target) => window.jeval.getAnnotation(target), old)
    expect(stored.annotation!.note).toBe('删除后重建')
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})
