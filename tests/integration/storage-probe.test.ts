import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, readdir, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { test } from 'node:test'
import { promisify } from 'node:util'

const run = promisify(execFile)
const executable = process.env.JEVAL_STORAGE_EXECUTABLE
  ? resolve(process.env.JEVAL_STORAGE_EXECUTABLE)
  : resolve('bin', process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine')

test('distributed engine creates, migrates and reopens SQLite without source files', async () => {
  const directory = await mkdtemp(resolve(tmpdir(), 'jeval-storage-中文 '))
  try {
    const result = await run(executable, ['--storage-check', directory], {
      timeout: 30000,
      // An empty PATH verifies this mode does not shell out to sqlite/go/node.
      env: { ...process.env, PATH: '', Path: '' }
    })
    assert.deepEqual(JSON.parse(result.stdout), {
      storageCheck: 'ok',
      schemaVersion: 6,
      scope: 'synthetic-preview-only'
    })
    assert.equal(result.stderr, '')
    assert.deepEqual(await readdir(directory), [])
    await assert.rejects(run(executable, ['--storage-check', 'relative-path']))
  } finally {
    await rm(directory, { recursive: true, force: true })
  }
})
