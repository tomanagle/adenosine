import type { Repository } from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CircleCheck, X } from 'lucide-react'
import { useState } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button, buttonVariants } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import {
  repositorySnapshotQueryOptions,
  retainCreatedRepository,
} from '@/features/repositories/repository-snapshot.query'
import { apiErrorMessage } from '@/lib/api-error'
import { fieldErrorMessage } from '@/lib/form'
import { cn } from '@/lib/utils'
import { organizationsQueryOptions } from '@/features/organizations/queries'

import { createRepositoryMutationOptions } from './home.query'
import {
  emptyRepositoryForm,
  repositoryFormSchema,
  repositoryRequest,
  slugMessage,
} from './requests'
import { repositoryParams } from './viewer-repositories'

export function CreateRepositoryPanel({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const mutation = useMutation(createRepositoryMutationOptions())
  const [created, setCreated] = useState<Repository>()
  const snapshotQuery = repositorySnapshotQueryOptions()
  const organizations = useQuery(organizationsQueryOptions())

  const form = useForm({
    defaultValues: emptyRepositoryForm,
    validators: { onChange: repositoryFormSchema, onSubmit: repositoryFormSchema },
    onSubmit: async ({ value, formApi }) => {
      const repository = await mutation.mutateAsync({ body: repositoryRequest(value) })
      setCreated(repository)
      formApi.reset()
      await queryClient.invalidateQueries({ queryKey: snapshotQuery.queryKey })
      queryClient.setQueryData(snapshotQuery.queryKey, (snapshot) =>
        retainCreatedRepository(snapshot, repository),
      )
    },
  })

  return (
    <section
      aria-labelledby="new-repository-title"
      className="rounded-xl border bg-card p-5 shadow-sm sm:p-6"
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="font-serif text-2xl" id="new-repository-title">
            New repository
          </h2>
          <p className="mt-1 text-sm text-muted-foreground">
            Created on this server and published to your identity. Push over HTTPS or SSH once it
            exists.
          </p>
        </div>
        <Button aria-label="Close new repository form" onClick={onClose} size="sm" variant="ghost">
          <X className="size-4" aria-hidden="true" />
        </Button>
      </div>

      {created ? (
        <CreatedRepository
          repository={created}
          onCreateAnother={() => setCreated(undefined)}
          onClose={onClose}
        />
      ) : (
        <form
          className="mt-5 grid gap-4 sm:grid-cols-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field
            name="slug"
            validators={{
              onMount: ({ value }) => slugMessage(value),
              onChange: ({ value }) => slugMessage(value),
              onSubmit: ({ value }) => slugMessage(value),
            }}
          >
            {(field) => {
              const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
              return (
                <Field
                  error={error}
                  hint="Lowercase letters, numbers, dots, and dashes."
                  htmlFor="new-repository-slug"
                  label="Name"
                >
                  <Input
                    aria-invalid={Boolean(error)}
                    autoComplete="off"
                    id="new-repository-slug"
                    maxLength={100}
                    name="slug"
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value.trim())}
                    placeholder="ledger-service"
                    spellCheck={false}
                    value={field.state.value}
                  />
                </Field>
              )
            }}
          </form.Field>

          <form.Field name="default_branch">
            {(field) => {
              const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
              return (
                <Field
                  error={error}
                  hint="The branch created with the first push."
                  htmlFor="new-repository-branch"
                  label="Default branch"
                >
                  <Input
                    aria-invalid={Boolean(error)}
                    id="new-repository-branch"
                    maxLength={255}
                    name="default_branch"
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    spellCheck={false}
                    value={field.state.value ?? ''}
                  />
                </Field>
              )
            }}
          </form.Field>

          <form.Field name="display_name">
            {(field) => (
              <Field
                error={fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)}
                hint="Optional."
                htmlFor="new-repository-display-name"
                label="Display name"
              >
                <Input
                  id="new-repository-display-name"
                  maxLength={255}
                  name="display_name"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  placeholder="Ledger Service"
                  value={field.state.value ?? ''}
                />
              </Field>
            )}
          </form.Field>

          <form.Field name="visibility">
            {(field) => (
              <Field
                hint="Private repositories stay off the public network list."
                htmlFor="new-repository-visibility"
                label="Visibility"
              >
                <Select
                  id="new-repository-visibility"
                  name="visibility"
                  onBlur={field.handleBlur}
                  onChange={(event) =>
                    field.handleChange(event.target.value === 'private' ? 'private' : 'public')
                  }
                  value={field.state.value ?? 'public'}
                >
                  <option value="public">Public</option>
                  <option value="private">Private</option>
                </Select>
              </Field>
            )}
          </form.Field>

          <form.Field name="organization">
            {(field) => (
              <Field
                hint="Choose shared ownership or keep this under your account."
                htmlFor="new-repository-owner"
                label="Owner"
              >
                <Select
                  id="new-repository-owner"
                  name="organization"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value || undefined)}
                  value={field.state.value ?? ''}
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

          <form.Field name="description">
            {(field) => (
              <Field
                className="sm:col-span-2"
                error={fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)}
                hint="Optional. Shown in search and on the network list."
                htmlFor="new-repository-description"
                label="Description"
              >
                <Textarea
                  id="new-repository-description"
                  maxLength={2000}
                  name="description"
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value ?? ''}
                />
              </Field>
            )}
          </form.Field>

          {mutation.isError ? (
            <Alert className="sm:col-span-2">
              <AlertTitle>Repository not created</AlertTitle>
              <AlertDescription>
                {apiErrorMessage(mutation.error, 'The server rejected this repository.')}
              </AlertDescription>
            </Alert>
          ) : null}

          <div className="flex items-center gap-3 sm:col-span-2">
            <form.Subscribe selector={(state) => state.isSubmitting}>
              {(isSubmitting) => (
                <Button disabled={isSubmitting} type="submit">
                  {isSubmitting ? 'Creating...' : 'Create repository'}
                </Button>
              )}
            </form.Subscribe>
            <Button onClick={onClose} type="button" variant="ghost">
              Cancel
            </Button>
          </div>
        </form>
      )}
    </section>
  )
}

function CreatedRepository({
  onClose,
  onCreateAnother,
  repository,
}: {
  onClose: () => void
  onCreateAnother: () => void
  repository: Repository
}) {
  const params = repositoryParams(repository)
  return (
    <div className="mt-5">
      <p className="flex items-center gap-2 font-medium text-primary">
        <CircleCheck className="size-4" aria-hidden="true" />
        {repository.slug} is ready
      </p>
      <p className="mt-2 text-sm text-muted-foreground">
        Push your first commit to publish code on the <code>{repository.default_branch}</code>{' '}
        branch.
      </p>
      <dl className="mt-4 space-y-2 text-sm">
        <div>
          <dt className="text-xs uppercase tracking-[0.16em] text-muted-foreground">HTTPS</dt>
          <dd className="mt-1 select-all break-all font-mono text-xs">
            {repository.hosting.git_https_url}
          </dd>
        </div>
        {repository.hosting.git_ssh_url ? (
          <div>
            <dt className="text-xs uppercase tracking-[0.16em] text-muted-foreground">SSH</dt>
            <dd className="mt-1 select-all break-all font-mono text-xs">
              {repository.hosting.git_ssh_url}
            </dd>
          </div>
        ) : null}
      </dl>
      <div className="mt-5 flex flex-wrap items-center gap-4">
        <Link className={cn(buttonVariants())} params={params} to="/$owner/$repo">
          Open repository
        </Link>
        <button
          className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          onClick={onCreateAnother}
          type="button"
        >
          Create another
        </button>
        <button
          className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          onClick={onClose}
          type="button"
        >
          Done
        </button>
      </div>
    </div>
  )
}
