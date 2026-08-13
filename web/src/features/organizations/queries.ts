import {
  acceptOrganizationInvitationMutation,
  createOrganizationMutation,
  createOrganizationTeamMutation,
  deleteOrganizationTeamMutation,
  getOrganizationOptions,
  inviteOrganizationMemberMutation,
  listOrganizationAuditEventsOptions,
  listOrganizationInvitationsOptions,
  listOrganizationInvitationsForCurrentUserOptions,
  listOrganizationMembersOptions,
  listOrganizationRepositoriesOptions,
  listOrganizationRepositoryCollaboratorsOptions,
  listOrganizationRepositoryCollaboratorsInfiniteOptions,
  listOrganizationTeamRepositoriesOptions,
  listOrganizationTeamRepositoriesInfiniteOptions,
  listOrganizationTeamMembersOptions,
  listOrganizationTeamMembersInfiniteOptions,
  listOrganizationTeamsOptions,
  listOrganizationTeamsInfiniteOptions,
  listOrganizationsOptions,
  listOrganizationsInfiniteOptions,
  listOrganizationAuditEventsInfiniteOptions,
  listOrganizationInvitationsInfiniteOptions,
  listOrganizationInvitationsForCurrentUserInfiniteOptions,
  listOrganizationMembersInfiniteOptions,
  listOrganizationRepositoriesInfiniteOptions,
  removeOrganizationMemberMutation,
  removeOrganizationRepositoryCollaboratorMutation,
  removeOrganizationTeamMemberMutation,
  updateOrganizationMemberMutation,
  putOrganizationTeamMemberMutation,
  putOrganizationTeamRepositoryMutation,
  removeOrganizationTeamRepositoryMutation,
  revokeOrganizationInvitationMutation,
  updateOrganizationMutation,
  updateOrganizationTeamMutation,
  putOrganizationRepositoryCollaboratorMutation,
} from '@adenosine/api-client/query'
import type {
  OrganizationAuditEventList,
  OrganizationInvitationList,
  OrganizationList,
  OrganizationMemberList,
  OrganizationRepositoryCollaboratorList,
  OrganizationTeamList,
  OrganizationTeamMemberList,
  OrganizationTeamRepositoryList,
  RepositoryList,
} from '@adenosine/api-client'

import { browserApiClient } from '@/api/browser-client'

export const organizationsQueryOptions = () =>
  listOrganizationsOptions({ client: browserApiClient })
export const organizationInvitationsQueryOptions = () =>
  listOrganizationInvitationsForCurrentUserOptions({ client: browserApiClient })
export const organizationQueryOptions = (organization: string) =>
  getOrganizationOptions({ client: browserApiClient, path: { organization } })
export const organizationMembersQueryOptions = (organization: string) =>
  listOrganizationMembersOptions({ client: browserApiClient, path: { organization } })
export const organizationTeamsQueryOptions = (organization: string) =>
  listOrganizationTeamsOptions({ client: browserApiClient, path: { organization } })
export const organizationTeamMembersQueryOptions = (organization: string, team: string) =>
  listOrganizationTeamMembersOptions({ client: browserApiClient, path: { organization, team } })
export const organizationOwnerInvitationsQueryOptions = (organization: string) =>
  listOrganizationInvitationsOptions({ client: browserApiClient, path: { organization } })
export const organizationAuditEventsQueryOptions = (organization: string) =>
  listOrganizationAuditEventsOptions({ client: browserApiClient, path: { organization } })
export const organizationRepositoriesQueryOptions = (organization: string) =>
  listOrganizationRepositoriesOptions({ client: browserApiClient, path: { organization } })
export const organizationRepositoryCollaboratorsQueryOptions = (
  organization: string,
  repository: string,
) =>
  listOrganizationRepositoryCollaboratorsOptions({
    client: browserApiClient,
    path: { organization, repository },
  })
export const organizationTeamRepositoriesQueryOptions = (organization: string, team: string) =>
  listOrganizationTeamRepositoriesOptions({
    client: browserApiClient,
    path: { organization, team },
  })

const nextCursor = (page: { page: { next_cursor: string | null } }) =>
  page.page.next_cursor ?? undefined

export const organizationsInfiniteQueryOptions = () => ({
  ...listOrganizationsInfiniteOptions({ client: browserApiClient }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationList) => nextCursor(page),
})
export const organizationInvitationsInfiniteQueryOptions = () => ({
  ...listOrganizationInvitationsForCurrentUserInfiniteOptions({ client: browserApiClient }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationInvitationList) => nextCursor(page),
})
export const organizationMembersInfiniteQueryOptions = (organization: string) => ({
  ...listOrganizationMembersInfiniteOptions({ client: browserApiClient, path: { organization } }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationMemberList) => nextCursor(page),
})
export const organizationTeamsInfiniteQueryOptions = (organization: string) => ({
  ...listOrganizationTeamsInfiniteOptions({ client: browserApiClient, path: { organization } }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationTeamList) => nextCursor(page),
})
export const organizationTeamMembersInfiniteQueryOptions = (
  organization: string,
  team: string,
) => ({
  ...listOrganizationTeamMembersInfiniteOptions({
    client: browserApiClient,
    path: { organization, team },
  }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationTeamMemberList) => nextCursor(page),
})
export const organizationOwnerInvitationsInfiniteQueryOptions = (organization: string) => ({
  ...listOrganizationInvitationsInfiniteOptions({
    client: browserApiClient,
    path: { organization },
  }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationInvitationList) => nextCursor(page),
})
export const organizationAuditEventsInfiniteQueryOptions = (organization: string) => ({
  ...listOrganizationAuditEventsInfiniteOptions({
    client: browserApiClient,
    path: { organization },
  }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationAuditEventList) => nextCursor(page),
})
export const organizationRepositoriesInfiniteQueryOptions = (organization: string) => ({
  ...listOrganizationRepositoriesInfiniteOptions({
    client: browserApiClient,
    path: { organization },
  }),
  initialPageParam: '',
  getNextPageParam: (page: RepositoryList) => nextCursor(page),
})
export const organizationRepositoryCollaboratorsInfiniteQueryOptions = (
  organization: string,
  repository: string,
) => ({
  ...listOrganizationRepositoryCollaboratorsInfiniteOptions({
    client: browserApiClient,
    path: { organization, repository },
  }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationRepositoryCollaboratorList) => nextCursor(page),
})
export const organizationTeamRepositoriesInfiniteQueryOptions = (
  organization: string,
  team: string,
) => ({
  ...listOrganizationTeamRepositoriesInfiniteOptions({
    client: browserApiClient,
    path: { organization, team },
  }),
  initialPageParam: '',
  getNextPageParam: (page: OrganizationTeamRepositoryList) => nextCursor(page),
})

export const createOrganizationMutationOptions = () =>
  createOrganizationMutation({ client: browserApiClient })
export const acceptOrganizationInvitationMutationOptions = () =>
  acceptOrganizationInvitationMutation({ client: browserApiClient })
export const inviteOrganizationMemberMutationOptions = () =>
  inviteOrganizationMemberMutation({ client: browserApiClient })
export const updateOrganizationMemberMutationOptions = () =>
  updateOrganizationMemberMutation({ client: browserApiClient })
export const removeOrganizationMemberMutationOptions = () =>
  removeOrganizationMemberMutation({ client: browserApiClient })
export const createOrganizationTeamMutationOptions = () =>
  createOrganizationTeamMutation({ client: browserApiClient })
export const updateOrganizationTeamMutationOptions = () =>
  updateOrganizationTeamMutation({ client: browserApiClient })
export const deleteOrganizationTeamMutationOptions = () =>
  deleteOrganizationTeamMutation({ client: browserApiClient })
export const putOrganizationTeamMemberMutationOptions = () =>
  putOrganizationTeamMemberMutation({ client: browserApiClient })
export const removeOrganizationTeamMemberMutationOptions = () =>
  removeOrganizationTeamMemberMutation({ client: browserApiClient })
export const updateOrganizationMutationOptions = () =>
  updateOrganizationMutation({ client: browserApiClient })
export const revokeOrganizationInvitationMutationOptions = () =>
  revokeOrganizationInvitationMutation({ client: browserApiClient })
export const putOrganizationTeamRepositoryMutationOptions = () =>
  putOrganizationTeamRepositoryMutation({ client: browserApiClient })
export const removeOrganizationTeamRepositoryMutationOptions = () =>
  removeOrganizationTeamRepositoryMutation({ client: browserApiClient })
export const putOrganizationRepositoryCollaboratorMutationOptions = () =>
  putOrganizationRepositoryCollaboratorMutation({ client: browserApiClient })
export const removeOrganizationRepositoryCollaboratorMutationOptions = () =>
  removeOrganizationRepositoryCollaboratorMutation({ client: browserApiClient })
