import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'
import { mkdir, mkdtemp, writeFile, rm } from 'node:fs/promises'

test('full text pages are readable offline, bounded, escaped and explicit about missing exchange content', async () => {
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-content-'))
  const source = resolve(directory, 'source.jsonl')
  const bundle = resolve(directory, 'preview.json')
  const row = (type: string, payload: object) => JSON.stringify({ type, payload }) + '\n'
  const body = '中🙂'.repeat(11000) + '<script>window.bodyExecuted=true</script>正文末尾凭据'
  await writeFile(
    source,
    row('session_meta', { id: 'content-ui' }) +
      row('response_item', { type: 'message', role: 'user', content: '完整正文验收' }) +
      row('response_item', { type: 'message', role: 'assistant', content: body }) +
      row('response_item', { type: 'message', role: 'assistant', content: '' })
  )
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = (profile = 'profile') =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [
        ...(packaged ? [] : [resolve('apps/desktop')]),
        `--user-data-dir=${resolve(directory, profile)}`
      ],
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
    await page.locator('.run-card').click()
    await expect(page.locator('.event')).toHaveCount(3)
    await application.close()
    await rm(source)
    application = await launch()
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await page.locator('.run-card').click()
    const longEvent = page.locator('.event').nth(1)
    await expect(longEvent.locator('.message-content')).not.toContainText('正文末尾凭据')
    await longEvent.getByRole('button', { name: '查看完整正文', exact: true }).click()
    const panel = page.getByRole('region', { name: '完整正文', exact: true })
    await expect(panel.locator('.full-content-text')).toBeVisible()
    let joined = ''
    for (;;) {
      await expect(panel).toHaveAttribute('aria-busy', 'false')
      const text = (await panel.locator('.full-content-text').textContent())!
      expect(Buffer.byteLength(text)).toBeLessThanOrEqual(32768)
      joined += text
      const next = panel.getByRole('button', { name: '下一页正文' })
      if (await next.isDisabled()) break
      await next.click()
    }
    expect(joined).toBe(body)
    expect(await page.evaluate(() => 'bodyExecuted' in window)).toBe(false)
    await page.screenshot({ path: 'test-results/full-content-1440.png' })
    await application.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    )
    await panel.scrollIntoViewIfNeeded()
    await page.screenshot({ path: 'test-results/full-content-1000.png' })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await panel.getByRole('button', { name: '上一页正文' }).click()
    await expect(panel.locator('.full-content-text')).not.toContainText('正文末尾凭据')
    await page
      .locator('.event')
      .nth(2)
      .getByRole('button', { name: '查看完整正文', exact: true })
      .click()
    await expect(panel.getByText('正文为空', { exact: true })).toBeVisible()
    // Fault injection only for the error/retry state; other checks use the real engine.
    const firstPage = await page.evaluate(async () => {
      const run = (await window.jeval.listRuns({ source: 'codex' })).items[0]
      const event = (await window.jeval.listEvents({ runId: run.id })).items[1]
      return window.jeval.getEventContent({
        runId: run.id,
        snapshotId: run.importInfo!.snapshotId,
        eventId: event.id
      })
    })
    await application.evaluate(({ ipcMain }, result) => {
      let fail = true
      ipcMain.removeHandler('jeval:event-content')
      ipcMain.handle('jeval:event-content', () => {
        if (fail) {
          fail = false
          throw new Error('Synthetic body read failure')
        }
        return result
      })
    }, firstPage)
    await longEvent.getByRole('button', { name: '查看完整正文', exact: true }).click()
    await expect(panel.getByRole('alert')).toContainText('Synthetic body read failure')
    await panel.getByRole('button', { name: '重试正文' }).click()
    await expect(panel.locator('.full-content-text')).toHaveText(firstPage.content)
    await application.evaluate(({ dialog }, path) => {
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: path })
    }, bundle)
    await page.getByRole('button', { name: '导出 JSON', exact: true }).click()
    await expect(page.getByText(/已导出 JSON/)).toBeVisible()
    await application.close()
    application = await launch('detached')
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await application.evaluate(({ dialog }, path) => {
      dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [path] })
    }, bundle)
    await page.getByRole('button', { name: '导入 jeval 记录', exact: true }).click()
    await page.locator('.run-card').click()
    await page
      .locator('.event')
      .nth(1)
      .getByRole('button', { name: '查看完整正文', exact: true })
      .click()
    await expect(page.getByRole('region', { name: '完整正文' })).toContainText(
      '此快照未保存完整正文'
    )
    await expect(page.getByRole('button', { name: '更新已登记记录', exact: true })).toBeDisabled()
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})
