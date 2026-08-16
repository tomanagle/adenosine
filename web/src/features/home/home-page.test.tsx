// @vitest-environment jsdom

import type { Repository } from '@adenosine/api-client'
import { cleanup, fireEvent, screen } from '@testing-library/react'
import { Suspense } from 'react'
import { afterEach, describe, expect, it } from 'vitest'

import { HomePage } from './home-page'
import { identityQueryOptions } from '@/features/identity/identity.query'
import { repositorySnapshotQueryOptions } from '@/features/repositories/repository-snapshot.query'
import { renderWithAppProviders } from '@/test/render'

function repository(overrides: Partial<Repository> = {}): Repository {
  return {
    id: '00000000-0000-4000-8000-000000000000',
    uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger',
    cid: 'bafy',
    slug: 'ledger',
    display_name: null,
    description: 'Double-entry accounts',
    visibility: 'public',
    state: 'active',
    default_branch: 'main',
    archived: false,
    owner: { did: 'did:plc:viewer', handle: 'viewer.example' },
    hosting: {
      local: true,
      web_url: 'https://example.test/viewer.example/ledger',
      git_https_url: 'https://example.test/viewer.example/ledger.git',
      git_ssh_url: null,
      source_browsing: 'local',
    },
    star_count: 4,
    issue_count: 3,
    open_issue_count: 2,
    comment_count: 0,
    pull_request_count: 1,
    open_pull_request_count: 1,
    fork_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-10T00:00:00Z',
    ...overrides,
  }
}

async function renderHome(value: { available: boolean; repositories: Repository[] }) {
  const dependencies = {
    IdentityQueryOptions: () => ({
      ...identityQueryOptions(),
      queryFn: () => ({ did: 'did:plc:viewer', handle: 'viewer.example' }),
    }),
    RepositorySnapshotQueryOptions: () => ({
      ...repositorySnapshotQueryOptions(),
      queryFn: () => value,
    }),
    LiveRepositories: () => <p>Live updates connected</p>,
  }
  renderWithAppProviders(
    <Suspense fallback={<p>Loading</p>}>
      <HomePage dependencies={dependencies} />
    </Suspense>,
  )
  await screen.findByRole('heading', { level: 1 })
}

afterEach(cleanup)

describe('HomePage', () => {
  it('invites a first repository when the identity owns none', async () => {
    await renderHome({
      available: true,
      repositories: [repository({ owner: { did: 'did:plc:other', handle: 'other.example' } })],
    })

    expect(screen.getByRole('heading', { name: 'No repositories yet' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Create a repository' })).toBeTruthy()
  })

  it('summarises owned work and links each repository to its issues and proposals', async () => {
    await renderHome({ available: true, repositories: [repository()] })

    expect(screen.getByText('1 repository · 2 open issues · 1 open pull request')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'ledger' }).getAttribute('href')).toBe(
      '/viewer.example/ledger',
    )
    expect(screen.getByRole('link', { name: '2 open issues' }).getAttribute('href')).toBe(
      '/viewer.example/ledger/issues',
    )
    expect(screen.getByRole('link', { name: '1 open pull request' }).getAttribute('href')).toBe(
      '/viewer.example/ledger/pulls',
    )
  })

  it('opens the repository form from the header action', async () => {
    await renderHome({ available: true, repositories: [] })

    fireEvent.click(screen.getByRole('button', { name: 'New repository' }))

    expect(screen.getByRole('heading', { name: 'New repository' })).toBeTruthy()
  })

  it('offers a pull request action only for repositories hosted here', async () => {
    await renderHome({ available: true, repositories: [repository()] })
    expect(screen.getByRole('button', { name: 'New pull request' })).toBeTruthy()
    cleanup()

    await renderHome({
      available: true,
      repositories: [
        repository({
          hosting: {
            local: false,
            web_url: 'https://elsewhere.test/ledger',
            git_https_url: 'https://elsewhere.test/ledger.git',
            git_ssh_url: null,
            source_browsing: 'canonical_host',
          },
        }),
      ],
    })
    expect(screen.queryByRole('button', { name: 'New pull request' })).toBeNull()
  })

  it('keeps the page usable when the repository projection cannot be read', async () => {
    await renderHome({ available: false, repositories: [] })

    expect(screen.getByRole('heading', { name: 'Repository list unavailable' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'New repository' })).toBeTruthy()
  })
})
