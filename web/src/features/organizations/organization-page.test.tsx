// @vitest-environment jsdom

import type {
  Organization,
  OrganizationAuditEventList,
  OrganizationInvitationList,
  OrganizationMemberList,
  OrganizationRepositoryCollaboratorList,
  OrganizationTeamList,
  RepositoryList,
} from '@adenosine/api-client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { Suspense } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { OrganizationPage } from './organization-page'

const state = vi.hoisted(() => ({
  organization: {} as Organization,
  members: { items: [], page: { next_cursor: null } } as OrganizationMemberList,
  teams: { items: [], page: { next_cursor: null } } as OrganizationTeamList,
  invitations: { items: [], page: { next_cursor: null } } as OrganizationInvitationList,
  audit: { items: [], page: { next_cursor: null } } as OrganizationAuditEventList,
  repositories: { items: [], page: { next_cursor: null } } as RepositoryList,
  collaborators: {
    items: [],
    page: { next_cursor: null },
  } as OrganizationRepositoryCollaboratorList,
}))

const queryHelpers = vi.hoisted(() => ({
  mutation: () => ({ mutationFn: vi.fn() }),
  infinite: <T,>(queryKey: string, data: () => T) => ({
    queryKey: [queryKey],
    queryFn: data,
    initialPageParam: '',
    getNextPageParam: () => undefined,
  }),
}))

vi.mock('./queries', () => {
  const { infinite, mutation } = queryHelpers
  return {
    organizationQueryOptions: () => ({
      queryKey: ['organization'],
      queryFn: () => state.organization,
    }),
    organizationMembersQueryOptions: () => ({
      queryKey: ['members'],
      queryFn: () => state.members,
    }),
    organizationMembersInfiniteQueryOptions: () =>
      infinite('members-infinite', () => state.members),
    organizationTeamsQueryOptions: () => ({ queryKey: ['teams'], queryFn: () => state.teams }),
    organizationTeamsInfiniteQueryOptions: () => infinite('teams-infinite', () => state.teams),
    organizationOwnerInvitationsQueryOptions: () => ({
      queryKey: ['invitations'],
      queryFn: () => state.invitations,
    }),
    organizationOwnerInvitationsInfiniteQueryOptions: () =>
      infinite('invitations-infinite', () => state.invitations),
    organizationAuditEventsQueryOptions: () => ({
      queryKey: ['audit'],
      queryFn: () => state.audit,
    }),
    organizationAuditEventsInfiniteQueryOptions: () =>
      infinite('audit-infinite', () => state.audit),
    organizationTeamMembersQueryOptions: () => ({
      queryKey: ['team-members'],
      queryFn: () => ({
        items: [
          {
            did: 'did:plc:viewer',
            handle: 'viewer.test',
            role: 'maintainer',
            created_at: '2026-08-13T00:00:00Z',
            updated_at: '2026-08-13T00:00:00Z',
          },
        ],
        page: { next_cursor: null },
      }),
    }),
    organizationTeamMembersInfiniteQueryOptions: () =>
      infinite('team-members-infinite', () => ({
        items: [
          {
            did: 'did:plc:viewer',
            handle: 'viewer.test',
            role: 'maintainer',
            created_at: '2026-08-13T00:00:00Z',
            updated_at: '2026-08-13T00:00:00Z',
          },
        ],
        page: { next_cursor: null },
      })),
    organizationRepositoriesQueryOptions: () => ({
      queryKey: ['repositories'],
      queryFn: () => state.repositories,
    }),
    organizationRepositoriesInfiniteQueryOptions: () =>
      infinite('repositories-infinite', () => state.repositories),
    organizationRepositoryCollaboratorsQueryOptions: () => ({
      queryKey: ['collaborators'],
      queryFn: () => state.collaborators,
    }),
    organizationRepositoryCollaboratorsInfiniteQueryOptions: () =>
      infinite('collaborators-infinite', () => state.collaborators),
    organizationTeamRepositoriesQueryOptions: () => ({
      queryKey: ['team-repositories'],
      queryFn: () => ({ items: [], page: { next_cursor: null } }),
    }),
    organizationTeamRepositoriesInfiniteQueryOptions: () =>
      infinite('team-repositories-infinite', () => ({ items: [], page: { next_cursor: null } })),
    inviteOrganizationMemberMutationOptions: mutation,
    createOrganizationTeamMutationOptions: mutation,
    updateOrganizationTeamMutationOptions: mutation,
    deleteOrganizationTeamMutationOptions: mutation,
    putOrganizationTeamMemberMutationOptions: mutation,
    putOrganizationTeamRepositoryMutationOptions: mutation,
    removeOrganizationMemberMutationOptions: mutation,
    removeOrganizationTeamMemberMutationOptions: mutation,
    removeOrganizationTeamRepositoryMutationOptions: mutation,
    revokeOrganizationInvitationMutationOptions: mutation,
    updateOrganizationMemberMutationOptions: mutation,
    updateOrganizationMutationOptions: mutation,
    putOrganizationRepositoryCollaboratorMutationOptions: mutation,
    removeOrganizationRepositoryCollaboratorMutationOptions: mutation,
  }
})

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

async function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
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
