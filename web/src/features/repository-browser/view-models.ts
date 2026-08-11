import type { Repository, TreeEntry } from '@adenosine/api-client'

const README_NAMES = ['readme.md', 'readme.markdown', 'readme.txt', 'readme']

export function findReadme(entries: TreeEntry[]) {
  return entries.find(
    (entry) => entry.type === 'blob' && README_NAMES.includes(entry.name.toLowerCase()),
  )
}

export function hostingLabel(repository: Repository) {
  if (repository.hosting.local) return 'Hosted here'
  try {
    return `Hosted by ${new URL(repository.hosting.web_url).host}`
  } catch {
    return 'Hosted remotely'
  }
}

export function safeWebUrl(value: string): string | undefined {
  try {
    const url = new URL(value)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined
  } catch {
    return undefined
  }
}

export function isProbablyBinary(bytes: Uint8Array) {
  const length = Math.min(bytes.length, 8000)
  for (let index = 0; index < length; index += 1) {
    if (bytes[index] === 0) return true
  }
  return false
}

export async function blobBytes(value: Blob | File): Promise<Uint8Array> {
  return new Uint8Array(await value.arrayBuffer())
}

export function formatBytes(size: number) {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KiB`
  return `${(size / 1024 / 1024).toFixed(1)} MiB`
}

export function shortSha(sha: string) {
  return sha.slice(0, 10)
}
