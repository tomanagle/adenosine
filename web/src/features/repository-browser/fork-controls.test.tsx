// @vitest-environment jsdom

import type { Repository, RepositoryForkList } from '@adenosine/api-client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ForkActions, ForkNetwork } from './fork-controls'

const mocks = vi.hoisted(() => ({
  createFork: vi.fn(),
  syncFork: vi.fn(),
  navigate: vi.fn(),
  forkPage: { items: [], fork_count: 0, page: { next_cursor: null } } as RepositoryForkList,
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  useNavigate: () => mocks.navigate,
}))

vi.mock('@/features/organizations/queries', () => ({
  organizationsQueryOptions: () => ({
    queryKey: ['organizations'],
    queryFn: () => ({ items: [], page: { next_cursor: null } }),
  }),
}))

vi.mock('@/features/repositories/repository-snapshot.query', () => ({
  repositorySnapshotQueryOptions: () => ({ queryKey: ['repository-snapshot'] }),
  retainCreatedRepository: (snapshot: { repositories: Repository[] }, created: Repository) => ({
    ...snapshot,
    repositories: [...snapshot.repositories, created],
  }),
}))

vi.mock('./queries', () => ({
  branchesQueryOptions: () => ({ queryKey: ['branches'] }),
  createRepositoryForkMutationOptions: () => ({ mutationFn: mocks.createFork }),
  repositoryForksQueryOptions: () => ({
    queryKey: ['forks'],
    queryFn: () => mocks.forkPage,
  }),
  syncRepositoryForkMutationOptions: () => ({ mutationFn: mocks.syncFork }),
}))

const upstream: Repository = {
  id: '0198a851-2a89-7ae2-a370-dc68883e3af1',
  uri: 'at://did:plc:alice/dev.adenosine.repo/project',
  cid: 'bafyupstream',
  slug: 'project',
  display_name: 'Project',
  description: 'Portable project',
  visibility: 'public',
  state: 'active',
  default_branch: 'main',
  owner: { did: 'did:plc:alice', handle: 'alice.test', kind: 'account' },
  hosting: {
    local: true,
    web_url: 'https://code.test/alice/project',
    git_https_url: 'https://code.test/alice/project.git',
    git_ssh_url: null,
    source_browsing: 'local',
  },
  star_count: 0,
  issue_count: 0,
  open_issue_count: 0,
  comment_count: 0,
  pull_request_count: 0,
  open_pull_request_count: 0,
  fork_count: 1,
  created_at: '2026-08-13T00:00:00Z',
  updated_at: '2026-08-13T00:00:00Z',
}

const created: Repository = {
  ...upstream,
  id: '0198a851-2a89-7ae2-a370-dc68883e3af2',
  uri: 'at://did:plc:viewer/dev.adenosine.repo/project-copy',
  cid: 'bafyfork',
  slug: 'project-copy',
  owner: { did: 'did:plc:viewer', handle: 'viewer.test', kind: 'account' },
  forked_from: { uri: upstream.uri!, cid: upstream.cid! },
  fork_count: 0,
}

const params = { owner: 'alice.test', repo: 'project' }

function renderWithQuery(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  client.setQueryData(['repository-snapshot'], { repositories: [], available: true })
  return {
    client,
    ...render(<QueryClientProvider client={client}>{children}</QueryClientProvider>),
  }
}

beforeEach(() => {
  mocks.createFork.mockReset()
  mocks.syncFork.mockReset()
  mocks.navigate.mockReset()
  mocks.forkPage = { items: [], fork_count: 0, page: { next_cursor: null } }
})
afterEach(cleanup)

describe('ForkActions', () => {
  it('creates a named fork and opens it', async () => {
    mocks.createFork.mockResolvedValue(created)
    const { client } = renderWithQuery(
      <ForkActions identityDid="did:plc:viewer" params={params} repository={upstream} />,
    )

    fireEvent.click(screen.getByText('Fork'))
    fireEvent.change(screen.getByLabelText('Repository name'), {
      target: { value: 'project-copy' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create fork' }))

    await waitFor(() => expect(mocks.createFork).toHaveBeenCalledTimes(1))
    expect(mocks.createFork.mock.calls[0]?.[0]).toEqual({
      body: { slug: 'project-copy', organization: undefined },
      path: params,
    })
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith({
        to: '/$owner/$repo',
        params: { owner: 'viewer.test', repo: 'project-copy' },
      }),
    )
    expect(
      client.getQueryData<{ repositories: Repository[] }>(['repository-snapshot'])?.repositories,
    ).toContainEqual(created)
  })

  it('updates the sync button after a successful fast-forward', async () => {
    mocks.syncFork.mockResolvedValue({ before_sha: 'before', after_sha: 'after', updated: true })
    renderWithQuery(
      <ForkActions identityDid="did:plc:viewer" params={params} repository={created} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Sync fork' }))

    expect(await screen.findByRole('button', { name: 'Fork updated' })).toBeTruthy()
    expect(mocks.syncFork.mock.calls[0]?.[0]).toEqual({ path: params })
  })
})

describe('ForkNetwork', () => {
  it('shows portable ancestry and direct forks', async () => {
    mocks.forkPage = { items: [created], fork_count: 1, page: { next_cursor: null } }
    renderWithQuery(<ForkNetwork params={params} repository={upstream} />)

    expect(await screen.findByText('viewer.test/project-copy')).toBeTruthy()
    expect(screen.getByText('1 direct fork')).toBeTruthy()
  })
})
