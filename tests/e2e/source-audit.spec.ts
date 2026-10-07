import { test, expect } from '@playwright/test'
import { resolve } from 'node:path'
import { auditDesktop } from '../../scripts/validation/codex-desktop-audit'

test('public real-derived fork metadata imports, updates and reopens offline in the desktop', async () => {
  const report = await auditDesktop(resolve('fixtures/adapters/codex/public/manifest.json'))
  expect(report.offlineRestart).toBe(true)
  expect(report.results.map((row) => row.events)).toEqual([0, 0])
})
