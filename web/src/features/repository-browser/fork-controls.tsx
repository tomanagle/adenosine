import type { Repository } from '@adenosine/api-client'
import {
  zCreateRepositoryForkRequest,
  zOrganizationSlug,
  zRepositorySlug,
} from '@adenosine/api-client/schemas'
import { z } from 'zod'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { CircleCheck, GitFork, RefreshCw, X } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button, buttonVariants } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input, Select } from '@/components/ui/input'
import { organizationsQueryOptions } from '@/features/organizations/queries'
import {
  repositorySnapshotQueryOptions,
  retainCreatedRepository,
} from '@/features/repositories/repository-snapshot.query'
import { apiErrorMessage } from '@/lib/api-error'
import { fieldErrorMessage } from '@/lib/form'
import { cn } from '@/lib/utils'

import {
  branchesQueryOptions,
  createRepositoryForkMutationOptions,
  repositoryForksQueryOptions,
  syncRepositoryForkMutationOptions,
} from './queries'
import type { RepositoryRouteParams } from './queries'

const forkFormSchema = zCreateRepositoryForkRequest.extend({
  slug: zRepositorySlug,
  organization: z.union([z.literal(''), zOrganizationSlug]),
})

export function ForkActions({
  identityDid,
  params,
  repository,
}: {
  identityDid?: string
  params: RepositoryRouteParams
  repository: Repository
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const organizations = useQuery({
    ...organizationsQueryOptions(),
    enabled: Boolean(identityDid),
  })
  const createFork = useMutation(createRepositoryForkMutationOptions())
  const syncFork = useMutation(syncRepositoryForkMutationOptions())
  const snapshotQuery = repositorySnapshotQueryOptions()
  const forksQuery = repositoryForksQueryOptions(params)
  const canSyncFork = Boolean(
    identityDid &&
    repository.hosting.local &&
    repository.forked_from &&
    ((repository.owner.kind === 'account' && repository.owner.did === identityDid) ||
      repository.viewer_can_admin),
  )

  const form = useForm({
    defaultValues: { slug: repository.slug, organization: '' },
    validators: { onChange: forkFormSchema, onSubmit: forkFormSchema },
    onSubmit: async ({ value }) => {
      const created = await createFork.mutateAsync({
        body: {
          slug: value.slug.trim(),
          organization: value.organization || undefined,
        },
        path: params,
      })
      await queryClient.invalidateQueries({ queryKey: forksQuery.queryKey })
      await queryClient.invalidateQueries({ queryKey: snapshotQuery.queryKey })
      queryClient.setQueryData(snapshotQuery.queryKey, (snapshot) =>
        retainCreatedRepository(snapshot, created),
      )
      const owner = created.owner.organization_slug ?? created.owner.handle ?? created.owner.did
      await navigate({ to: '/$owner/$repo', params: { owner, repo: created.slug } })
    },
  })

  async function sync() {
    await syncFork.mutateAsync({ path: params })
    await queryClient.invalidateQueries({ queryKey: branchesQueryOptions(params).queryKey })
  }

  return (
    <div className="flex flex-col items-start gap-2 sm:items-end">
      <div className="flex flex-wrap gap-2">
        {!identityDid ? (
          <Link className={cn(buttonVariants({ size: 'sm', variant: 'outline' }))} to="/login">
            <GitFork aria-hidden="true" className="size-3.5" />
            Sign in to fork
            <span className="tabular-nums">{repository.fork_count}</span>
          </Link>
        ) : (
          <details className="group relative">
            <summary
              className={cn(
                buttonVariants({ size: 'sm', variant: 'outline' }),
                'cursor-pointer list-none',
              )}
            >
              <GitFork aria-hidden="true" className="size-3.5" /> Fork
              <span className="tabular-nums">{repository.fork_count}</span>
            </summary>
            <div className="absolute right-0 z-30 mt-2 w-[min(26rem,calc(100vw-2.5rem))] rounded-xl border bg-card p-5 text-left shadow-xl">
              <div className="flex items-start justify-between gap-4">
                <div>
                  <h2 className="font-serif text-xl">Create your fork</h2>
                  <p className="mt-1 text-xs leading-5 text-muted-foreground">
                    Copies public branches and tags here and records portable upstream ancestry.
                  </p>
                </div>
                <Button
                  aria-label="Close fork form"
                  onClick={(event) => {
                    const details = event.currentTarget.closest('details')
                    if (details) details.open = false
                  }}
                  className="size-8 px-0"
                  size="sm"
                  type="button"
                  variant="ghost"
                >
                  <X className="size-4" />
                </Button>
              </div>
              <form
                className="mt-4 grid gap-4"
                onSubmit={(event) => {
                  event.preventDefault()
                  void form.handleSubmit()
                }}
              >
                <form.Field name="slug">
                  {(field) => (
                    <Field
                      error={fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)}
                      htmlFor="fork-slug"
                      label="Repository name"
                    >
                      <Input
                        id="fork-slug"
                        maxLength={100}
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value.trim())}
                        spellCheck={false}
                        value={field.state.value}
                      />
                    </Field>
                  )}
                </form.Field>
                <form.Field name="organization">
                  {(field) => (
                    <Field
                      hint="Keep it under your account or choose a shared organization."
                      htmlFor="fork-owner"
                      label="Owner"
                    >
                      <Select
                        id="fork-owner"
                        onBlur={field.handleBlur}
                        onChange={(event) => field.handleChange(event.target.value)}
                        value={field.state.value}
                      >
                        <option value="">Your account</option>
                        {organizations.data?.items.map((organization) => (
                          <option key={organization.id} value={organization.slug}>
                            {organization.name}
                          </option>
                        ))}
                      </Select>
                    </Field>
                  )}
                </form.Field>
                {createFork.isError ? (
                  <Alert>
                    <AlertTitle>Fork not created</AlertTitle>
                    <AlertDescription>
                      {apiErrorMessage(createFork.error, 'The repository could not be forked.')}
                    </AlertDescription>
                  </Alert>
                ) : null}
                <form.Subscribe selector={(state) => state.isSubmitting}>
                  {(isSubmitting) => (
                    <Button disabled={isSubmitting} type="submit">
                      <GitFork className="size-4" />
                      {isSubmitting ? 'Copying repository…' : 'Create fork'}
                    </Button>
                  )}
                </form.Subscribe>
              </form>
            </div>
          </details>
        )}
        {canSyncFork ? (
          <Button
            aria-busy={syncFork.isPending}
            disabled={syncFork.isPending}
            onClick={() => void sync()}
            size="sm"
            variant="outline"
          >
            {syncFork.data && !syncFork.data.updated ? (
              <CircleCheck className="size-3.5" />
            ) : (
              <RefreshCw className={cn('size-3.5', syncFork.isPending && 'animate-spin')} />
            )}
            {syncFork.isPending
              ? 'Syncing…'
              : syncFork.data?.updated
                ? 'Fork updated'
                : syncFork.data
                  ? 'Up to date'
                  : 'Sync fork'}
          </Button>
        ) : null}
      </div>
      {syncFork.isError ? (
        <p className="max-w-sm text-xs text-danger" role="alert">
          {apiErrorMessage(
            syncFork.error,
            'The fork could not be fast-forwarded. It may have diverged from upstream.',
          )}
        </p>
      ) : null}
    </div>
  )
}

export function ForkNetwork({
  params,
  repository,
}: {
  params: RepositoryRouteParams
  repository: Repository
}) {
  const forks = useQuery({
    ...repositoryForksQueryOptions(params),
    enabled: Boolean(repository.uri && repository.fork_count),
  })

  return (
    <div>
      <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        Fork network
      </h2>
      {repository.forked_from ? (
        <p className="mt-2 text-xs leading-5 text-muted-foreground">
          Forked from{' '}
          <code className="break-all text-foreground" title={repository.forked_from.uri}>
            {shortRecord(repository.forked_from.uri)}
          </code>
        </p>
      ) : (
        <p className="mt-2 text-xs text-muted-foreground">Original repository</p>
      )}
      <p className="mt-2 text-sm">
        {repository.fork_count} direct {repository.fork_count === 1 ? 'fork' : 'forks'}
      </p>
      {forks.data?.items.length ? (
        <ul className="mt-2 space-y-1" aria-label="Repository forks">
          {forks.data.items.slice(0, 5).map((fork) => {
            const owner = fork.owner.organization_slug ?? fork.owner.handle ?? fork.owner.did
            return (
              <li key={fork.uri ?? `${owner}/${fork.slug}`}>
                <Link
                  className="block truncate rounded px-1 py-1 font-mono text-xs hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  params={{ owner, repo: fork.slug }}
                  to="/$owner/$repo"
                >
                  {owner}/{fork.slug}
                </Link>
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}

function shortRecord(uri: string) {
  const parts = uri.split('/')
  if (parts.length < 5) return uri
  return `${parts[2]}/${parts.at(-1)?.slice(0, 8)}…`
}
