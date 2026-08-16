// @vitest-environment jsdom

import type { PullRequest, Repository } from '@adenosine/api-client'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CreatePullRequestPanel } from './create-pull-request-panel'
import { createPullRequestMutationOptions } from './home.query'
import { createTestQueryClient, renderWithAppProviders } from '@/test/render'
import { pullRequestsQueryOptions } from '@/features/collaboration/queries'
import { branchesQueryOptions } from '@/features/repository-browser/queries'

const headSha = 'a'.repeat(40)
const pullRequest: PullRequest = {
  uri: 'at://did:plc:viewer/sh.adenosine.pullRequest/1',
  cid: 'bafypull',
  author_did: 'did:plc:viewer',
  source_repository_uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger',
  source_repository_cid: 'bafy',
  source_branch: 'feature',
  target_repository_uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger',
  target_repository_cid: 'bafy',
  target_branch: 'main',
  head_sha: headSha,
  title: 'Reconcile balances',
  body: '',
  state: 'open',
  status_uri: null,
  status_cid: null,
  merged_commit_sha: null,
  review_count: 0,
  created_at: '2026-08-12T00:00:00Z',
  updated_at: '2026-08-12T00:00:00Z',
  indexed_at: '2026-08-12T00:00:00Z',
}

type CreatePullRequestMutation = NonNullable<
  ReturnType<typeof createPullRequestMutationOptions>['mutationFn']
>

const createPullRequest = vi.fn<CreatePullRequestMutation>()

const dependencies = {
  createPullRequestMutationOptions: () => ({
    ...createPullRequestMutationOptions(),
    mutationFn: createPullRequest,
  }),
  branchesQueryOptions: (params: { owner: string; repo: string }) => ({
    ...branchesQueryOptions(params),
    queryFn: () => ({
      items: [
        { name: 'main', sha: 'b'.repeat(40), default: true },
        { name: 'feature', sha: headSha, default: false },
      ],
      page: { next_cursor: null },
    }),
  }),
  pullRequestsQueryOptions: (uri: string) => ({
    ...pullRequestsQueryOptions(uri),
    queryFn: () => ({
      items: [pullRequest],
      page: { next_cursor: null },
      pull_request_count: 1,
      open_pull_request_count: 1,
    }),
  }),
}

const repository: Repository = {
  id: '00000000-0000-4000-8000-000000000000',
  uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger',
  cid: 'bafy',
  slug: 'ledger',
  display_name: null,
  description: null,
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
  star_count: 0,
  issue_count: 0,
  open_issue_count: 0,
  comment_count: 0,
  pull_request_count: 0,
  open_pull_request_count: 0,
  fork_count: 0,
  created_at: '2026-08-12T00:00:00Z',
  updated_at: '2026-08-12T00:00:00Z',
}

function renderPanel(
  repositories: Repository[] = [repository],
  networkRepositories: Repository[] = [repository],
) {
  const client = createTestQueryClient()
  return renderWithAppProviders(
    <CreatePullRequestPanel
      dependencies={dependencies}
      networkRepositories={networkRepositories}
      onClose={() => undefined}
      repositories={repositories}
    />,
    { queryClient: client },
  )
}

async function waitForBranches() {
  await waitFor(() => expect(screen.getAllByRole('option', { name: 'feature' })).toHaveLength(2))
}

beforeEach(() => {
  createPullRequest.mockReset()
})
afterEach(cleanup)

describe('CreatePullRequestPanel', () => {
  it('offers the repository branches once they load', async () => {
    renderPanel()
    await waitForBranches()

    expect(screen.getByLabelText<HTMLSelectElement>('Target branch').value).toBe('main')
    expect(screen.getByLabelText<HTMLSelectElement>('Source branch').disabled).toBe(false)
  })

  it('refuses a proposal onto the same branch', async () => {
    renderPanel()
    await waitForBranches()

    fireEvent.change(screen.getByLabelText('Source branch'), { target: { value: 'main' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Reconcile balances' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open pull request' }))

    expect(
      await screen.findByText('Pick a source branch that differs from the target branch.'),
    ).toBeTruthy()
    expect(createPullRequest).not.toHaveBeenCalled()
  })

  it('records the source branch head and opens the repository proposals', async () => {
    createPullRequest.mockResolvedValue({ pull_request: pullRequest, projected: false })
    const { router } = renderPanel()
    await waitForBranches()

    fireEvent.change(screen.getByLabelText('Source branch'), { target: { value: 'feature' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: ' Reconcile balances ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open pull request' }))

    await waitFor(() => expect(createPullRequest).toHaveBeenCalledTimes(1))
    expect(createPullRequest.mock.calls[0]?.[0]).toMatchObject({
      body: {
        source_repository_uri: repository.uri,
        target_repository_uri: repository.uri,
        source_branch: 'feature',
        target_branch: 'main',
        head_sha: headSha,
        title: 'Reconcile balances',
      },
    })
    await waitFor(() => expect(router.state.location.pathname).toBe('/viewer.example/ledger/pulls'))
  })

  it('requires a title', async () => {
    renderPanel()
    await waitForBranches()

    fireEvent.change(screen.getByLabelText('Source branch'), { target: { value: 'feature' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open pull request' }))

    expect(await screen.findByText('Enter a title.')).toBeTruthy()
    expect(createPullRequest).not.toHaveBeenCalled()
  })

  it('targets a fork upstream and opens the upstream proposal list', async () => {
    const upstream = {
      ...repository,
      uri: 'at://did:plc:upstream/sh.adenosine.repository/ledger',
      cid: 'bafyupstream',
      owner: { did: 'did:plc:upstream', handle: 'upstream.example' },
    }
    const fork = {
      ...repository,
      uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger-fork',
      cid: 'bafyfork',
      slug: 'ledger-fork',
      forked_from: { uri: upstream.uri, cid: upstream.cid },
    }
    createPullRequest.mockResolvedValue({ pull_request: pullRequest, projected: false })
    const { router } = renderPanel([fork], [fork, upstream])
    await waitForBranches()

    expect(screen.getByLabelText<HTMLInputElement>('Target repository').value).toBe(
      'upstream.example/ledger',
    )
    fireEvent.change(screen.getByLabelText('Source branch'), { target: { value: 'main' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Send upstream' } })
    fireEvent.click(screen.getByRole('button', { name: 'Open pull request' }))

    await waitFor(() => expect(createPullRequest).toHaveBeenCalledTimes(1))
    expect(createPullRequest.mock.calls[0]?.[0]).toMatchObject({
      body: {
        source_repository_uri: fork.uri,
        target_repository_uri: upstream.uri,
        source_branch: 'main',
        target_branch: 'main',
      },
    })
    await waitFor(() =>
      expect(router.state.location.pathname).toBe('/upstream.example/ledger/pulls'),
    )
  })
})
