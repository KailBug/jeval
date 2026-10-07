import { writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { auditSources } from './codex-source-audit.ts'
import { auditDesktop } from './codex-desktop-audit.ts'

const [mode, manifest, report] = process.argv.slice(2)
if (!['source', 'desktop'].includes(mode) || !manifest || !report) {
  console.error(
    'Usage: node --import tsx scripts/validation/run-codex-audit.ts source|desktop MANIFEST REPORT'
  )
  process.exitCode = 1
} else {
  const work =
    mode === 'source'
      ? auditSources(
          resolve(manifest),
          resolve(
            process.env.JEVAL_AUDIT_EXECUTABLE ??
              `bin/jeval-engine${process.platform === 'win32' ? '.exe' : ''}`
          )
        )
      : auditDesktop(resolve(manifest))
  work
    .then((result) => writeFile(report, JSON.stringify(result, null, 2) + '\n'))
    .then(() =>
      console.log('Source audit passed; only aggregate evidence saved, no screenshots or traces.')
    )
    .catch(() => {
      // Assertions may contain actual/expected transcripts. Do not print them.
      console.error('Source audit failed; no transcript details logged.')
      process.exitCode = 1
    })
}
