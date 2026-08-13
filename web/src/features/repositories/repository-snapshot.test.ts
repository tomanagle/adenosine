import { describe, expect, it } from 'vitest'

import type { Repository } from '@adenosine/api-client'

import { retainCreatedRepository, type RepositorySnapshot } from './repository-snapshot.query'

const created: Repository = {
  id: '00000000-0000-4000-8000-000000000000',
  uri: 'at://did:plc:viewer/dev.adenosine.repo/ledger',
  cid: 'bafy',
  slug: 'ledger',
  display_name: 'Ledger',
  description: null,
  visibility: 'public',
  state: 'active',
  default_branch: 'main',
  owner: { did: 'did:plc:viewer', handle: 'viewer.example' },
  hosting: {
    local: true,
    web_url: 'https://example.test/viewer.example/ledger',
    git_https_url: 'https://example.test/viewer.example/ledger.git',
    git_ssh_url: null,
    source_browsing: 'local',
  },
  star_count: 0,
  issue_count: 0,
  open_issue_count: 0,
  comment_count: 0,
  pull_request_count: 0,
  open_pull_request_count: 0,
  created_at: '2026-08-12T00:00:00Z',
  updated_at: '2026-08-12T00:00:00Z',
}

describe('repository snapshot fallback', () => {
  it('can represent an unavailable REST snapshot without losing the home route', () => {
    const snapshot: RepositorySnapshot = { repositories: [], available: false }
    expect(snapshot.available).toBe(false)
    expect(snapshot.repositories).toEqual([])
  })

  it('retains a newly created public repository while projection catches up', () => {
    const snapshot: RepositorySnapshot = { repositories: [], available: true }

    expect(retainCreatedRepository(snapshot, created)?.repositories).toEqual([created])
  })

  it('does not duplicate a repository already returned by the projection', () => {
    const snapshot: RepositorySnapshot = { repositories: [created], available: true }

    expect(retainCreatedRepository(snapshot, created)).toBe(snapshot)
  })

  it('keeps private repositories out of the public snapshot', () => {
    const snapshot: RepositorySnapshot = { repositories: [], available: true }

    expect(retainCreatedRepository(snapshot, { ...created, visibility: 'private' })).toBe(snapshot)
  })
})
