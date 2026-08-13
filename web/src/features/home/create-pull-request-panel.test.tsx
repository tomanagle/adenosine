// @vitest-environment jsdom

import type { Repository } from '@adenosine/api-client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CreatePullRequestPanel } from './create-pull-request-panel'

const headSha = 'a'.repeat(40)
const pullRequest = { uri: 'at://did:plc:viewer/sh.adenosine.pullRequest/1', cid: 'bafypull' }

const { createPullRequest, navigate } = vi.hoisted(() => ({
  createPullRequest: vi.fn(),
  navigate: vi.fn(),
}))

vi.mock('./home.query', () => ({
  createPullRequestMutationOptions: () => ({ mutationFn: createPullRequest }),
}))

vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))

vi.mock('@/features/repository-browser/queries', () => ({
  branchesQueryOptions: (params: { owner: string; repo: string }) => ({
    queryKey: ['branches', params],
    queryFn: () => ({
      items: [
        { name: 'main', sha: 'b'.repeat(40), default: true },
        { name: 'feature', sha: headSha, default: false },
      ],
    }),
  }),
}))

vi.mock('@/features/collaboration/queries', () => ({
  pullRequestsQueryOptions: (uri: string) => ({
    queryKey: ['pull-requests', uri],
    queryFn: () => ({
      items: [pullRequest],
      page: { next_cursor: null },
      pull_request_count: 1,
      open_pull_request_count: 1,
    }),
  }),
}))

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
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <CreatePullRequestPanel
        networkRepositories={networkRepositories}
        onClose={() => undefined}
        repositories={repositories}
      />
    </QueryClientProvider>,
  )
}

async function waitForBranches() {
  await waitFor(() => expect(screen.getAllByRole('option', { name: 'feature' })).toHaveLength(2))
}

beforeEach(() => {
  createPullRequest.mockReset()
  navigate.mockReset()
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
    renderPanel()
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
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        params: { owner: 'viewer.example', repo: 'ledger' },
        to: '/$owner/$repo/pulls',
      }),
    )
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
    renderPanel([fork], [fork, upstream])
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
      expect(navigate).toHaveBeenCalledWith({
        params: { owner: 'upstream.example', repo: 'ledger' },
        to: '/$owner/$repo/pulls',
      }),
    )
  })
})
