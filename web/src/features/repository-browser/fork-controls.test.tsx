// @vitest-environment jsdom

import type { Repository, RepositoryForkList } from '@adenosine/api-client'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ForkActions, ForkNetwork } from './fork-controls'
import {
  branchesQueryOptions,
  createRepositoryForkMutationOptions,
  repositoryForksQueryOptions,
  syncRepositoryForkMutationOptions,
} from './queries'
import { organizationsQueryOptions } from '@/features/organizations/queries'
import { repositorySnapshotQueryOptions } from '@/features/repositories/repository-snapshot.query'
import { createTestQueryClient, renderWithAppProviders } from '@/test/render'

type CreateForkMutation = NonNullable<
  ReturnType<typeof createRepositoryForkMutationOptions>['mutationFn']
>
type SyncForkMutation = NonNullable<
  ReturnType<typeof syncRepositoryForkMutationOptions>['mutationFn']
>

const createFork = vi.fn<CreateForkMutation>()
const syncFork = vi.fn<SyncForkMutation>()
let forkPage: RepositoryForkList = { items: [], fork_count: 0, page: { next_cursor: null } }

const dependencies = {
  branchesQueryOptions,
  createRepositoryForkMutationOptions: () => ({
    ...createRepositoryForkMutationOptions(),
    mutationFn: createFork,
  }),
  organizationsQueryOptions: () => ({
    ...organizationsQueryOptions(),
    queryFn: () => ({ items: [], page: { next_cursor: null } }),
  }),
  repositoryForksQueryOptions: (params: { owner: string; repo: string }) => ({
    ...repositoryForksQueryOptions(params),
    queryFn: () => forkPage,
  }),
  repositorySnapshotQueryOptions,
  syncRepositoryForkMutationOptions: () => ({
    ...syncRepositoryForkMutationOptions(),
    mutationFn: syncFork,
  }),
}

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
  archived: false,
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
  const client = createTestQueryClient()
  client.setQueryData(repositorySnapshotQueryOptions().queryKey, {
    repositories: [],
    available: true,
  })
  return {
    client,
    ...renderWithAppProviders(children, { queryClient: client }),
  }
}

beforeEach(() => {
  createFork.mockReset()
  syncFork.mockReset()
  forkPage = { items: [], fork_count: 0, page: { next_cursor: null } }
})
afterEach(cleanup)

describe('ForkActions', () => {
  it('creates a named fork and opens it', async () => {
    createFork.mockResolvedValue(created)
    const { client, router } = renderWithQuery(
      <ForkActions
        dependencies={dependencies}
        identityDid="did:plc:viewer"
        params={params}
        repository={upstream}
      />,
    )

    fireEvent.click(screen.getByText('Fork'))
    fireEvent.change(screen.getByLabelText('Repository name'), {
      target: { value: 'project-copy' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create fork' }))

    await waitFor(() => expect(createFork).toHaveBeenCalledTimes(1))
    expect(createFork.mock.calls[0]?.[0]).toEqual({
      body: { slug: 'project-copy', organization: undefined },
      path: params,
    })
    await waitFor(() => expect(router.state.location.pathname).toBe('/viewer.test/project-copy'))
    expect(
      client.getQueryData<{ repositories: Repository[] }>(repositorySnapshotQueryOptions().queryKey)
        ?.repositories,
    ).toContainEqual(created)
  })

  it('updates the sync button after a successful fast-forward', async () => {
    syncFork.mockResolvedValue({ before_sha: 'before', after_sha: 'after', updated: true })
    renderWithQuery(
      <ForkActions
        dependencies={dependencies}
        identityDid="did:plc:viewer"
        params={params}
        repository={created}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Sync fork' }))

    expect(await screen.findByRole('button', { name: 'Fork updated' })).toBeTruthy()
    expect(syncFork.mock.calls[0]?.[0]).toEqual({ path: params })
  })
})

describe('ForkNetwork', () => {
  it('shows portable ancestry and direct forks', async () => {
    forkPage = { items: [created], fork_count: 1, page: { next_cursor: null } }
    renderWithQuery(
      <ForkNetwork dependencies={dependencies} params={params} repository={upstream} />,
    )

    expect(await screen.findByText('viewer.test/project-copy')).toBeTruthy()
    expect(screen.getByText('1 direct fork')).toBeTruthy()
  })
})
