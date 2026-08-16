import { describe, expect, test } from 'bun:test'
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { z } from 'zod'

const root = join(import.meta.dir, '..', '..')
const markdownFiles = filesBelow(root).filter((file) => file.endsWith('.md'))
const currentDocs = markdownFiles.filter((file) => {
  const path = relative(root, file)
  return path !== 'plan.md' && !path.startsWith('plans/') && path !== 'AGENTS.md'
})

describe('documentation freshness', () => {
  test('relative Markdown links resolve locally', () => {
    const broken: string[] = []

    for (const file of markdownFiles) {
      const contents = readFileSync(file, 'utf8')
      for (const match of contents.matchAll(/(?<!!)\[[^\]]*\]\(([^)]+)\)/g)) {
        const destination = match[1].trim().replace(/^<|>$/g, '')
        if (
          destination === '' ||
          destination.startsWith('#') ||
          destination.startsWith('/') ||
          /^[a-z][a-z0-9+.-]*:/i.test(destination)
        ) {
          continue
        }

        const path = decodeURIComponent(destination.split('#', 1)[0])
        if (!existsSync(resolve(dirname(file), path))) {
          broken.push(`${relative(root, file)} -> ${destination}`)
        }
      }
    }

    expect(broken).toEqual([])
  })

  test('documented REST routes exist in the installed OpenAPI contract', () => {
    const contract = z
      .object({ paths: z.record(z.string(), z.json()) })
      .parse(JSON.parse(readFileSync(join(root, 'api', 'openapi.yaml'), 'utf8')))
    const routes = new Set(Object.keys(contract.paths))
    const stale: string[] = []

    for (const file of currentDocs) {
      const contents = readFileSync(file, 'utf8')
      for (const match of contents.matchAll(/\/api\/v1(?:\/[A-Za-z0-9_.{}*-]+)+/g)) {
        if (!routes.has(match[0])) {
          stale.push(`${relative(root, file)} -> ${match[0]}`)
        }
      }
    }

    expect(stale).toEqual([])
  })

  test('documented make commands are current targets', () => {
    const makefile = readFileSync(join(root, 'Makefile'), 'utf8')
    const targets = new Set(
      [...makefile.matchAll(/^([a-z][a-z0-9-]*):/gm)].map((match) => match[1]),
    )
    const stale: string[] = []

    for (const file of currentDocs) {
      const contents = readFileSync(file, 'utf8')
      for (const match of contents.matchAll(/(?:`|^)make ([a-z][a-z0-9-]*)\b/gm)) {
        if (!targets.has(match[1])) {
          stale.push(`${relative(root, file)} -> make ${match[1]}`)
        }
      }
    }

    expect(stale).toEqual([])
  })

  test('stale development and root-route claims stay removed', () => {
    const claims = markdownFiles.flatMap((file) => {
      const contents = readFileSync(file, 'utf8')
      const stale = [
        /without host-installed (?:Bun|Go|Node)/i,
        /no host-installed (?:Go|Node|runtime)/i,
        /func must\[T any\]/,
        /[|├]── home\.tsx/,
        /\| `\/home` \|/,
      ]
      return stale
        .filter((pattern) => pattern.test(contents))
        .map((pattern) => `${relative(root, file)} -> ${pattern.source}`)
    })

    expect(claims).toEqual([])
  })
})

function filesBelow(directory: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(directory)) {
    if (entry === '.git' || entry === 'node_modules' || entry === '.air') {
      continue
    }
    const path = join(directory, entry)
    if (statSync(path).isDirectory()) {
      files.push(...filesBelow(path))
    } else {
      files.push(path)
    }
  }
  return files
}
