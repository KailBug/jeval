import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const result = spawnSync('go', ['test', './...'], {
  cwd: fileURLToPath(new URL('../../engine', import.meta.url)),
  stdio: 'inherit'
})
if (result.error) console.error(result.error.message)
process.exit(result.status ?? 1)
