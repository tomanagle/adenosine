import type { Repository } from '@adenosine/api-client'
import { describe, expect, it } from 'vitest'

import {
  filterRepositories,
  ownedRepositories,
  proposalRepositories,
  repositoryKey,
  repositoryParams,
  repositorySummary,
  summarySentence,
} from './viewer-repositories'

function repository(overrides: Partial<Repository> = {}): Repository {
  return {
    id: '00000000-0000-4000-8000-000000000000',
    uri: 'at://did:plc:viewer/sh.adenosine.repository/alpha',
    cid: 'bafy',
    slug: 'alpha',
    display_name: null,
    description: null,
    visibility: 'public',
    state: 'active',
    default_branch: 'main',
    owner: { did: 'did:plc:viewer', handle: 'viewer.example' },
    hosting: {
      local: true,
      web_url: 'https://example.test/viewer.example/alpha',
      git_https_url: 'https://example.test/viewer.example/alpha.git',
      git_ssh_url: null,
      source_browsing: 'local',
    },
    star_count: 0,
    issue_count: 0,
    open_issue_count: 0,
    comment_count: 0,
    pull_request_count: 0,
    open_pull_request_count: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('ownedRepositories', () => {
  const testCases = [
    {
      name: 'keeps only the signed-in identity and orders by most recent update',
      did: 'did:plc:viewer' as string | undefined,
      repositories: [
        repository({ slug: 'older', updated_at: '2026-01-01T00:00:00Z' }),
        repository({ slug: 'other', owner: { did: 'did:plc:other', handle: 'other.example' } }),
        repository({ slug: 'newer', updated_at: '2026-03-01T00:00:00Z' }),
      ],
      want: ['newer', 'older'],
    },
    {
      name: 'returns nothing without an identity',
      did: undefined,
      repositories: [repository()],
      want: [],
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(ownedRepositories(testCase.repositories, testCase.did).map((r) => r.slug)).toEqual(
        testCase.want,
      )
    })
  }
})

describe('filterRepositories', () => {
  const repositories = [
    repository({ slug: 'alpha', description: 'Ledger service' }),
    repository({ slug: 'beta', display_name: 'Beta Console' }),
  ]
  const testCases = [
    { name: 'empty query keeps everything', query: '  ', want: ['alpha', 'beta'] },
    { name: 'matches the slug', query: 'ALP', want: ['alpha'] },
    { name: 'matches the display name', query: 'console', want: ['beta'] },
    { name: 'matches the description', query: 'ledger', want: ['alpha'] },
    { name: 'no match returns nothing', query: 'gamma', want: [] },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(filterRepositories(repositories, testCase.query).map((r) => r.slug)).toEqual(
        testCase.want,
      )
    })
  }
})

describe('proposalRepositories', () => {
  const testCases = [
    {
      name: 'only active, published repositories whose Git objects are served here, by slug',
      repositories: [
        repository({ slug: 'zeta' }),
        repository({ slug: 'creating', state: 'creating' }),
        repository({ slug: 'unpublished', uri: null }),
        repository({
          slug: 'remote',
          hosting: {
            local: false,
            web_url: 'https://elsewhere.test/remote',
            git_https_url: 'https://elsewhere.test/remote.git',
            git_ssh_url: null,
            source_browsing: 'canonical_host',
          },
        }),
        repository({ slug: 'alpha' }),
      ],
      want: ['alpha', 'zeta'],
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(proposalRepositories(testCase.repositories).map((r) => r.slug)).toEqual(testCase.want)
    })
  }
})

describe('repositoryParams', () => {
  const testCases = [
    { name: 'prefers the handle', handle: 'viewer.example', want: 'viewer.example' },
    { name: 'falls back to the DID', handle: null, want: 'did:plc:viewer' },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      const params = repositoryParams(
        repository({ owner: { did: 'did:plc:viewer', handle: testCase.handle } }),
      )
      expect(params).toEqual({ owner: testCase.want, repo: 'alpha' })
    })
  }
})

describe('repositoryKey', () => {
  const testCases = [
    {
      name: 'uses the record URI',
      repository: repository(),
      want: 'at://did:plc:viewer/sh.adenosine.repository/alpha',
    },
    {
      name: 'falls back to owner and slug when the record is not published yet',
      repository: repository({ uri: null, id: null }),
      want: 'did:plc:viewer/alpha',
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(repositoryKey(testCase.repository)).toBe(testCase.want)
    })
  }
})

describe('repositorySummary', () => {
  const testCases = [
    {
      name: 'totals the open work across owned repositories',
      repositories: [
        repository({ open_issue_count: 2, open_pull_request_count: 1 }),
        repository({ open_issue_count: 3, open_pull_request_count: 4 }),
      ],
      want: { repositories: 2, openIssues: 5, openPullRequests: 5 },
    },
    {
      name: 'empty list totals zero',
      repositories: [],
      want: { repositories: 0, openIssues: 0, openPullRequests: 0 },
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(repositorySummary(testCase.repositories)).toEqual(testCase.want)
    })
  }
})

describe('summarySentence', () => {
  const testCases = [
    {
      name: 'reads singular counts as singular',
      summary: { repositories: 1, openIssues: 1, openPullRequests: 1 },
      want: '1 repository · 1 open issue · 1 open pull request',
    },
    {
      name: 'reads plural and zero counts as plural',
      summary: { repositories: 2, openIssues: 0, openPullRequests: 3 },
      want: '2 repositories · 0 open issues · 3 open pull requests',
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(summarySentence(testCase.summary)).toBe(testCase.want)
    })
  }
})
