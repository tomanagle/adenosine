import type { Repository, TreeEntry } from '@adenosine/api-client'
import { describe, expect, it } from 'vitest'

import { findReadme, hostingLabel, isProbablyBinary, safeWebUrl } from './view-models'

const repository = {
  slug: 'project',
  visibility: 'public',
  state: 'active',
  default_branch: 'main',
  owner: { did: 'did:plc:alice', handle: 'alice.example' },
  hosting: {
    local: true,
    web_url: 'https://forge.example/alice/project',
    git_https_url: 'https://forge.example/alice/project.git',
    source_browsing: 'local',
  },
  star_count: 0,
  issue_count: 0,
  open_issue_count: 0,
  comment_count: 0,
  pull_request_count: 0,
  open_pull_request_count: 0,
  created_at: '2026-08-11T00:00:00Z',
  updated_at: '2026-08-11T00:00:00Z',
} satisfies Repository

describe('repository browser view models', () => {
  it('makes local and remote hosting explicit from API metadata', () => {
    expect(hostingLabel(repository)).toBe('Hosted here')
    expect(
      hostingLabel({
        ...repository,
        hosting: {
          ...repository.hosting,
          local: false,
          source_browsing: 'canonical_host',
          web_url: 'https://code.example/r/project',
        },
      }),
    ).toBe('Hosted by code.example')
    expect(safeWebUrl('javascript:alert(1)')).toBeUndefined()
  })

  it('finds a root README without trusting entry casing or non-blob entries', () => {
    const entries: TreeEntry[] = [
      { name: 'README.md', path: 'README.md', mode: '040000', type: 'tree', sha: 'a' },
      { name: 'readme.MD', path: 'readme.MD', mode: '100644', type: 'blob', sha: 'b', size: 3 },
    ]
    expect(findReadme(entries)?.sha).toBe('b')
    expect(findReadme([])).toBeUndefined()
  })

  it('distinguishes NUL-containing binary blobs from text', () => {
    expect(isProbablyBinary(new Uint8Array([104, 105, 10]))).toBe(false)
    expect(isProbablyBinary(new Uint8Array([104, 0, 105]))).toBe(true)
  })
})
