import type {
  CurrentIdentity,
  Organization,
  OrganizationAuditEvent,
  OrganizationInvitation,
  OrganizationMember,
  OrganizationRepositoryCollaborator,
  OrganizationTeam,
} from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import {
  Building2,
  ExternalLink,
  LockKeyhole,
  MapPin,
  ShieldCheck,
  UserPlus,
  Users,
} from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'

import {
  inviteOrganizationMemberMutationOptions,
  createOrganizationTeamMutationOptions,
  deleteOrganizationTeamMutationOptions,
  organizationAuditEventsInfiniteQueryOptions,
  organizationMembersInfiniteQueryOptions,
  organizationMembersQueryOptions,
  organizationOwnerInvitationsQueryOptions,
  organizationOwnerInvitationsInfiniteQueryOptions,
  organizationQueryOptions,
  organizationRepositoriesInfiniteQueryOptions,
  organizationRepositoryCollaboratorsQueryOptions,
  organizationRepositoryCollaboratorsInfiniteQueryOptions,
  organizationTeamMembersQueryOptions,
  organizationTeamMembersInfiniteQueryOptions,
  organizationTeamRepositoriesQueryOptions,
  organizationTeamRepositoriesInfiniteQueryOptions,
  organizationTeamsQueryOptions,
  organizationTeamsInfiniteQueryOptions,
  putOrganizationTeamMemberMutationOptions,
  putOrganizationRepositoryCollaboratorMutationOptions,
  putOrganizationTeamRepositoryMutationOptions,
  removeOrganizationMemberMutationOptions,
  removeOrganizationRepositoryCollaboratorMutationOptions,
  removeOrganizationTeamMemberMutationOptions,
  removeOrganizationTeamRepositoryMutationOptions,
  revokeOrganizationInvitationMutationOptions,
  updateOrganizationMemberMutationOptions,
  updateOrganizationMutationOptions,
  updateOrganizationTeamMutationOptions,
} from './queries'

type RepositoryRole = OrganizationRepositoryCollaborator['role']

function organizationBasePermission(value: string): Organization['base_permission'] {
  switch (value) {
    case 'read':
    case 'write':
      return value
    default:
      return 'none'
  }
}

function repositoryRole(value: string): RepositoryRole {
  switch (value) {
    case 'triage':
    case 'write':
    case 'maintain':
    case 'admin':
      return value
    default:
      return 'read'
  }
}

function organizationMemberRole(value: string): OrganizationMember['role'] {
  return value === 'owner' ? 'owner' : 'member'
}

function teamVisibility(value: string): OrganizationTeam['visibility'] {
  return value === 'secret' ? 'secret' : 'visible'
}

export function OrganizationPage({
  identity,
  slug,
}: {
  identity?: CurrentIdentity | null
  slug: string
}) {
  const { data: organization } = useSuspenseQuery(organizationQueryOptions(slug))
  const membersQuery = useInfiniteQuery(organizationMembersInfiniteQueryOptions(slug))
  const members = (membersQuery.data?.pages ?? []).flatMap((page) => page.items)
  const owner = organization.viewer_role === 'owner'

  return (
    <main>
      <section className="border-b bg-muted/20">
        <div className="mx-auto max-w-6xl px-5 py-9 sm:px-8 sm:py-12">
          <div className="flex flex-col gap-6 sm:flex-row sm:items-start">
            <span className="grid size-20 shrink-0 place-items-center rounded-2xl border bg-card text-primary shadow-sm">
              <Building2 className="size-9" />
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h1 className="font-serif text-4xl tracking-tight">{organization.name}</h1>
                {organization.viewer_role ? (
                  <Badge variant="outline">{organization.viewer_role}</Badge>
                ) : null}
              </div>
              <p className="mt-1 font-mono text-sm text-muted-foreground">@{organization.slug}</p>
              {organization.description ? (
                <p className="mt-4 max-w-3xl text-base leading-7">{organization.description}</p>
              ) : null}
              <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-sm text-muted-foreground">
                {organization.location ? (
                  <span className="flex items-center gap-1.5">
                    <MapPin className="size-4" />
                    {organization.location}
                  </span>
                ) : null}
                {organization.website ? (
                  <a
                    className="flex items-center gap-1.5 hover:text-foreground hover:underline"
                    href={organization.website}
                    rel="noreferrer"
                    target="_blank"
                  >
                    Website <ExternalLink className="size-3.5" />
                  </a>
                ) : null}
                <span className="flex items-center gap-1.5">
                  <Users className="size-4" />
                  {members.length}
                  {membersQuery.hasNextPage ? '+' : ''}{' '}
                  {members.length === 1 ? 'member' : 'members'}
                </span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <div className="mx-auto grid max-w-6xl gap-8 px-5 py-9 sm:px-8 lg:grid-cols-[1fr_18rem]">
        <div className="min-w-0 space-y-10">
          {owner ? <OrganizationSettings organization={organization} slug={slug} /> : null}
          <MembersSection
            creatorDID={organization.creator_did}
            identity={identity}
            fetchNextPage={() => membersQuery.fetchNextPage()}
            hasNextPage={membersQuery.hasNextPage}
            isFetchingNextPage={membersQuery.isFetchingNextPage}
            members={members}
            owner={owner}
            slug={slug}
          />
          {owner ? <PendingInvitations slug={slug} /> : null}
          {organization.viewer_role ? (
            <TeamsSection members={members} owner={owner} slug={slug} />
          ) : null}
          {organization.viewer_role ? <OutsideCollaborators slug={slug} /> : null}
          {owner ? <AuditLog slug={slug} /> : null}
        </div>
        <aside className="space-y-4 lg:sticky lg:top-24 lg:self-start">
          <section className="rounded-xl border bg-card p-5">
            <h2 className="font-serif text-xl">Repository policy</h2>
            <dl className="mt-4 space-y-4 text-sm">
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Base permission
                </dt>
                <dd className="mt-1 font-medium capitalize">{organization.base_permission}</dd>
              </div>
              <div>
                <dt className="text-xs uppercase tracking-wider text-muted-foreground">
                  Member creation
                </dt>
                <dd className="mt-1 font-medium">
                  {organization.members_can_create_repositories ? 'Allowed' : 'Owners only'}
                </dd>
              </div>
            </dl>
            <p className="mt-4 border-t pt-4 text-xs leading-5 text-muted-foreground">
              Direct and team repository roles can only add access; the strongest permission wins.
            </p>
          </section>
          <section className="rounded-xl border bg-card p-5">
            <h2 className="flex items-center gap-2 font-serif text-xl">
              <ShieldCheck className="size-4 text-primary" />
              Portable authority
            </h2>
            <p className="mt-3 text-xs leading-5 text-muted-foreground">
              The organization root, owner grants, member consent, and revocations are signed
              records in their authors’ AT Protocol repositories.
            </p>
          </section>
        </aside>
      </div>
    </main>
  )
}

function OrganizationSettings({
  organization,
  slug,
}: {
  organization: Organization
  slug: string
}) {
  const queryClient = useQueryClient()
  const mutation = useMutation(updateOrganizationMutationOptions())
  const [open, setOpen] = useState(false)
  const form = useForm({
    defaultValues: {
      name: organization.name,
      description: organization.description ?? '',
      website: organization.website ?? '',
      location: organization.location ?? '',
      basePermission: organization.base_permission,
      membersCanCreate: organization.members_can_create_repositories,
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        path: { organization: slug },
        body: {
          name: value.name.trim(),
          description: value.description.trim() || undefined,
          website: value.website.trim() || undefined,
          location: value.location.trim() || undefined,
          base_permission: value.basePermission,
          members_can_create_repositories: value.membersCanCreate,
        },
      })
      await queryClient.invalidateQueries({ queryKey: organizationQueryOptions(slug).queryKey })
      setOpen(false)
    },
  })
  return (
    <section aria-labelledby="organization-settings-heading">
      <div className="flex items-end justify-between gap-3 border-b pb-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">Owners</p>
          <h2 className="mt-1 font-serif text-3xl" id="organization-settings-heading">
            Organization settings
          </h2>
        </div>
        <Button onClick={() => setOpen((value) => !value)} size="sm" variant="outline">
          {open ? 'Close' : 'Edit settings'}
        </Button>
      </div>
      {open ? (
        <form
          className="mt-5 grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="name">
            {(field) => (
              <Field htmlFor="organization-name" label="Name">
                <Input
                  id="organization-name"
                  maxLength={255}
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  required
                  value={field.state.value}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="location">
            {(field) => (
              <Field htmlFor="organization-location" label="Location">
                <Input
                  id="organization-location"
                  maxLength={255}
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="website">
            {(field) => (
              <Field htmlFor="organization-website" label="Website">
                <Input
                  id="organization-website"
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  type="url"
                  value={field.state.value}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="basePermission">
            {(field) => (
              <Field htmlFor="organization-base-permission" label="Base repository permission">
                <Select
                  id="organization-base-permission"
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) =>
                    field.handleChange(organizationBasePermission(event.target.value))
                  }
                  value={field.state.value}
                >
                  <option value="none">None</option>
                  <option value="read">Read</option>
                  <option value="write">Write</option>
                </Select>
              </Field>
            )}
          </form.Field>
          <form.Field name="description">
            {(field) => (
              <Field
                className="sm:col-span-2"
                htmlFor="organization-description"
                label="Description"
              >
                <Textarea
                  id="organization-description"
                  maxLength={2000}
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="membersCanCreate">
            {(field) => (
              <label className="flex items-center gap-3 text-sm sm:col-span-2">
                <Input
                  checked={field.state.value}
                  className="size-4"
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.checked)}
                  type="checkbox"
                />{' '}
                Members may create organization repositories
              </label>
            )}
          </form.Field>
          <div className="flex items-center gap-2 sm:col-span-2">
            <Button disabled={mutation.isPending} type="submit">
              {mutation.isPending ? 'Publishing…' : 'Save and publish'}
            </Button>
            <p className="text-xs text-muted-foreground">
              Updates the local policy and the creator-authored AT Protocol root.
            </p>
          </div>
          {mutation.isError ? (
            <Alert className="sm:col-span-2">
              <AlertTitle>Settings not saved</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(mutation.error, 'The organization could not be updated.')}
              </AlertDescription>
            </Alert>
          ) : null}
        </form>
      ) : null}
    </section>
  )
}

function PendingInvitations({ slug }: { slug: string }) {
  const queryClient = useQueryClient()
  const query = useInfiniteQuery(organizationOwnerInvitationsInfiniteQueryOptions(slug))
  const invitations = (query.data?.pages ?? []).flatMap((page) => page.items)
  const revoke = useMutation(revokeOrganizationInvitationMutationOptions())
  const refresh = () =>
    queryClient.invalidateQueries({
      queryKey: organizationOwnerInvitationsQueryOptions(slug).queryKey,
    })
  if (invitations.length === 0) return null
  return (
    <section aria-labelledby="pending-invitations-heading">
      <div className="border-b pb-4">
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">Access</p>
        <h2 className="mt-1 font-serif text-3xl" id="pending-invitations-heading">
          Pending invitations
        </h2>
      </div>
      <ul className="divide-y">
        {invitations.map((invitation: OrganizationInvitation) => (
          <li className="flex flex-wrap items-center gap-3 py-4" key={invitation.id}>
            <div className="min-w-0 flex-1">
              <p className="truncate font-mono text-sm">{invitation.invitee_did}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                {invitation.role} · expires {new Date(invitation.expires_at).toLocaleDateString()}
              </p>
            </div>
            <Button
              disabled={revoke.isPending}
              onClick={() =>
                void revoke
                  .mutateAsync({ path: { organization: slug, invitation: invitation.id } })
                  .then(refresh)
              }
              size="sm"
              variant="ghost"
            >
              Revoke
            </Button>
          </li>
        ))}
      </ul>
      {query.hasNextPage ? (
        <LoadMoreButton
          isFetching={query.isFetchingNextPage}
          label="invitations"
          onClick={() => query.fetchNextPage()}
        />
      ) : null}
    </section>
  )
}

function AuditLog({ slug }: { slug: string }) {
  const query = useInfiniteQuery(organizationAuditEventsInfiniteQueryOptions(slug))
  const events = (query.data?.pages ?? []).flatMap((page) => page.items)
  return (
    <section aria-labelledby="audit-log-heading">
      <div className="border-b pb-4">
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">Security</p>
        <h2 className="mt-1 font-serif text-3xl" id="audit-log-heading">
          Audit log
        </h2>
      </div>
      {events.length === 0 ? (
        <p className="py-6 text-sm text-muted-foreground">No organization changes recorded yet.</p>
      ) : (
        <ol className="divide-y">
          {events.map((event: OrganizationAuditEvent) => (
            <li className="grid gap-1 py-4 sm:grid-cols-[1fr_auto]" key={event.id}>
              <p className="text-sm">
                <span className="font-mono text-xs text-muted-foreground">{event.actor_did}</span>{' '}
                <span className="font-medium">{auditActionLabel(event.action)}</span>{' '}
                <span className="font-mono text-xs text-muted-foreground">{event.target_id}</span>
              </p>
              <time className="text-xs text-muted-foreground" dateTime={event.created_at}>
                {new Date(event.created_at).toLocaleString()}
              </time>
              {event.request_id ? (
                <p className="font-mono text-[11px] text-muted-foreground sm:col-span-2">
                  Request {event.request_id}
                </p>
              ) : null}
            </li>
          ))}
        </ol>
      )}
      {query.hasNextPage ? (
        <LoadMoreButton
          isFetching={query.isFetchingNextPage}
          label="audit events"
          onClick={() => query.fetchNextPage()}
        />
      ) : null}
    </section>
  )
}

function OutsideCollaborators({ slug }: { slug: string }) {
  const query = useInfiniteQuery(organizationRepositoriesInfiniteQueryOptions(slug))
  const repositories = (query.data?.pages ?? []).flatMap((page) => page.items)
  const manageableRepositories = repositories.filter((repository) => repository.viewer_can_admin)
  if (repositories.length === 0 && !query.hasNextPage) return null
  return (
    <section aria-labelledby="outside-collaborators-heading">
      <div className="border-b pb-4">
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">
          Direct access
        </p>
        <h2 className="mt-1 font-serif text-3xl" id="outside-collaborators-heading">
          Repository collaborators
        </h2>
        <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
          Give a person access to one repository without making them an organization member.
        </p>
      </div>
      <div className="mt-5 grid gap-4">
        {manageableRepositories.map((repository) =>
          repository.id ? (
            <RepositoryCollaborators
              key={repository.id}
              repositoryID={repository.id}
              repositorySlug={repository.slug}
              slug={slug}
            />
          ) : null,
        )}
        {manageableRepositories.length === 0 ? (
          <p className="rounded-xl border border-dashed p-5 text-sm text-muted-foreground">
            You do not have admin access to a repository in this page of results.
          </p>
        ) : null}
      </div>
      {query.hasNextPage ? (
        <LoadMoreButton
          isFetching={query.isFetchingNextPage}
          label="repositories"
          onClick={() => query.fetchNextPage()}
        />
      ) : null}
    </section>
  )
}

function RepositoryCollaborators({
  repositoryID,
  repositorySlug,
  slug,
}: {
  repositoryID: string
  repositorySlug: string
  slug: string
}) {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const collaborators = useInfiniteQuery({
    ...organizationRepositoryCollaboratorsInfiniteQueryOptions(slug, repositoryID),
    enabled: open,
  })
  const collaboratorItems = collaborators.data?.pages.flatMap((page) => page.items) ?? []
  const put = useMutation(putOrganizationRepositoryCollaboratorMutationOptions())
  const remove = useMutation(removeOrganizationRepositoryCollaboratorMutationOptions())
  const refresh = () =>
    queryClient.invalidateQueries({
      queryKey: organizationRepositoryCollaboratorsQueryOptions(slug, repositoryID).queryKey,
    })
  const form = useForm({
    defaultValues: {
      did: '',
      role: repositoryRole('read'),
    },
    onSubmit: async ({ value, formApi }) => {
      if (!value.did.trim()) return
      await put.mutateAsync({
        path: { organization: slug, repository: repositoryID, collaborator: value.did.trim() },
        body: { role: value.role },
      })
      formApi.reset()
      await refresh()
    },
  })
  const updateRole = (
    collaborator: OrganizationRepositoryCollaborator,
    role: typeof form.state.values.role,
  ) =>
    put
      .mutateAsync({
        path: { organization: slug, repository: repositoryID, collaborator: collaborator.did },
        body: { role },
      })
      .then(refresh)
  return (
    <article className="rounded-xl border bg-card">
      <button
        className="flex w-full items-center justify-between gap-4 p-5 text-left"
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        <span className="font-medium">{repositorySlug}</span>
        <Badge variant="outline">{open ? 'Close' : 'Manage access'}</Badge>
      </button>
      {open ? (
        <div className="border-t p-5">
          <ul className="space-y-2">
            {collaboratorItems.map((collaborator) => (
              <li className="flex flex-wrap items-center gap-2 text-sm" key={collaborator.did}>
                <span className="min-w-0 flex-1 truncate">
                  {collaborator.handle ? `@${collaborator.handle}` : collaborator.did}
                </span>
                <Select
                  aria-label={`Repository role for ${collaborator.handle ?? collaborator.did}`}
                  className="h-8 w-32"
                  disabled={put.isPending}
                  onChange={(event) =>
                    void updateRole(collaborator, repositoryRole(event.target.value))
                  }
                  value={collaborator.role}
                >
                  <option value="read">Read</option>
                  <option value="triage">Triage</option>
                  <option value="write">Write</option>
                  <option value="maintain">Maintain</option>
                  <option value="admin">Admin</option>
                </Select>
                <Button
                  disabled={remove.isPending}
                  onClick={() =>
                    void remove
                      .mutateAsync({
                        path: {
                          organization: slug,
                          repository: repositoryID,
                          collaborator: collaborator.did,
                        },
                      })
                      .then(refresh)
                  }
                  size="sm"
                  variant="ghost"
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
          {collaborators.hasNextPage ? (
            <LoadMoreButton
              isFetching={collaborators.isFetchingNextPage}
              label="collaborators"
              onClick={() => collaborators.fetchNextPage()}
            />
          ) : null}
          <form
            className="mt-4 grid gap-2 sm:grid-cols-[1fr_9rem_auto]"
            onSubmit={(event) => {
              event.preventDefault()
              void form.handleSubmit()
            }}
          >
            <form.Field name="did">
              {(field) => (
                <Input
                  aria-label={`Collaborator DID for ${repositorySlug}`}
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder="did:plc:…"
                  required
                  value={field.state.value}
                />
              )}
            </form.Field>
            <form.Field name="role">
              {(field) => (
                <Select
                  aria-label={`New collaborator role for ${repositorySlug}`}
                  name={field.name}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(repositoryRole(event.target.value))}
                  value={field.state.value}
                >
                  <option value="read">Read</option>
                  <option value="triage">Triage</option>
                  <option value="write">Write</option>
                  <option value="maintain">Maintain</option>
                  <option value="admin">Admin</option>
                </Select>
              )}
            </form.Field>
            <Button disabled={put.isPending} size="sm" type="submit">
              Add collaborator
            </Button>
          </form>
        </div>
      ) : null}
    </article>
  )
}

function auditActionLabel(action: string) {
  return action.replaceAll('.', ' ').replaceAll('_', ' ')
}

function LoadMoreButton<TResult>({
  isFetching,
  label,
  onClick,
}: {
  isFetching: boolean
  label: string
  onClick: () => Promise<TResult>
}) {
  return (
    <div className="mt-4 flex justify-center">
      <Button disabled={isFetching} onClick={() => void onClick()} size="sm" variant="outline">
        {isFetching ? 'Loading…' : `Load more ${label}`}
      </Button>
    </div>
  )
}

function MembersSection<TPageResult>({
  creatorDID,
  fetchNextPage,
  hasNextPage,
  identity,
  isFetchingNextPage,
  members,
  owner,
  slug,
}: {
  creatorDID: string
  fetchNextPage: () => Promise<TPageResult>
  hasNextPage: boolean
  identity?: CurrentIdentity | null
  isFetchingNextPage: boolean
  members: OrganizationMember[]
  owner: boolean
  slug: string
}) {
  const [inviting, setInviting] = useState(false)
  return (
    <section aria-labelledby="people-heading">
      <div className="flex flex-wrap items-end justify-between gap-3 border-b pb-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">People</p>
          <h2 className="mt-1 font-serif text-3xl" id="people-heading">
            Members
          </h2>
        </div>
        {owner ? (
          <Button onClick={() => setInviting((value) => !value)} size="sm">
            <UserPlus className="size-4" /> Invite member
          </Button>
        ) : null}
      </div>
      {inviting ? <InviteForm onDone={() => setInviting(false)} slug={slug} /> : null}
      <ul className="divide-y">
        {members.map((member) => (
          <MemberRow
            creator={member.did === creatorDID}
            identity={identity}
            key={member.did}
            member={member}
            owner={owner}
            slug={slug}
          />
        ))}
      </ul>
      {hasNextPage ? (
        <LoadMoreButton isFetching={isFetchingNextPage} label="members" onClick={fetchNextPage} />
      ) : null}
    </section>
  )
}

function InviteForm({ onDone, slug }: { onDone: () => void; slug: string }) {
  const queryClient = useQueryClient()
  const mutation = useMutation(inviteOrganizationMemberMutationOptions())
  const form = useForm({
    defaultValues: { did: '', role: organizationMemberRole('member') },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        path: { organization: slug },
        body: { did: value.did.trim(), role: value.role },
      })
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: organizationMembersQueryOptions(slug).queryKey }),
        queryClient.invalidateQueries({
          queryKey: organizationOwnerInvitationsQueryOptions(slug).queryKey,
        }),
      ])
      onDone()
    },
  })
  return (
    <form
      className="mt-5 grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-[1fr_10rem_auto] sm:items-end"
      onSubmit={(event) => {
        event.preventDefault()
        void form.handleSubmit()
      }}
    >
      <form.Field name="did">
        {(field) => (
          <Field hint="Use the account’s canonical DID." htmlFor="invite-did" label="Account DID">
            <Input
              id="invite-did"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
              placeholder="did:plc:…"
              required
              value={field.state.value}
            />
          </Field>
        )}
      </form.Field>
      <form.Field name="role">
        {(field) => (
          <Field htmlFor="invite-role" label="Role">
            <Select
              id="invite-role"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) =>
                field.handleChange(event.target.value === 'owner' ? 'owner' : 'member')
              }
              value={field.state.value}
            >
              <option value="member">Member</option>
              <option value="owner">Owner</option>
            </Select>
          </Field>
        )}
      </form.Field>
      <Button disabled={mutation.isPending} type="submit">
        {mutation.isPending ? 'Inviting…' : 'Send invite'}
      </Button>
      {mutation.isError ? (
        <Alert className="sm:col-span-3">
          <AlertTitle>Invitation not sent</AlertTitle>
          <AlertDescription>
            {apiErrorMessage(mutation.error, 'The server rejected this invitation.')}
          </AlertDescription>
        </Alert>
      ) : null}
    </form>
  )
}

function MemberRow({
  creator,
  identity,
  member,
  owner,
  slug,
}: {
  creator: boolean
  identity?: CurrentIdentity | null
  member: OrganizationMember
  owner: boolean
  slug: string
}) {
  const queryClient = useQueryClient()
  const update = useMutation(updateOrganizationMemberMutationOptions())
  const remove = useMutation(removeOrganizationMemberMutationOptions())
  const self = identity?.did === member.did
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: organizationMembersQueryOptions(slug).queryKey })
  return (
    <li className="flex flex-col gap-4 py-5 sm:flex-row sm:items-center">
      <span className="grid size-10 shrink-0 place-items-center rounded-full bg-muted font-semibold">
        {(member.handle ?? member.did).slice(0, 1).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate font-medium">{member.handle ? `@${member.handle}` : member.did}</p>
        {member.handle ? (
          <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{member.did}</p>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {owner && !creator ? (
          <Select
            aria-label={`Role for ${member.handle ?? member.did}`}
            className="h-9 w-28"
            disabled={update.isPending}
            onChange={(event) =>
              void update
                .mutateAsync({
                  path: { organization: slug, member: member.did },
                  body: { role: event.target.value === 'owner' ? 'owner' : 'member' },
                })
                .then(refresh)
            }
            value={member.role}
          >
            <option value="member">Member</option>
            <option value="owner">Owner</option>
          </Select>
        ) : (
          <Badge variant="outline">{creator ? 'founding owner' : member.role}</Badge>
        )}
        {self ? (
          <Button
            disabled={update.isPending}
            onClick={() => {
              if (
                member.visibility === 'public' &&
                !window.confirm(
                  'Hide this membership? The current public AT record will be removed and its grant revoked, but historical copies may remain on the network.',
                )
              )
                return
              void update
                .mutateAsync({
                  path: { organization: slug, member: member.did },
                  body: { visibility: member.visibility === 'public' ? 'private' : 'public' },
                })
                .then(refresh)
            }}
            size="sm"
            variant="outline"
          >
            {member.visibility === 'public' ? 'Make private' : 'Make public'}
          </Button>
        ) : member.visibility === 'private' ? (
          <Badge variant="secondary">
            <LockKeyhole className="mr-1 size-3" />
            Private
          </Badge>
        ) : null}
        {(owner || self) && !creator ? (
          <Button
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(self ? 'Leave this organization?' : 'Remove this member?'))
                void remove
                  .mutateAsync({ path: { organization: slug, member: member.did } })
                  .then(refresh)
            }}
            size="sm"
            variant="ghost"
          >
            {self ? 'Leave' : 'Remove'}
          </Button>
        ) : null}
      </div>
    </li>
  )
}

function TeamsSection({
  members,
  owner,
  slug,
}: {
  members: OrganizationMember[]
  owner: boolean
  slug: string
}) {
  const query = useInfiniteQuery(organizationTeamsInfiniteQueryOptions(slug))
  const teams = (query.data?.pages ?? []).flatMap((page) => page.items)
  const [creating, setCreating] = useState(false)
  return (
    <section aria-labelledby="teams-heading">
      <div className="flex items-end justify-between gap-3 border-b pb-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">
            Access groups
          </p>
          <h2 className="mt-1 font-serif text-3xl" id="teams-heading">
            Teams
          </h2>
        </div>
        {owner ? (
          <Button onClick={() => setCreating((value) => !value)} size="sm" variant="outline">
            <PlusIcon /> New team
          </Button>
        ) : null}
      </div>
      {creating ? (
        <CreateTeamForm onDone={() => setCreating(false)} slug={slug} teams={teams} />
      ) : null}
      {teams.length === 0 ? (
        <div className="mt-5 rounded-xl border border-dashed p-7">
          <h3 className="font-serif text-xl">Group repository access</h3>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
            Teams collect organization members and apply shared repository permissions. Visible
            teams can be discovered by every member; secret teams are limited to owners and their
            members.
          </p>
        </div>
      ) : (
        <div className="mt-5 grid gap-4">
          {teams.map((team) => (
            <TeamCard
              key={team.id}
              members={members}
              owner={owner}
              slug={slug}
              team={team}
              teams={teams}
            />
          ))}
        </div>
      )}
      {query.hasNextPage ? (
        <LoadMoreButton
          isFetching={query.isFetchingNextPage}
          label="teams"
          onClick={() => query.fetchNextPage()}
        />
      ) : null}
    </section>
  )
}

function PlusIcon() {
  return (
    <span aria-hidden="true" className="text-base leading-none">
      +
    </span>
  )
}

function CreateTeamForm({
  onDone,
  slug,
  teams,
}: {
  onDone: () => void
  slug: string
  teams: OrganizationTeam[]
}) {
  const queryClient = useQueryClient()
  const mutation = useMutation(createOrganizationTeamMutationOptions())
  const form = useForm({
    defaultValues: {
      name: '',
      slug: '',
      visibility: teamVisibility('visible'),
      parentTeamID: '',
    },
    onSubmit: async ({ value }) => {
      await mutation.mutateAsync({
        path: { organization: slug },
        body: {
          name: value.name.trim(),
          slug: value.slug.trim(),
          visibility: value.visibility,
          parent_team_id: value.parentTeamID || undefined,
        },
      })
      await queryClient.invalidateQueries({
        queryKey: organizationTeamsQueryOptions(slug).queryKey,
      })
      onDone()
    },
  })
  return (
    <form
      className="mt-5 grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-3 sm:items-end"
      onSubmit={(event) => {
        event.preventDefault()
        void form.handleSubmit()
      }}
    >
      <form.Field name="name">
        {(field) => (
          <Field htmlFor="team-name" label="Team name">
            <Input
              id="team-name"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) => {
                field.handleChange(event.target.value)
                if (!form.getFieldValue('slug'))
                  form.setFieldValue(
                    'slug',
                    event.target.value
                      .toLowerCase()
                      .replace(/[^a-z0-9]+/g, '-')
                      .replace(/^-|-$/g, ''),
                  )
              }}
              required
              value={field.state.value}
            />
          </Field>
        )}
      </form.Field>
      <form.Field name="slug">
        {(field) => (
          <Field htmlFor="team-slug" label="URL name">
            <Input
              id="team-slug"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
              pattern="[a-z0-9][a-z0-9-]*"
              required
              value={field.state.value}
            />
          </Field>
        )}
      </form.Field>
      <form.Field name="visibility">
        {(field) => (
          <Field htmlFor="team-visibility" label="Visibility">
            <Select
              id="team-visibility"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) =>
                field.handleChange(event.target.value === 'secret' ? 'secret' : 'visible')
              }
              value={field.state.value}
            >
              <option value="visible">Visible</option>
              <option value="secret">Secret</option>
            </Select>
          </Field>
        )}
      </form.Field>
      <form.Field name="parentTeamID">
        {(field) => (
          <Field
            hint="Child teams inherit repository access from their parent."
            htmlFor="team-parent"
            label="Parent team"
          >
            <Select
              id="team-parent"
              name={field.name}
              onBlur={field.handleBlur}
              onChange={(event) => field.handleChange(event.target.value)}
              value={field.state.value}
            >
              <option value="">No parent</option>
              {teams.map((team) => (
                <option key={team.id} value={team.id}>
                  {team.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
      </form.Field>
      <div className="flex gap-2 sm:col-span-3">
        <Button disabled={mutation.isPending} type="submit">
          {mutation.isPending ? 'Creating…' : 'Create team'}
        </Button>
        <Button onClick={onDone} type="button" variant="ghost">
          Cancel
        </Button>
      </div>
    </form>
  )
}

function TeamCard({
  members,
  owner,
  slug,
  team,
  teams,
}: {
  members: OrganizationMember[]
  owner: boolean
  slug: string
  team: OrganizationTeam
  teams: OrganizationTeam[]
}) {
  const queryClient = useQueryClient()
  const canManageMembers = owner || team.viewer_role === 'maintainer'
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const teamMembers = useInfiniteQuery({
    ...organizationTeamMembersInfiniteQueryOptions(slug, team.id),
    enabled: open,
  })
  const teamMemberItems = teamMembers.data?.pages.flatMap((page) => page.items) ?? []
  const putMember = useMutation(putOrganizationTeamMemberMutationOptions())
  const removeMember = useMutation(removeOrganizationTeamMemberMutationOptions())
  const repositories = useInfiniteQuery({
    ...organizationRepositoriesInfiniteQueryOptions(slug),
    enabled: open,
  })
  const repositoryItems = repositories.data?.pages.flatMap((page) => page.items) ?? []
  const teamRepositories = useInfiniteQuery({
    ...organizationTeamRepositoriesInfiniteQueryOptions(slug, team.id),
    enabled: open,
  })
  const teamRepositoryItems = teamRepositories.data?.pages.flatMap((page) => page.items) ?? []
  const putRepository = useMutation(putOrganizationTeamRepositoryMutationOptions())
  const removeRepository = useMutation(removeOrganizationTeamRepositoryMutationOptions())
  const updateTeam = useMutation(updateOrganizationTeamMutationOptions())
  const deleteTeam = useMutation(deleteOrganizationTeamMutationOptions())
  const refresh = () =>
    queryClient.invalidateQueries({
      queryKey: organizationTeamMembersQueryOptions(slug, team.id).queryKey,
    })
  const refreshRepositories = () =>
    queryClient.invalidateQueries({
      queryKey: organizationTeamRepositoriesQueryOptions(slug, team.id).queryKey,
    })
  const memberForm = useForm({
    defaultValues: { did: '' },
    onSubmit: async ({ value, formApi }) => {
      if (!value.did) return
      await putMember.mutateAsync({
        path: { organization: slug, team: team.id, member: value.did },
        body: { role: 'member' },
      })
      formApi.reset()
      await refresh()
    },
  })
  const repositoryForm = useForm({
    defaultValues: {
      repository: '',
      role: repositoryRole('read'),
    },
    onSubmit: async ({ value, formApi }) => {
      if (!value.repository) return
      await putRepository.mutateAsync({
        path: { organization: slug, team: team.id, repository: value.repository },
        body: { role: value.role },
      })
      formApi.reset()
      await refreshRepositories()
    },
  })
  const settingsForm = useForm({
    defaultValues: {
      name: team.name,
      description: team.description ?? '',
      visibility: team.visibility,
      parentTeamID: team.parent_team_id ?? '',
    },
    onSubmit: async ({ value }) => {
      await updateTeam.mutateAsync({
        path: { organization: slug, team: team.id },
        body: {
          name: value.name.trim(),
          description: value.description.trim() || undefined,
          visibility: value.visibility,
          parent_team_id: value.parentTeamID || null,
        },
      })
      await queryClient.invalidateQueries({
        queryKey: organizationTeamsQueryOptions(slug).queryKey,
      })
      setEditing(false)
    },
  })
  const nested = Boolean(
    team.parent_team_id || teams.some((candidate) => candidate.parent_team_id === team.id),
  )
  return (
    <article className="rounded-xl border bg-card">
      <button
        className="flex w-full items-center justify-between gap-4 p-5 text-left"
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        <div>
          <h3 className="font-serif text-xl">{team.name}</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            @{team.slug} · {team.visibility}
            {team.parent_team_id
              ? ` · child of ${teams.find((candidate) => candidate.id === team.parent_team_id)?.name ?? 'another team'}`
              : ''}
          </p>
        </div>
        <Badge variant="outline">{open ? 'Hide' : 'Manage'}</Badge>
      </button>
      {open ? (
        <div className="border-t p-5">
          {canManageMembers ? (
            <div className="mb-5 border-b pb-5">
              <div className="flex flex-wrap items-center justify-between gap-3">
                <h4 className="font-medium">Team settings</h4>
                <Button onClick={() => setEditing((value) => !value)} size="sm" variant="ghost">
                  {editing ? 'Cancel' : 'Edit'}
                </Button>
              </div>
              {editing ? (
                <form
                  className="mt-4 grid gap-3 sm:grid-cols-2"
                  onSubmit={(event) => {
                    event.preventDefault()
                    void settingsForm.handleSubmit()
                  }}
                >
                  <settingsForm.Field name="name">
                    {(field) => (
                      <Field htmlFor={`team-name-${team.id}`} label="Name">
                        <Input
                          id={`team-name-${team.id}`}
                          maxLength={255}
                          name={field.name}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                          required
                          value={field.state.value}
                        />
                      </Field>
                    )}
                  </settingsForm.Field>
                  <settingsForm.Field name="visibility">
                    {(field) => (
                      <Field
                        hint={nested ? 'Nested teams must remain visible.' : undefined}
                        htmlFor={`team-visibility-${team.id}`}
                        label="Visibility"
                      >
                        <Select
                          id={`team-visibility-${team.id}`}
                          name={field.name}
                          onBlur={field.handleBlur}
                          onChange={(event) => {
                            const visibility =
                              event.target.value === 'secret' ? 'secret' : 'visible'
                            field.handleChange(visibility)
                            if (visibility === 'secret')
                              settingsForm.setFieldValue('parentTeamID', '')
                          }}
                          value={field.state.value}
                        >
                          <option value="visible">Visible</option>
                          <option disabled={nested} value="secret">
                            Secret
                          </option>
                        </Select>
                      </Field>
                    )}
                  </settingsForm.Field>
                  <settingsForm.Field name="description">
                    {(field) => (
                      <Field htmlFor={`team-description-${team.id}`} label="Description">
                        <Input
                          id={`team-description-${team.id}`}
                          maxLength={2000}
                          name={field.name}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                          value={field.state.value}
                        />
                      </Field>
                    )}
                  </settingsForm.Field>
                  <settingsForm.Field name="parentTeamID">
                    {(field) => (
                      <Field htmlFor={`team-parent-${team.id}`} label="Parent team">
                        <Select
                          disabled={settingsForm.getFieldValue('visibility') === 'secret'}
                          id={`team-parent-${team.id}`}
                          name={field.name}
                          onBlur={field.handleBlur}
                          onChange={(event) => field.handleChange(event.target.value)}
                          value={field.state.value}
                        >
                          <option value="">No parent</option>
                          {teams
                            .filter(
                              (candidate) =>
                                candidate.id !== team.id && candidate.visibility === 'visible',
                            )
                            .map((candidate) => (
                              <option key={candidate.id} value={candidate.id}>
                                {candidate.name}
                              </option>
                            ))}
                        </Select>
                      </Field>
                    )}
                  </settingsForm.Field>
                  <div className="flex flex-wrap gap-2 sm:col-span-2">
                    <Button disabled={updateTeam.isPending} size="sm" type="submit">
                      {updateTeam.isPending ? 'Saving…' : 'Save team'}
                    </Button>
                    <Button
                      className="text-destructive hover:text-destructive"
                      disabled={deleteTeam.isPending}
                      onClick={() => {
                        const childCount = teams.filter(
                          (candidate) => candidate.parent_team_id === team.id,
                        ).length
                        const warning = childCount
                          ? `Delete ${team.name} and its ${childCount} child team${childCount === 1 ? '' : 's'}?`
                          : `Delete ${team.name}?`
                        if (window.confirm(warning))
                          void deleteTeam
                            .mutateAsync({ path: { organization: slug, team: team.id } })
                            .then(() =>
                              queryClient.invalidateQueries({
                                queryKey: organizationTeamsQueryOptions(slug).queryKey,
                              }),
                            )
                      }}
                      size="sm"
                      type="button"
                      variant="outline"
                    >
                      {deleteTeam.isPending ? 'Deleting…' : 'Delete team'}
                    </Button>
                  </div>
                </form>
              ) : null}
            </div>
          ) : null}
          <ul className="space-y-2">
            {teamMemberItems.map((member) => (
              <li className="flex items-center justify-between gap-3 text-sm" key={member.did}>
                <span>{member.handle ? `@${member.handle}` : member.did}</span>
                {canManageMembers ? (
                  <div className="flex items-center gap-2">
                    <Select
                      aria-label={`Team role for ${member.handle ?? member.did}`}
                      className="h-8 w-32"
                      disabled={putMember.isPending}
                      onChange={(event) =>
                        void putMember
                          .mutateAsync({
                            path: {
                              organization: slug,
                              team: team.id,
                              member: member.did,
                            },
                            body: {
                              role: event.target.value === 'maintainer' ? 'maintainer' : 'member',
                            },
                          })
                          .then(refresh)
                      }
                      value={member.role}
                    >
                      <option value="member">Member</option>
                      <option value="maintainer">Maintainer</option>
                    </Select>
                    <Button
                      disabled={removeMember.isPending}
                      onClick={() =>
                        void removeMember
                          .mutateAsync({
                            path: {
                              organization: slug,
                              team: team.id,
                              member: member.did,
                            },
                          })
                          .then(refresh)
                      }
                      size="sm"
                      variant="ghost"
                    >
                      Remove
                    </Button>
                  </div>
                ) : (
                  <Badge variant="secondary">{member.role}</Badge>
                )}
              </li>
            ))}
          </ul>
          {teamMembers.hasNextPage ? (
            <LoadMoreButton
              isFetching={teamMembers.isFetchingNextPage}
              label="team members"
              onClick={() => teamMembers.fetchNextPage()}
            />
          ) : null}
          {canManageMembers ? (
            <form
              className="mt-4 flex flex-wrap gap-2"
              onSubmit={(event) => {
                event.preventDefault()
                void memberForm.handleSubmit()
              }}
            >
              <memberForm.Field name="did">
                {(field) => (
                  <Select
                    aria-label={`Add member to ${team.name}`}
                    className="min-w-56 flex-1"
                    name={field.name}
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    value={field.state.value}
                  >
                    <option value="">Choose an organization member</option>
                    {members
                      .filter(
                        (candidate) =>
                          !teamMemberItems.some((member) => member.did === candidate.did),
                      )
                      .map((candidate) => (
                        <option key={candidate.did} value={candidate.did}>
                          {candidate.handle ?? candidate.did}
                        </option>
                      ))}
                  </Select>
                )}
              </memberForm.Field>
              <Button disabled={putMember.isPending} size="sm" type="submit">
                Add member
              </Button>
            </form>
          ) : null}
          <div className="mt-6 border-t pt-5">
            <h4 className="font-medium">Repository access</h4>
            <ul className="mt-3 space-y-2">
              {teamRepositoryItems.map((assignment) => (
                <li className="flex items-center gap-3 text-sm" key={assignment.repository_id}>
                  <span className="min-w-0 flex-1 truncate">{assignment.repository_slug}</span>
                  <Badge variant="outline">{assignment.role}</Badge>
                  {canManageMembers ? (
                    <Button
                      disabled={removeRepository.isPending}
                      onClick={() =>
                        void removeRepository
                          .mutateAsync({
                            path: {
                              organization: slug,
                              team: team.id,
                              repository: assignment.repository_id,
                            },
                          })
                          .then(refreshRepositories)
                      }
                      size="sm"
                      variant="ghost"
                    >
                      Remove
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
            {teamRepositories.hasNextPage ? (
              <LoadMoreButton
                isFetching={teamRepositories.isFetchingNextPage}
                label="team repositories"
                onClick={() => teamRepositories.fetchNextPage()}
              />
            ) : null}
            {canManageMembers ? (
              <form
                className="mt-4 grid gap-2 sm:grid-cols-[1fr_9rem_auto]"
                onSubmit={(event) => {
                  event.preventDefault()
                  void repositoryForm.handleSubmit()
                }}
              >
                <repositoryForm.Field name="repository">
                  {(field) => (
                    <Select
                      aria-label={`Repository for ${team.name}`}
                      name={field.name}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(event.target.value)}
                      value={field.state.value}
                    >
                      <option value="">Choose repository</option>
                      {repositoryItems
                        .filter(
                          (repository) =>
                            repository.id &&
                            !teamRepositoryItems.some(
                              (assignment) => assignment.repository_id === repository.id,
                            ),
                        )
                        .map((repository) => (
                          <option key={repository.id} value={repository.id ?? undefined}>
                            {repository.slug}
                          </option>
                        ))}
                    </Select>
                  )}
                </repositoryForm.Field>
                <repositoryForm.Field name="role">
                  {(field) => (
                    <Select
                      aria-label={`Repository role for ${team.name}`}
                      name={field.name}
                      onBlur={field.handleBlur}
                      onChange={(event) => field.handleChange(repositoryRole(event.target.value))}
                      value={field.state.value}
                    >
                      <option value="read">Read</option>
                      <option value="triage">Triage</option>
                      <option value="write">Write</option>
                      <option value="maintain">Maintain</option>
                      <option value="admin">Admin</option>
                    </Select>
                  )}
                </repositoryForm.Field>
                <Button disabled={putRepository.isPending} size="sm" type="submit">
                  Assign
                </Button>
              </form>
            ) : null}
            {canManageMembers && repositories.hasNextPage ? (
              <LoadMoreButton
                isFetching={repositories.isFetchingNextPage}
                label="available repositories"
                onClick={() => repositories.fetchNextPage()}
              />
            ) : null}
          </div>
        </div>
      ) : null}
    </article>
  )
}
