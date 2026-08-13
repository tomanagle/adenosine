// @vitest-environment jsdom

import type { Repository } from '@adenosine/api-client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Suspense, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { HomePage } from './home-page'

const { snapshot } = vi.hoisted(() => ({
  snapshot: { value: { available: true, repositories: [] as Repository[] } },
}))

vi.mock('@/features/identity/identity.query', () => ({
  identityQueryOptions: () => ({
    queryKey: ['identity'],
    queryFn: () => ({ did: 'did:plc:viewer', handle: 'viewer.example' }),
  }),
}))

vi.mock('@/features/repositories/repository-snapshot.query', () => ({
  repositorySnapshotQueryOptions: () => ({
    queryKey: ['repository-snapshot'],
    queryFn: () => snapshot.value,
  }),
}))

vi.mock('@/features/repositories/live-repositories', () => ({
  LiveRepositories: () => <p>Live updates connected</p>,
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  useNavigate: () => vi.fn(),
}))

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
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-10T00:00:00Z',
    ...overrides,
  }
}

async function renderHome(value: { available: boolean; repositories: Repository[] }) {
  snapshot.value = value
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <Suspense fallback={<p>Loading</p>}>
        <HomePage />
      </Suspense>
    </QueryClientProvider>,
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
    expect(screen.getByRole('link', { name: 'ledger' }).getAttribute('href')).toBe('/$owner/$repo')
    expect(screen.getByRole('link', { name: '2 open issues' }).getAttribute('href')).toBe(
      '/$owner/$repo/issues',
    )
    expect(screen.getByRole('link', { name: '1 open pull request' }).getAttribute('href')).toBe(
      '/$owner/$repo/pulls',
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
