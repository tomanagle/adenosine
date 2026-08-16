import type { Repository } from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { X } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import { PublicationNotice, usePublication } from '@/features/collaboration/publication'
import { pullRequestsQueryOptions } from '@/features/collaboration/queries'
import { branchesQueryOptions } from '@/features/repository-browser/queries'
import { apiErrorMessage } from '@/lib/api-error'
import { fieldErrorMessage } from '@/lib/form'

import { createPullRequestMutationOptions } from './home.query'
import {
  branchHeadSha,
  emptyProposalForm,
  proposalFormSchema,
  proposalRequest,
  requiredMessage,
} from './requests'
import { repositoryParams } from './viewer-repositories'

type CreatePullRequestDependencies = {
  branchesQueryOptions: typeof branchesQueryOptions
  createPullRequestMutationOptions: typeof createPullRequestMutationOptions
  pullRequestsQueryOptions: typeof pullRequestsQueryOptions
}

const createPullRequestDependencies: CreatePullRequestDependencies = {
  branchesQueryOptions,
  createPullRequestMutationOptions,
  pullRequestsQueryOptions,
}

/** Home composes branch proposals and sends fork work to its upstream by default. */
export function CreatePullRequestPanel({
  dependencies = createPullRequestDependencies,
  onClose,
  repositories,
  networkRepositories,
}: {
  dependencies?: CreatePullRequestDependencies
  onClose: () => void
  repositories: Repository[]
  networkRepositories: Repository[]
}) {
  const first = repositories[0]
  const [selectedUri, setSelectedUri] = useState(first?.uri ?? '')
  const selected = repositories.find((repository) => repository.uri === selectedUri) ?? first
  const params = selected ? repositoryParams(selected) : { owner: '', repo: '' }
  const branchQuery = useQuery({
    ...dependencies.branchesQueryOptions(params),
    enabled: Boolean(selected),
  })
  const branches = branchQuery.data?.items ?? []

  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const mutation = useMutation(dependencies.createPullRequestMutationOptions())
  const publication = usePublication()
  const [headError, setHeadError] = useState<string>()

  const form = useForm({
    defaultValues: emptyProposalForm(
      first?.uri ?? '',
      first?.default_branch ?? '',
      first?.forked_from?.uri ?? first?.uri ?? '',
    ),
    validators: { onChange: proposalFormSchema, onSubmit: proposalFormSchema },
    onSubmit: async ({ value }) => {
      const headSha = branchHeadSha(branches, value.source_branch)
      if (!headSha) {
        setHeadError('That branch head could not be read. Reload the branch list and try again.')
        return
      }
      setHeadError(undefined)
      await publication.publish(
        async () =>
          (await mutation.mutateAsync({ body: proposalRequest(value, headSha) })).pull_request,
        async (reference) => {
          const projection = await queryClient.fetchQuery({
            ...dependencies.pullRequestsQueryOptions(value.target_repository_uri),
            staleTime: 0,
          })
          return projection.items.some(
            (pull) => pull.uri === reference.uri && pull.cid === reference.cid,
          )
        },
      )
      await queryClient.invalidateQueries({
        queryKey: dependencies.pullRequestsQueryOptions(value.target_repository_uri).queryKey,
      })
      const target = networkRepositories.find(
        (repository) => repository.uri === value.target_repository_uri,
      )
      if (target) {
        await navigate({ params: repositoryParams(target), to: '/$owner/$repo/pulls' })
      } else if (value.target_repository_uri === value.repository_uri) {
        await navigate({ params, to: '/$owner/$repo/pulls' })
      }
    },
  })

  return (
    <section
      aria-labelledby="new-pull-request-title"
      className="rounded-xl border bg-card p-5 shadow-sm sm:p-6"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="font-serif text-2xl" id="new-pull-request-title">
            New pull request
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Propose a local branch to its repository or, for a fork, back to its portable upstream.
            The proposal records the current branch head.
          </p>
        </div>
        <Button
          aria-label="Close new pull request form"
          onClick={onClose}
          size="sm"
          variant="ghost"
        >
          <X className="size-4" aria-hidden="true" />
        </Button>
      </div>

      <form
        className="mt-5 grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => {
          event.preventDefault()
          void form.handleSubmit()
        }}
      >
        <form.Field name="repository_uri">
          {(field) => (
            <Field
              className="sm:col-span-2"
              error={fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)}
              htmlFor="new-pull-repository"
              label="Repository"
            >
              <Select
                id="new-pull-repository"
                name="repository_uri"
                onBlur={field.handleBlur}
                onChange={(event) => {
                  const uri = event.target.value
                  const repository = repositories.find((value) => value.uri === uri)
                  setSelectedUri(uri)
                  field.handleChange(uri)
                  form.setFieldValue('target_branch', repository?.default_branch ?? '')
                  form.setFieldValue(
                    'target_repository_uri',
                    repository?.forked_from?.uri ?? repository?.uri ?? '',
                  )
                  form.setFieldValue('source_branch', '')
                }}
                value={field.state.value}
              >
                {repositories.map((repository) => (
                  <option key={repository.uri} value={repository.uri ?? ''}>
                    {repository.slug}
                  </option>
                ))}
              </Select>
            </Field>
          )}
        </form.Field>

        <form.Field name="target_repository_uri">
          {(field) => {
            const target = networkRepositories.find(
              (repository) => repository.uri === field.state.value,
            )
            return (
              <Field
                className="sm:col-span-2"
                hint={
                  selected?.forked_from
                    ? 'Pull requests from forks target their upstream by default.'
                    : 'This proposal stays within the repository.'
                }
                htmlFor="new-pull-target-repository"
                label="Target repository"
              >
                <Input
                  id="new-pull-target-repository"
                  readOnly
                  value={
                    target
                      ? `${target.owner.organization_slug ?? target.owner.handle ?? target.owner.did}/${target.slug}`
                      : field.state.value
                  }
                />
              </Field>
            )
          }}
        </form.Field>

        <form.Field
          name="source_branch"
          validators={{
            onMount: ({ value }) => requiredMessage(value, 'Select a source branch.'),
            onChange: ({ value }) => requiredMessage(value, 'Select a source branch.'),
            onSubmit: ({ value }) => requiredMessage(value, 'Select a source branch.'),
          }}
        >
          {(field) => {
            const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
            return (
              <Field
                error={error}
                hint={branchHint(branchQuery.isPending, branches.length)}
                htmlFor="new-pull-source"
                label="Source branch"
              >
                <Select
                  aria-invalid={Boolean(error)}
                  disabled={branches.length === 0}
                  id="new-pull-source"
                  name="source_branch"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                >
                  <option value="">Select a branch</option>
                  {branches.map((branch) => (
                    <option key={branch.name} value={branch.name}>
                      {branch.name}
                    </option>
                  ))}
                </Select>
              </Field>
            )
          }}
        </form.Field>

        <form.Field
          name="target_branch"
          validators={{
            onMount: ({ value }) => requiredMessage(value, 'Select a target branch.'),
            onChange: ({ value }) => requiredMessage(value, 'Select a target branch.'),
            onSubmit: ({ value }) => requiredMessage(value, 'Select a target branch.'),
          }}
        >
          {(field) => {
            const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
            return (
              <Field
                error={error}
                hint="Where the work should land."
                htmlFor="new-pull-target"
                label="Target branch"
              >
                <Select
                  aria-invalid={Boolean(error)}
                  disabled={branches.length === 0}
                  id="new-pull-target"
                  name="target_branch"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                >
                  <option value="">Select a branch</option>
                  {branches.map((branch) => (
                    <option key={branch.name} value={branch.name}>
                      {branch.name}
                    </option>
                  ))}
                </Select>
              </Field>
            )
          }}
        </form.Field>

        <form.Field
          name="title"
          validators={{
            onMount: ({ value }) => requiredMessage(value, 'Enter a title.'),
            onChange: ({ value }) => requiredMessage(value, 'Enter a title.'),
            onSubmit: ({ value }) => requiredMessage(value, 'Enter a title.'),
          }}
        >
          {(field) => {
            const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
            return (
              <Field className="sm:col-span-2" error={error} htmlFor="new-pull-title" label="Title">
                <Input
                  aria-invalid={Boolean(error)}
                  id="new-pull-title"
                  maxLength={255}
                  name="title"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder="Reconcile ledger balances"
                  value={field.state.value}
                />
              </Field>
            )
          }}
        </form.Field>

        <form.Field name="body">
          {(field) => (
            <Field
              className="sm:col-span-2"
              error={fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)}
              hint="Markdown is rendered on the proposal page."
              htmlFor="new-pull-body"
              label="Description"
            >
              <Textarea
                id="new-pull-body"
                maxLength={65535}
                name="body"
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
                value={field.state.value}
              />
            </Field>
          )}
        </form.Field>

        {branchQuery.isError ? (
          <Alert className="sm:col-span-2">
            <AlertTitle>Branches unavailable</AlertTitle>
            <AlertDescription>
              {apiErrorMessage(branchQuery.error, 'This server could not read the branch list.')}
            </AlertDescription>
          </Alert>
        ) : null}

        {mutation.isError || headError ? (
          <Alert className="sm:col-span-2">
            <AlertTitle>Pull request not opened</AlertTitle>
            <AlertDescription>
              {headError ?? apiErrorMessage(mutation.error, 'The server rejected this proposal.')}
            </AlertDescription>
          </Alert>
        ) : null}

        <div className="flex flex-wrap items-center gap-3 sm:col-span-2">
          <form.Subscribe selector={(state) => state.isSubmitting}>
            {(isSubmitting) => (
              <Button disabled={isSubmitting || branches.length === 0} type="submit">
                {isSubmitting ? 'Publishing...' : 'Open pull request'}
              </Button>
            )}
          </form.Subscribe>
          <Button onClick={onClose} type="button" variant="ghost">
            Cancel
          </Button>
          <PublicationNotice state={publication.state} />
        </div>
      </form>
    </section>
  )
}

function branchHint(pending: boolean, count: number) {
  if (pending) return 'Reading branches...'
  if (count === 0) return 'This repository has no branches yet. Push a commit first.'
  return 'The branch you want reviewed.'
}
