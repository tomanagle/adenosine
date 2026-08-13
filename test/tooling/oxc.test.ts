import { afterEach, describe, expect, test } from 'bun:test'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const root = join(import.meta.dir, '..', '..')
const temporaryDirectories: string[] = []

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { force: true, recursive: true })
  }
})

describe('Oxc checks', () => {
  test('oxfmt rejects deliberate formatting drift', () => {
    const file = temporaryFile('format.ts', 'const value={answer:42}\n')
    const result = Bun.spawnSync([
      join(root, 'node_modules', '.bin', 'oxfmt'),
      '--config',
      join(root, '.oxfmtrc.json'),
      '--check',
      file,
    ])

    expect(result.exitCode).not.toBe(0)
  })
})

function temporaryFile(name: string, contents: string): string {
  const directory = mkdtempSync(join(tmpdir(), 'adenosine-oxc-'))
  temporaryDirectories.push(directory)
  const file = join(directory, name)
  writeFileSync(file, contents)
  return file
}
