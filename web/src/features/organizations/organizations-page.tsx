import type { Organization } from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Building2, Check, Clock3, Plus, Users } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'

import {
  acceptOrganizationInvitationMutationOptions,
  createOrganizationMutationOptions,
  organizationInvitationsQueryOptions,
  organizationInvitationsInfiniteQueryOptions,
  organizationsQueryOptions,
  organizationsInfiniteQueryOptions,
} from './queries'

export function OrganizationsPage() {
  const organizationsQuery = useInfiniteQuery(organizationsInfiniteQueryOptions())
  const invitationsQuery = useInfiniteQuery(organizationInvitationsInfiniteQueryOptions())
  const organizations = (organizationsQuery.data?.pages ?? []).flatMap((page) => page.items)
  const invitations = (invitationsQuery.data?.pages ?? []).flatMap((page) => page.items)
  const [creating, setCreating] = useState(false)

  return (
    <main className="mx-auto max-w-6xl px-5 py-9 sm:px-8 sm:py-12">
      <div className="flex flex-col gap-5 border-b pb-7 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.18em] text-primary">
            Shared work
          </p>
          <h1 className="mt-2 font-serif text-4xl tracking-tight">Organizations</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
            Bring people, repositories, and permissions together under a portable network identity.
          </p>
        </div>
        <Button onClick={() => setCreating((value) => !value)}>
          <Plus aria-hidden="true" className="size-4" /> New organization
        </Button>
      </div>

      {creating ? <CreateOrganizationForm onDone={() => setCreating(false)} /> : null}
      {invitations.length > 0 ? (
        <InvitationList
          fetchNextPage={() => invitationsQuery.fetchNextPage()}
          hasNextPage={invitationsQuery.hasNextPage}
          invitations={invitations}
          isFetchingNextPage={invitationsQuery.isFetchingNextPage}
        />
      ) : null}

      <section aria-labelledby="organization-list-title" className="mt-9">
        <div className="flex items-baseline justify-between gap-4">
          <h2 className="font-serif text-2xl" id="organization-list-title">
            Your organizations
          </h2>
          <span className="text-sm tabular-nums text-muted-foreground">
            {organizations.length}
            {organizationsQuery.hasNextPage ? '+' : ''}
          </span>
        </div>
        {organizations.length === 0 ? (
          <div className="mt-4 rounded-xl border border-dashed px-6 py-12 text-center">
            <Building2 aria-hidden="true" className="mx-auto size-8 text-muted-foreground" />
            <h3 className="mt-4 font-serif text-2xl">No organizations yet</h3>
            <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-muted-foreground">
              Create one for a company, open-source project, or any group that owns code together.
            </p>
          </div>
        ) : (
          <ul className="mt-4 grid gap-4 sm:grid-cols-2">
            {organizations.map((organization) => (
              <OrganizationCard key={organization.id} organization={organization} />
            ))}
          </ul>
        )}
        {organizationsQuery.hasNextPage ? (
          <div className="mt-5 flex justify-center">
            <Button
              disabled={organizationsQuery.isFetchingNextPage}
              onClick={() => void organizationsQuery.fetchNextPage()}
              variant="outline"
            >
              {organizationsQuery.isFetchingNextPage ? 'Loading…' : 'Load more organizations'}
            </Button>
          </div>
        ) : null}
      </section>
    </main>
  )
}

function OrganizationCard({ organization }: { organization: Organization }) {
  return (
    <li className="group rounded-xl border bg-card p-5 transition-colors hover:border-primary/40">
      <div className="flex items-start gap-4">
        <span className="grid size-11 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
          <Building2 className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <Link
            className="font-serif text-xl underline-offset-4 group-hover:underline"
            params={{ organization: organization.slug }}
            to="/organizations/$organization"
          >
            {organization.name}
          </Link>
          <p className="mt-0.5 font-mono text-xs text-muted-foreground">@{organization.slug}</p>
          {organization.description ? (
            <p className="mt-3 line-clamp-2 text-sm leading-6 text-muted-foreground">
              {organization.description}
            </p>
          ) : null}
          <div className="mt-4 flex items-center gap-2">
            <Badge variant="outline">{organization.viewer_role}</Badge>
            <Badge variant="secondary">Base: {organization.base_permission}</Badge>
          </div>
        </div>
      </div>
    </li>
  )
}

function CreateOrganizationForm({ onDone }: { onDone: () => void }) {
  const queryClient = useQueryClient()
  const mutation = useMutation(createOrganizationMutationOptions())
  const form = useForm({
    defaultValues: { slug: '', name: '', description: '' },
    onSubmit: async ({ value }) => {
      const organization = await mutation.mutateAsync({
        body: {
          slug: value.slug.trim(),
          name: value.name.trim(),
          description: value.description.trim() || undefined,
        },
      })
      await queryClient.invalidateQueries({ queryKey: organizationsQueryOptions().queryKey })
      onDone()
      window.location.assign(`/organizations/${organization.slug}`)
    },
  })

  return (
    <section className="mt-7 rounded-xl border bg-card p-5 shadow-sm sm:p-6">
      <h2 className="font-serif text-2xl">Create an organization</h2>
      <p className="mt-1 text-sm text-muted-foreground">
        You will be its first owner. The organization profile is published to your AT Protocol
        repository.
      </p>
      <form
        className="mt-5 grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault()
          void form.handleSubmit()
        }}
      >
        <form.Field name="name">
          {(field) => (
            <Field htmlFor="organization-name" label="Display name">
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
        <form.Field name="slug">
          {(field) => (
            <Field
              hint="Lowercase letters, numbers, and dashes."
              htmlFor="organization-slug"
              label="URL name"
            >
              <Input
                id="organization-slug"
                maxLength={100}
                name={field.name}
                onBlur={field.handleBlur}
                onChange={(event) =>
                  field.handleChange(event.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))
                }
                pattern="[a-z0-9][a-z0-9-]*"
                required
                value={field.state.value}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="description">
          {(field) => (
            <Field
              className="sm:col-span-2"
              hint="Optional."
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
        {mutation.isError ? (
          <Alert className="sm:col-span-2">
            <AlertTitle>Organization not created</AlertTitle>
            <AlertDescription>
              {apiErrorMessage(mutation.error, 'The server rejected this organization.')}
            </AlertDescription>
          </Alert>
        ) : null}
        <div className="flex gap-2 sm:col-span-2">
          <Button disabled={mutation.isPending} type="submit">
            {mutation.isPending ? 'Creating…' : 'Create organization'}
          </Button>
          <Button onClick={onDone} type="button" variant="ghost">
            Cancel
          </Button>
        </div>
      </form>
    </section>
  )
}

function InvitationList({
  fetchNextPage,
  hasNextPage,
  invitations,
  isFetchingNextPage,
}: {
  fetchNextPage: () => Promise<unknown>
  hasNextPage: boolean
  invitations: Array<{
    id: string
    organization_name?: string | null
    organization_slug?: string | null
    role: string
    expires_at: string
  }>
  isFetchingNextPage: boolean
}) {
  const queryClient = useQueryClient()
  const mutation = useMutation(acceptOrganizationInvitationMutationOptions())
  return (
    <section className="mt-7 rounded-xl border border-primary/25 bg-primary/[0.035] p-5 sm:p-6">
      <h2 className="flex items-center gap-2 font-serif text-2xl">
        <Clock3 className="size-5 text-primary" /> Invitations
      </h2>
      <ul className="mt-4 divide-y">
        {invitations.map((invitation) => (
          <li
            className="flex flex-wrap items-center justify-between gap-4 py-4"
            key={invitation.id}
          >
            <div>
              <p className="font-medium">
                {invitation.organization_name ?? invitation.organization_slug ?? 'Organization'}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                Invited as {invitation.role} · expires{' '}
                {new Date(invitation.expires_at).toLocaleDateString()}
              </p>
            </div>
            <Button
              disabled={mutation.isPending}
              onClick={() =>
                void mutation
                  .mutateAsync({ path: { invitation: invitation.id } })
                  .then(async () => {
                    await Promise.all([
                      queryClient.invalidateQueries({
                        queryKey: organizationsQueryOptions().queryKey,
                      }),
                      queryClient.invalidateQueries({
                        queryKey: organizationInvitationsQueryOptions().queryKey,
                      }),
                    ])
                  })
              }
              size="sm"
            >
              <Check className="size-4" /> Accept
            </Button>
          </li>
        ))}
      </ul>
      {hasNextPage ? (
        <Button
          className="mt-3"
          disabled={isFetchingNextPage}
          onClick={() => void fetchNextPage()}
          size="sm"
          variant="outline"
        >
          {isFetchingNextPage ? 'Loading…' : 'Load more invitations'}
        </Button>
      ) : null}
      <p className="mt-2 flex items-center gap-1.5 text-xs text-muted-foreground">
        <Users className="size-3.5" /> Membership is private until you choose to make it public.
      </p>
    </section>
  )
}
