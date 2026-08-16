import { useState } from 'react'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Archive, GitBranch, Send, Settings2, ShieldCheck, Trash2, Webhook } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'

import {
  branchProtectionsQueryOptions,
  createBranchProtectionMutationOptions,
  createRepositoryWebhookMutationOptions,
  deleteBranchProtectionMutationOptions,
  deleteRepositoryMutationOptions,
  deleteRepositoryWebhookMutationOptions,
  repositoryQueryOptions,
  repositoryWebhooksQueryOptions,
  restoreRepositoryDeletionMutationOptions,
  updateRepositoryMutationOptions,
} from './queries'
import type { RepositoryRouteParams } from './queries'

function repositoryVisibility(value: string): 'public' | 'private' {
  return value === 'private' ? 'private' : 'public'
}

export function RepositorySettings({ params }: { params: RepositoryRouteParams }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  if (!repository.viewer_can_admin) {
    return (
      <Alert>
        <AlertTitle>Settings unavailable</AlertTitle>
        <AlertDescription>
          You need repository admin permission to view these settings.
        </AlertDescription>
      </Alert>
    )
  }
  return (
    <div className="space-y-8">
      <header className="border-b pb-4">
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">
          Administration
        </p>
        <h2 className="mt-1 font-serif text-3xl">Repository settings</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Metadata, delivery integrations, receive policy, and lifecycle.
        </p>
      </header>
      <GeneralSettings params={params} />
      <WebhookSettings params={params} />
      <ProtectionSettings params={params} />
      <DangerZone params={params} />
    </div>
  )
}

function GeneralSettings({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const mutation = useMutation(updateRepositoryMutationOptions())
  const form = useForm({
    defaultValues: {
      slug: repository.slug,
      displayName: repository.display_name ?? '',
      description: repository.description ?? '',
      visibility: repository.visibility,
      defaultBranch: repository.default_branch,
      archived: repository.archived,
    },
    onSubmit: async ({ value }) => {
      const updated = await mutation.mutateAsync({
        path: { owner: params.owner, repo: params.repo },
        body: {
          slug: value.slug.trim(),
          display_name: value.displayName.trim(),
          description: value.description.trim(),
          visibility: value.visibility,
          default_branch: value.defaultBranch.trim(),
          archived: value.archived,
        },
      })
      const next = {
        owner: updated.owner.organization_slug ?? updated.owner.handle ?? params.owner,
        repo: updated.slug,
      }
      queryClient.setQueryData(repositoryQueryOptions(next).queryKey, updated)
      if (next.owner !== params.owner || next.repo !== params.repo)
        await navigate({ to: '/$owner/$repo/settings', params: next, replace: true })
      else
        await queryClient.invalidateQueries({ queryKey: repositoryQueryOptions(params).queryKey })
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Settings2 className="size-4" /> General
        </CardTitle>
        <CardDescription>
          Renames keep the old URL working. Public changes are republished to your AT Protocol
          repository.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="slug">
            {(field) => (
              <Field htmlFor="repository-slug" label="Repository slug">
                <Input
                  id="repository-slug"
                  pattern="[a-z0-9][a-z0-9._-]*"
                  maxLength={100}
                  required
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="displayName">
            {(field) => (
              <Field htmlFor="repository-display-name" label="Display name">
                <Input
                  id="repository-display-name"
                  maxLength={255}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="defaultBranch">
            {(field) => (
              <Field htmlFor="repository-default-branch" label="Default branch">
                <Input
                  id="repository-default-branch"
                  maxLength={255}
                  required
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="visibility">
            {(field) => (
              <Field htmlFor="repository-visibility" label="Visibility">
                <Select
                  id="repository-visibility"
                  value={field.state.value}
                  onChange={(event) => field.handleChange(repositoryVisibility(event.target.value))}
                >
                  <option value="public">Public</option>
                  <option value="private">Private</option>
                </Select>
              </Field>
            )}
          </form.Field>
          <form.Field name="description">
            {(field) => (
              <Field className="sm:col-span-2" htmlFor="repository-description" label="Description">
                <Textarea
                  id="repository-description"
                  maxLength={2000}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="archived">
            {(field) => (
              <label
                className="flex items-start gap-3 rounded-lg border bg-muted/20 p-4 text-sm sm:col-span-2"
                htmlFor="repository-archived"
              >
                <Input
                  checked={field.state.value}
                  className="mt-0.5 size-4"
                  id="repository-archived"
                  type="checkbox"
                  onChange={(event) => field.handleChange(event.target.checked)}
                />
                <span>
                  <span className="flex items-center gap-2 font-medium">
                    <Archive className="size-4" /> Archive this repository
                  </span>
                  <span className="mt-1 block text-xs leading-5 text-muted-foreground">
                    Archived repositories remain readable, but HTTP and SSH pushes are rejected.
                  </span>
                </span>
              </label>
            )}
          </form.Field>
          <div className="flex items-center gap-3 sm:col-span-2">
            <Button disabled={mutation.isPending} type="submit">
              {mutation.isPending ? 'Saving…' : 'Save settings'}
            </Button>
            {mutation.isSuccess ? (
              <span className="text-xs text-muted-foreground">Settings saved.</span>
            ) : null}
          </div>
          {mutation.isError ? (
            <Alert className="sm:col-span-2">
              <AlertTitle>Settings not saved</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(mutation.error, 'The repository could not be updated.')}
              </AlertDescription>
            </Alert>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function WebhookSettings({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(repositoryWebhooksQueryOptions(params))
  const create = useMutation(createRepositoryWebhookMutationOptions())
  const remove = useMutation(deleteRepositoryWebhookMutationOptions())
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: repositoryWebhooksQueryOptions(params).queryKey })
  const form = useForm({
    defaultValues: {
      url: '',
      secret: '',
      push: true,
      issue: false,
      pullRequest: true,
      review: false,
    },
    onSubmit: async ({ value }) => {
      const events = [
        ...(value.push ? ['push' as const] : []),
        ...(value.issue ? ['issue' as const] : []),
        ...(value.pullRequest ? ['pull_request' as const] : []),
        ...(value.review ? ['review' as const] : []),
      ]
      await create.mutateAsync({
        path: { owner: params.owner, repo: params.repo },
        body: { url: value.url.trim(), secret: value.secret, events, enabled: true },
      })
      form.reset()
      await refresh()
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Webhook className="size-4" /> Webhooks
        </CardTitle>
        <CardDescription>
          Signed HTTPS delivery with encrypted secrets, bounded responses, and exponential retries.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {data.items.length ? (
          <ul className="divide-y rounded-lg border">
            {data.items.map((item) => (
              <li className="flex flex-wrap items-center gap-3 p-4" key={item.id}>
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">{item.url}</span>
                  <span className="mt-1 flex flex-wrap gap-1">
                    {item.events.map((event) => (
                      <Badge key={event} variant="secondary">
                        {event}
                      </Badge>
                    ))}
                  </span>
                </span>
                <Badge variant={item.enabled ? 'default' : 'outline'}>
                  {item.enabled ? 'Active' : 'Paused'}
                </Badge>
                <Button
                  disabled={remove.isPending}
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void remove
                      .mutateAsync({
                        path: { owner: params.owner, repo: params.repo, webhook: item.id },
                      })
                      .then(refresh)
                  }}
                >
                  Remove
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No webhook destinations configured.</p>
        )}
        <form
          className="grid gap-4 rounded-lg border bg-muted/15 p-4 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="url">
            {(field) => (
              <Field htmlFor="webhook-url" label="Payload URL">
                <Input
                  id="webhook-url"
                  type="url"
                  placeholder="https://example.com/hooks/adenosine"
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="secret">
            {(field) => (
              <Field htmlFor="webhook-secret" label="Signing secret">
                <Input
                  id="webhook-secret"
                  minLength={16}
                  maxLength={256}
                  type="password"
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <div className="flex flex-wrap gap-4 text-sm sm:col-span-2">
            {(['push', 'issue', 'pullRequest', 'review'] as const).map((name) => (
              <form.Field key={name} name={name}>
                {(field) => (
                  <label className="flex items-center gap-2">
                    <Input
                      checked={field.state.value}
                      className="size-4"
                      type="checkbox"
                      onChange={(event) => field.handleChange(event.target.checked)}
                    />
                    {name === 'pullRequest'
                      ? 'Pull request'
                      : name[0].toUpperCase() + name.slice(1)}
                  </label>
                )}
              </form.Field>
            ))}
          </div>
          <div className="sm:col-span-2">
            <Button disabled={create.isPending} type="submit">
              <Send className="size-4" />
              {create.isPending ? 'Adding…' : 'Add webhook'}
            </Button>
          </div>
          {create.isError || remove.isError ? (
            <Alert className="sm:col-span-2">
              <AlertTitle>Webhook change failed</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(create.error ?? remove.error, 'The webhook could not be changed.')}
              </AlertDescription>
            </Alert>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function ProtectionSettings({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(branchProtectionsQueryOptions(params))
  const create = useMutation(createBranchProtectionMutationOptions())
  const remove = useMutation(deleteBranchProtectionMutationOptions())
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: branchProtectionsQueryOptions(params).queryKey })
  const form = useForm({
    defaultValues: {
      pattern: 'main',
      denyForcePush: true,
      denyDeletion: true,
      requiredApprovals: 1,
      dismissStaleReviews: true,
      requiredStatusChecks: '',
      requireSignedCommits: false,
    },
    onSubmit: async ({ value }) => {
      const requiredStatusChecks = value.requiredStatusChecks
        .split(/[\n,]/)
        .map((context) => context.trim())
        .filter(Boolean)
      await create.mutateAsync({
        path: { owner: params.owner, repo: params.repo },
        body: {
          pattern: value.pattern.trim(),
          deny_force_push: value.denyForcePush,
          deny_deletion: value.denyDeletion,
          required_approvals: value.requiredApprovals,
          dismiss_stale_reviews: value.dismissStaleReviews,
          required_status_checks: requiredStatusChecks,
          require_signed_commits: value.requireSignedCommits,
        },
      })
      await refresh()
    },
  })
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="size-4" /> Branch protection
        </CardTitle>
        <CardDescription>
          Exact branches outrank namespace rules; the longest namespace outranks the * fallback.
          Every transport evaluates the same policy before refs move.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {data.items.length ? (
          <div className="divide-y rounded-lg border bg-muted/10">
            {data.items.map((protection) => (
              <div
                className="grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center"
                key={protection.id}
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <GitBranch className="size-4 text-primary" />
                    <code className="rounded bg-muted px-1.5 py-0.5 text-sm font-semibold">
                      {protection.pattern}
                    </code>
                    <Badge variant="outline">
                      {protection.pattern === '*'
                        ? 'fallback'
                        : protection.pattern.endsWith('/*')
                          ? 'namespace'
                          : 'exact'}
                    </Badge>
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
                    {protection.deny_force_push ? (
                      <Badge variant="outline">No force push</Badge>
                    ) : null}
                    {protection.deny_deletion ? <Badge variant="outline">No deletion</Badge> : null}
                    {protection.required_approvals ? (
                      <Badge variant="outline">
                        {protection.required_approvals} approval
                        {protection.required_approvals === 1 ? '' : 's'}
                      </Badge>
                    ) : null}
                    {protection.required_status_checks.map((context) => (
                      <Badge key={context} variant="outline">
                        {context}
                      </Badge>
                    ))}
                    {protection.require_signed_commits ? (
                      <Badge variant="outline">Signed commits</Badge>
                    ) : null}
                  </div>
                </div>
                <Button
                  variant="outline"
                  disabled={remove.isPending}
                  onClick={() => {
                    void remove
                      .mutateAsync({
                        path: {
                          owner: params.owner,
                          repo: params.repo,
                          protection: protection.id,
                        },
                      })
                      .then(refresh)
                  }}
                >
                  Remove
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <div className="rounded-lg border border-dashed p-5 text-sm text-muted-foreground">
            No rules yet. Branch updates currently rely on repository write permission alone.
          </div>
        )}

        <form
          className="grid gap-4 border-t pt-6 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <div className="sm:col-span-2">
            <p className="text-sm font-semibold">Add a policy</p>
            <p className="mt-1 text-xs leading-5 text-muted-foreground">
              Use <code>main</code> for one branch, <code>release/*</code> for a namespace, or{' '}
              <code>*</code> as the fallback.
            </p>
          </div>
          <form.Field name="pattern">
            {(field) => (
              <Field htmlFor="protection-pattern" label="Branch pattern">
                <Input
                  id="protection-pattern"
                  maxLength={255}
                  required
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="requiredApprovals">
            {(field) => (
              <Field htmlFor="protection-approvals" label="Required approvals">
                <Input
                  id="protection-approvals"
                  max={100}
                  min={0}
                  type="number"
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.valueAsNumber || 0)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="requiredStatusChecks">
            {(field) => (
              <Field
                className="sm:col-span-2"
                htmlFor="protection-contexts"
                label="Required status contexts"
                hint="Comma or line separated; matching is case-sensitive."
              >
                <Textarea
                  id="protection-contexts"
                  placeholder={'ci/test\nci/build'}
                  value={field.state.value}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <div className="grid gap-2 sm:col-span-2 sm:grid-cols-2">
            {(
              [
                ['denyForcePush', 'Block force pushes'],
                ['denyDeletion', 'Block branch deletion'],
                ['dismissStaleReviews', 'Dismiss stale approvals'],
                ['requireSignedCommits', 'Require trusted SSH signatures'],
              ] as const
            ).map(([name, label]) => (
              <form.Field key={name} name={name}>
                {(field) => (
                  <label
                    className="flex items-center gap-3 rounded-lg border bg-muted/15 px-3 py-2.5 text-sm"
                    htmlFor={`protection-${name}`}
                  >
                    <Input
                      checked={field.state.value}
                      className="size-4"
                      id={`protection-${name}`}
                      type="checkbox"
                      onChange={(event) => field.handleChange(event.target.checked)}
                    />
                    {label}
                  </label>
                )}
              </form.Field>
            ))}
          </div>
          <div className="sm:col-span-2">
            <Button disabled={create.isPending} type="submit">
              <ShieldCheck className="size-4" />
              {create.isPending ? 'Adding policy…' : 'Add policy'}
            </Button>
          </div>
          {create.isError || remove.isError ? (
            <Alert className="sm:col-span-2">
              <AlertTitle>Protection change failed</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(
                  create.error ?? remove.error,
                  'The branch policy could not be changed.',
                )}
              </AlertDescription>
            </Alert>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}

function DangerZone({ params }: { params: RepositoryRouteParams }) {
  const navigate = useNavigate()
  const deletion = useMutation(deleteRepositoryMutationOptions())
  const restoration = useMutation(restoreRepositoryDeletionMutationOptions())
  const [confirmation, setConfirmation] = useState('')
  if (deletion.data) {
    return (
      <Card className="border-primary/45">
        <CardHeader>
          <CardTitle>Repository quarantined</CardTitle>
          <CardDescription>
            Git data is recoverable until {new Date(deletion.data.purge_after).toLocaleString()}.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            disabled={restoration.isPending}
            onClick={() => {
              void restoration
                .mutateAsync({ path: { deletion: deletion.data.id } })
                .then((repository) =>
                  navigate({
                    to: '/$owner/$repo/settings',
                    params: {
                      owner:
                        repository.owner.organization_slug ??
                        repository.owner.handle ??
                        params.owner,
                      repo: repository.slug,
                    },
                    replace: true,
                  }),
                )
            }}
          >
            {restoration.isPending ? 'Restoring…' : 'Restore repository'}
          </Button>
          {restoration.isError ? (
            <Alert className="mt-4">
              <AlertTitle>Restore failed</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(restoration.error, 'The repository could not be restored.')}
              </AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>
    )
  }
  return (
    <Card className="border-danger/45">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-danger">
          <Trash2 className="size-4" /> Danger zone
        </CardTitle>
        <CardDescription>
          Deletion immediately removes live routes and moves Git data into quarantine for recovery.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <Field htmlFor="delete-confirmation" label={`Type ${params.repo} to confirm`}>
          <Input
            id="delete-confirmation"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
          />
        </Field>
        <Button
          disabled={confirmation !== params.repo || deletion.isPending}
          variant="destructive"
          onClick={() => {
            void deletion.mutateAsync({ path: { owner: params.owner, repo: params.repo } })
          }}
        >
          {deletion.isPending ? 'Quarantining…' : 'Delete repository'}
        </Button>
        {deletion.isError ? (
          <Alert>
            <AlertTitle>Repository not deleted</AlertTitle>
            <AlertDescription>
              {apiErrorMessage(deletion.error, 'The repository could not be quarantined.')}
            </AlertDescription>
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  )
}
