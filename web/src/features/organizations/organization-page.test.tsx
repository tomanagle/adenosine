// @vitest-environment jsdom

import type {
  Organization,
  OrganizationAuditEventList,
  OrganizationInvitationList,
  OrganizationMemberList,
  OrganizationRepositoryCollaboratorList,
  OrganizationTeamMemberList,
  OrganizationTeamRepositoryList,
  OrganizationTeamList,
  RepositoryList,
} from '@adenosine/api-client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Suspense } from 'react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { OrganizationPage } from './organization-page'
import {
  organizationAuditEventsInfiniteQueryOptions,
  organizationMembersInfiniteQueryOptions,
  organizationOwnerInvitationsInfiniteQueryOptions,
  organizationQueryOptions,
  organizationRepositoriesInfiniteQueryOptions,
  organizationRepositoryCollaboratorsInfiniteQueryOptions,
  organizationTeamMembersInfiniteQueryOptions,
  organizationTeamRepositoriesInfiniteQueryOptions,
  organizationTeamsInfiniteQueryOptions,
} from './queries'

type OrganizationTestState = {
  organization: Organization
  members: OrganizationMemberList
  teams: OrganizationTeamList
  invitations: OrganizationInvitationList
  audit: OrganizationAuditEventList
  repositories: RepositoryList
  collaborators: OrganizationRepositoryCollaboratorList
  teamMembers: OrganizationTeamMemberList
  teamRepositories: OrganizationTeamRepositoryList
}

let state: OrganizationTestState

const now = '2026-08-13T00:00:00Z'

function organization(overrides: Partial<Organization> = {}): Organization {
  return {
    id: '0198a851-2a89-7ae2-a370-dc68883e3af1',
    slug: 'adenosine',
    name: 'Adenosine',
    creator_did: 'did:plc:alice',
    base_permission: 'read',
    members_can_create_repositories: true,
    state: 'active',
    created_at: now,
    updated_at: now,
    ...overrides,
  }
}

function infiniteData<T>(page: T) {
  return { pages: [page], pageParams: [''] }
}

beforeEach(() => {
  state = {
    organization: organization(),
    members: { items: [], page: { next_cursor: null } },
    teams: { items: [], page: { next_cursor: null } },
    invitations: { items: [], page: { next_cursor: null } },
    audit: { items: [], page: { next_cursor: null } },
    repositories: { items: [], page: { next_cursor: null } },
    collaborators: { items: [], page: { next_cursor: null } },
    teamMembers: {
      items: [
        {
          did: 'did:plc:viewer',
          handle: 'viewer.test',
          role: 'maintainer',
          created_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    },
    teamRepositories: { items: [], page: { next_cursor: null } },
  }
})

async function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Number.POSITIVE_INFINITY } },
  })
  client.setQueryData(organizationQueryOptions('adenosine').queryKey, state.organization)
  client.setQueryData(
    organizationMembersInfiniteQueryOptions('adenosine').queryKey,
    infiniteData(state.members),
  )
  client.setQueryData(
    organizationTeamsInfiniteQueryOptions('adenosine').queryKey,
    infiniteData(state.teams),
  )
  client.setQueryData(
    organizationOwnerInvitationsInfiniteQueryOptions('adenosine').queryKey,
    infiniteData(state.invitations),
  )
  client.setQueryData(
    organizationAuditEventsInfiniteQueryOptions('adenosine').queryKey,
    infiniteData(state.audit),
  )
  client.setQueryData(
    organizationRepositoriesInfiniteQueryOptions('adenosine').queryKey,
    infiniteData(state.repositories),
  )
  for (const repository of state.repositories.items) {
    if (!repository.id) continue
    client.setQueryData(
      organizationRepositoryCollaboratorsInfiniteQueryOptions('adenosine', repository.id).queryKey,
      infiniteData(state.collaborators),
    )
  }
  for (const team of state.teams.items) {
    client.setQueryData(
      organizationTeamMembersInfiniteQueryOptions('adenosine', team.id).queryKey,
      infiniteData(state.teamMembers),
    )
    client.setQueryData(
      organizationTeamRepositoriesInfiniteQueryOptions('adenosine', team.id).queryKey,
      infiniteData(state.teamRepositories),
    )
  }
  render(
    <QueryClientProvider client={client}>
      <Suspense fallback={<p>Loading</p>}>
        <OrganizationPage
          identity={{ did: 'did:plc:viewer', handle: 'viewer.test' }}
          slug="adenosine"
        />
      </Suspense>
    </QueryClientProvider>,
  )
  await screen.findByRole('heading', { name: 'Adenosine' })
}

afterEach(cleanup)

describe('OrganizationPage', () => {
  it('shows founding-owner protection and the owner audit log', async () => {
    state.organization = organization({ viewer_role: 'owner' })
    state.members = {
      items: [
        {
          did: 'did:plc:alice',
          handle: 'alice.test',
          role: 'owner',
          visibility: 'private',
          joined_at: now,
          updated_at: now,
        },
        {
          did: 'did:plc:viewer',
          handle: 'viewer.test',
          role: 'owner',
          visibility: 'private',
          joined_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.teams = { items: [], page: { next_cursor: null } }
    state.invitations = { items: [], page: { next_cursor: null } }
    state.audit = {
      items: [
        {
          id: '0198a851-2a89-7ae2-a370-dc68883e3af2',
          actor_did: 'did:plc:viewer',
          action: 'member.role',
          target_type: 'member',
          target_id: 'did:plc:bob',
          request_id: 'request-1',
          metadata: {},
          created_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.repositories = { items: [], page: { next_cursor: null } }

    await renderPage()

    expect(await screen.findByText('founding owner')).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Audit log' })).toBeTruthy()
    expect(screen.getByText('Request request-1')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull()
  })

  it('lets a team maintainer administer team membership without organization-owner controls', async () => {
    state.organization = organization({ viewer_role: 'member' })
    state.members = {
      items: [
        {
          did: 'did:plc:viewer',
          handle: 'viewer.test',
          role: 'member',
          visibility: 'private',
          joined_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.teams = {
      items: [
        {
          id: '0198a851-2a89-7ae2-a370-dc68883e3af3',
          organization_id: state.organization.id,
          slug: 'core',
          name: 'Core',
          visibility: 'visible',
          viewer_role: 'maintainer',
          created_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.invitations = { items: [], page: { next_cursor: null } }
    state.audit = { items: [], page: { next_cursor: null } }
    state.repositories = { items: [], page: { next_cursor: null } }

    await renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /Core/ }))

    expect(await screen.findByRole('combobox', { name: 'Team role for viewer.test' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Remove' })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Organization settings' })).toBeNull()
    expect(screen.queryByRole('heading', { name: 'Audit log' })).toBeNull()
  })

  it('gives owners a direct outside-collaborator management flow', async () => {
    state.organization = organization({ viewer_role: 'owner' })
    state.members = {
      items: [
        {
          did: 'did:plc:alice',
          handle: 'alice.test',
          role: 'owner',
          visibility: 'private',
          joined_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.teams = { items: [], page: { next_cursor: null } }
    state.invitations = { items: [], page: { next_cursor: null } }
    state.audit = { items: [], page: { next_cursor: null } }
    state.repositories = {
      items: [
        {
          id: '0198a851-2a89-7ae2-a370-dc68883e3af4',
          slug: 'ledger',
          visibility: 'private',
          state: 'active',
          default_branch: 'main',
          archived: false,
          viewer_can_admin: true,
          owner: { did: 'did:plc:alice', kind: 'organization', organization_slug: 'adenosine' },
          hosting: {
            local: true,
            web_url: 'https://example.test/adenosine/ledger',
            git_https_url: 'https://example.test/adenosine/ledger.git',
            source_browsing: 'local',
          },
          star_count: 0,
          issue_count: 0,
          open_issue_count: 0,
          comment_count: 0,
          pull_request_count: 0,
          open_pull_request_count: 0,
          fork_count: 0,
          created_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }
    state.collaborators = {
      items: [
        {
          repository_id: state.repositories.items[0]!.id!,
          did: 'did:plc:outside',
          handle: 'outside.test',
          role: 'read',
          created_at: now,
          updated_at: now,
        },
      ],
      page: { next_cursor: null },
    }

    await renderPage()
    fireEvent.click(await screen.findByRole('button', { name: /ledger/ }))

    expect(
      await screen.findByRole('combobox', { name: 'Repository role for outside.test' }),
    ).toBeTruthy()
    expect(screen.getByRole('textbox', { name: 'Collaborator DID for ledger' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Add collaborator' })).toBeTruthy()
  })
})
