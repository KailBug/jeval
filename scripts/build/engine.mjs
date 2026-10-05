import { mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
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
if (result.status !== 0) process.exit(result.status ?? 1)

// Carry the locked driver's and linked modules' notices with the binary.
const modules = spawnSync(
  'go',
  [
    'list',
    '-deps',
    '-f',
    '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}',
    './cmd/jeval-engine'
  ],
  {
    cwd: resolve(root, 'engine'),
    encoding: 'utf8',
    env: { ...process.env, CGO_ENABLED: '0' }
  }
)
if (modules.status !== 0) {
  console.error(modules.error?.message ?? modules.stderr)
  process.exit(1)
}
const notices = ['Third-party notices for the jeval Go engine\n']
for (const entry of [...new Set(modules.stdout.trim().split(/\r?\n/))].sort()) {
  const [name, version, directory] = entry.split('|')
  if (!version) continue
  const files = readdirSync(directory)
    .filter((file) => /^(LICENSE|COPYING|NOTICE)([.-].*)?$/i.test(file))
    .sort()
  if (files.length === 0) throw new Error(`Missing license notice for ${name}@${version}`)
  for (const file of files)
    notices.push(
      `\n--- ${name}@${version}: ${file} ---\n${readFileSync(resolve(directory, file), 'utf8')}`
    )
}
writeFileSync(resolve(root, 'bin', 'THIRD-PARTY-NOTICES.txt'), notices.join('\n'))
