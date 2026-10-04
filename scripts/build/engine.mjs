import { mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const root = fileURLToPath(new URL('../../', import.meta.url))
mkdirSync(resolve(root, 'bin'), { recursive: true })
const executable = process.platform === 'win32' ? 'jeval-engine.exe' : 'jeval-engine'
const result = spawnSync(
  'go',
  ['build', '-trimpath', '-o', resolve(root, 'bin', executable), './cmd/jeval-engine'],
  {
    cwd: resolve(root, 'engine'),
    stdio: 'inherit',
    env: { ...process.env, CGO_ENABLED: '0' }
  }
)
if (result.error) console.error(result.error.message)
process.exit(result.status ?? 1)
