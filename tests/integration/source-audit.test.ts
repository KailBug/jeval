import assert from 'node:assert/strict'
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises'
import { resolve } from 'node:path'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { auditSources } from '../../scripts/validation/codex-source-audit.ts'

const executable = resolve(
  process.env.JEVAL_AUDIT_EXECUTABLE ??
    `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
)

test('pinned real-derived public fork corpus preserves lineage and honest missing-content boundaries', async () => {
  const report = await auditSources(
    resolve('fixtures/adapters/codex/public/manifest.json'),
    executable
  )
  assert.equal(report.results.length, 2)
  assert.deepEqual(
    report.results.map((row) => row.hasFork),
    [false, true]
  )
  assert.ok(
    report.results.every(
      (row) => row.cliVersion === 'unknown' && row.sourceHistoryMode === 'unknown'
    )
  )
  assert.ok(report.results.every((row) => row.eventCount === 0 && row.warningCount === 1))
  // Reports expose neither the transcript's identifiers nor filesystem paths.
  const json = JSON.stringify(report)
  assert.ok(!json.includes('parent-session') && !json.includes('child-session'))
  assert.ok(!json.includes('source-audit-') && !json.includes('C:') && !json.includes('D:'))
})

test('source audit rejects changed bytes and local rows on or after the explicit cutoff', async () => {
  const root = resolve('.local')
  await mkdir(root, { recursive: true })
  const directory = await mkdtemp(resolve(root, 'audit-qualification-'))
  try {
    const bytes = await readFile(resolve('fixtures/adapters/codex/classic.jsonl'))
    await writeFile(resolve(directory, 'source.jsonl'), bytes)
    const manifest = resolve(directory, 'manifest.json')
    const sample = {
      id: 'T01',
      path: 'source.jsonl',
      provenance: 'local-historical',
      sha256: '0'.repeat(64),
      notAfter: '2026-07-07T00:00:00+08:00'
    }
    await writeFile(manifest, JSON.stringify({ samples: [sample] }))
    await assert.rejects(
      auditSources(manifest, executable),
      /^Error: Source audit failed: T01\/qualification;/
    )
    // A valid-shaped, recent source must be rejected before engine startup.
    const recent = Buffer.from(
      JSON.stringify({
        type: 'session_meta',
        timestamp: sample.notAfter,
        payload: { id: 'sensitive-session', timestamp: sample.notAfter }
      }) + '\n'
    )
    sample.sha256 = createHash('sha256').update(recent).digest('hex')
    await writeFile(resolve(directory, 'source.jsonl'), recent)
    await writeFile(manifest, JSON.stringify({ samples: [sample] }))
    await assert.rejects(
      auditSources(manifest, 'missing-engine'),
      /^Error: Source audit failed: T01\/qualification;/
    )
    assert.deepEqual(await readFile(resolve(directory, 'source.jsonl')), recent)
  } finally {
    assert.ok(directory.startsWith(root + (process.platform === 'win32' ? '\\' : '/')))
    await rm(directory, { recursive: true, force: true })
  }
})
