// @vitest-environment jsdom

import type { Repository } from '@adenosine/api-client'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  repositorySnapshotQueryOptions,
  type RepositorySnapshot,
} from '@/features/repositories/repository-snapshot.query'

import { CreateRepositoryPanel } from './create-repository-panel'
import { createRepositoryMutationOptions } from './home.query'
import { createTestQueryClient, renderWithAppProviders } from '@/test/render'
import { organizationsQueryOptions } from '@/features/organizations/queries'

const { createRepository } = vi.hoisted(() => ({ createRepository: vi.fn() }))

const dependencies = {
  createRepositoryMutationOptions: () => ({
    ...createRepositoryMutationOptions(),
    mutationFn: createRepository,
  }),
  organizationsQueryOptions: () => ({
    ...organizationsQueryOptions(),
    queryFn: () => ({ items: [], page: { next_cursor: null } }),
  }),
}

const created: Repository = {
  id: '00000000-0000-4000-8000-000000000000',
  uri: 'at://did:plc:viewer/sh.adenosine.repository/ledger',
  cid: 'bafy',
  slug: 'ledger',
  display_name: 'Ledger',
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

function renderPanel(client = createTestQueryClient()) {
  return {
    client,
    ...renderWithAppProviders(
      <CreateRepositoryPanel dependencies={dependencies} onClose={() => undefined} />,
      { queryClient: client },
    ),
  }
}

beforeEach(() => createRepository.mockReset())
afterEach(cleanup)

describe('CreateRepositoryPanel', () => {
  it('refuses to submit without a name', async () => {
    renderPanel()

    fireEvent.click(screen.getByRole('button', { name: 'Create repository' }))

    expect(await screen.findByText('Enter a repository name.')).toBeTruthy()
    expect(createRepository).not.toHaveBeenCalled()
  })

  it('reports the slug rule the API enforces', async () => {
    renderPanel()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Ledger Service' } })

    expect(
      await screen.findByText(
        'Use lowercase letters, numbers, dots, and dashes, starting with a letter or number.',
      ),
    ).toBeTruthy()
  })

  it('sends a trimmed request body and then shows how to push', async () => {
    createRepository.mockResolvedValue(created)
    renderPanel()

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: '  ledger  ' } })
    fireEvent.change(screen.getByLabelText('Display name'), { target: { value: ' Ledger ' } })
    fireEvent.change(screen.getByLabelText('Visibility'), { target: { value: 'private' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create repository' }))

    await waitFor(() => expect(createRepository).toHaveBeenCalledTimes(1))
    expect(createRepository.mock.calls[0]?.[0]).toMatchObject({
      body: {
        slug: 'ledger',
        display_name: 'Ledger',
        visibility: 'private',
        default_branch: 'main',
      },
    })

    expect(await screen.findByText('ledger is ready')).toBeTruthy()
    expect(screen.getByText('https://example.test/viewer.example/ledger.git')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Open repository' }).getAttribute('href')).toBe(
      '/viewer.example/ledger',
    )
  })

  it('keeps the created repository in the home snapshot while projection catches up', async () => {
    createRepository.mockResolvedValue(created)
    const client = createTestQueryClient()
    const queryKey = repositorySnapshotQueryOptions().queryKey
    client.setQueryData<RepositorySnapshot>(queryKey, { repositories: [], available: true })
    renderPanel(client)

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'ledger' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create repository' }))

    expect(await screen.findByText('ledger is ready')).toBeTruthy()
    expect(client.getQueryData<RepositorySnapshot>(queryKey)?.repositories).toEqual([created])
  })
})
