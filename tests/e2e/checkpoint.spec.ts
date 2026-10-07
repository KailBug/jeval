import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'
import { mkdir, mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'

test('registered update survives app restart, completes partial lines and preserves offline evidence', async () => {
  await mkdir(resolve('.local'), { recursive: true })
  const directory = await mkdtemp(resolve('.local', 'e2e-checkpoint-'))
  const source = resolve(directory, 'source.jsonl')
  const row = (type: string, payload: object) => JSON.stringify({ type, payload }) + '\n'
  let contents =
    row('session_meta', { id: 'checkpoint-ui' }) +
    row('response_item', { type: 'message', role: 'user', content: '检查点桌面验证' })
  await writeFile(source, contents)
  const env = { ...process.env }
  delete env.ELECTRON_RUN_AS_NODE
  const packaged = process.env.JEVAL_PACKAGED_EXECUTABLE
  const launch = () =>
    electron.launch({
      ...(packaged ? { executablePath: resolve(packaged) } : {}),
      args: [
        ...(packaged ? [] : [resolve('apps/desktop')]),
        `--user-data-dir=${resolve(directory, 'profile')}`
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
    await expect(page.locator('.run-card')).toHaveCount(1)
    await page.locator('.run-card').click()
    const answer = row('response_item', {
      type: 'message',
      role: 'assistant',
      content: '追加内容已恢复'
    })
    contents += answer
    await writeFile(source, contents)
    await page.getByRole('button', { name: '更新已登记记录', exact: true }).click()
    await expect(page.getByText('已更新记录 · 2 个事件', { exact: true })).toBeVisible()
    await expect(page.locator('.event').filter({ hasText: '追加内容已恢复' })).toBeVisible()
    await application.close()
    application = await launch()
    page = await application.firstWindow()
    await expect(page.getByText('本地引擎已连接')).toBeVisible()
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(1)
    await page.locator('.run-card').click()
    await application.evaluate(({ dialog }) => {
      dialog.showOpenDialog = async () => {
        throw new Error('update must not ask for a file')
      }
    })
    const update = page.getByRole('button', { name: '更新已登记记录', exact: true })
    await update.click()
    await expect(page.getByText('来源未变化 · 2 个事件', { exact: true })).toBeVisible()
    const final = row('response_item', {
      type: 'message',
      role: 'assistant',
      content: '半行补全后可见'
    })
    await writeFile(source, contents + final.slice(0, 15))
    await update.click()
    await expect(page.getByText('已更新记录 · 2 个事件', { exact: true })).toBeVisible()
    contents += final
    await writeFile(source, contents)
    await update.click()
    await expect(page.getByText('已更新记录 · 3 个事件', { exact: true })).toBeVisible()
    await expect(page.locator('.event').filter({ hasText: '半行补全后可见' })).toBeVisible()
    expect(await readFile(source, 'utf8')).toBe(contents)
    // Exercise cancellation through the real preload/main/engine path while
    // bounded synthetic input is being read/decoded, without a parser test hook.
    await writeFile(
      source,
      contents + row('turn_context', { padding: 'x'.repeat(260) }).repeat(45000)
    )
    const cancelled = await page.evaluate(async () => {
      const run = (await window.jeval.listRuns({ source: 'codex' })).items[0]
      const job = await window.jeval.updateCodex(run.id)
      return window.jeval.cancelUpdate(job.id)
    })
    expect(cancelled.state).toBe('cancelling')
    await expect
      .poll(() =>
        page.evaluate((id) => window.jeval.updateStatus(id).then((s) => s.state), cancelled.id)
      )
      .toBe('cancelled')
    expect(
      (await page.evaluate(() => window.jeval.listRuns({ source: 'codex' }))).items[0].eventCount
    ).toBe(3)
    await rm(source)
    await update.click()
    await expect(page.getByRole('alert').filter({ hasText: '已保存的快照保留' })).toBeVisible()
    await expect(page.locator('.event')).toHaveCount(3)
    await page.screenshot({ path: 'test-results/checkpoint-update-1440.png' })
    await application.evaluate(({ BrowserWindow }) =>
      BrowserWindow.getAllWindows()[0].setSize(1000, 760)
    )
    await page.screenshot({ path: 'test-results/checkpoint-update-1000.png' })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  } finally {
    await application.close()
    await rm(directory, { recursive: true, force: true })
  }
})
