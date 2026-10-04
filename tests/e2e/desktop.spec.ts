import { test, expect, _electron as electron } from '@playwright/test'
import { resolve } from 'node:path'

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
    await page.screenshot({ path: 'test-results/desktop-1440.png' })
    await detail.getByText('回归验证通过', { exact: true }).click()
    await expect(detail.getByText('Tests: 3 passed', { exact: false })).toBeVisible()
    await detail.getByRole('button', { name: '查看来源证据' }).first().click()
    await expect(detail.getByText('embedded:demo-cache : 1')).toBeVisible()
    await detail.evaluate((element) => {
      element.scrollTop = 0
    })
    await page.screenshot({ path: 'test-results/desktop-evidence.png' })
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
    await expect(page.getByRole('alert')).toContainText('IMPORT_FAILED')
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
    await detail.getByText('工具结果', { exact: true }).last().click()
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
    await page.getByRole('button', { name: '合成演示 3', exact: true }).click()
    await expect(page.locator('.run-card')).toHaveCount(3)
    await page.evaluate(() => window.jeval.restartEngine())
    await page.getByRole('button', { name: 'Codex 本地', exact: true }).click()
    await expect(page.getByText('还没有 Codex 记录')).toBeVisible()
    expect(errors).toEqual([])
  } finally {
    await application.close()
  }
})
