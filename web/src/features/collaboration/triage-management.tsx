import type {
  RepositoryLabel,
  RepositoryLabelList,
  RepositoryMilestone,
  RepositoryMilestoneInput,
  RepositoryMilestoneList,
} from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CalendarDays, Pencil, Tag, Trash2 } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'
import {
  repositoryQueryOptions,
  type RepositoryRouteParams,
} from '@/features/repository-browser/queries'

import {
  createRepositoryLabelMutationOptions,
  createRepositoryMilestoneMutationOptions,
  deleteRepositoryLabelMutationOptions,
  deleteRepositoryMilestoneMutationOptions,
  repositoryLabelsQueryOptions,
  repositoryMilestonesQueryOptions,
  updateRepositoryLabelMutationOptions,
  updateRepositoryMilestoneMutationOptions,
} from './queries'

export function LabelsPage({ params }: { params: RepositoryRouteParams }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data } = useSuspenseQuery(repositoryLabelsQueryOptions(params.owner, params.repo))
  const canManage = repository.hosting.local && Boolean(repository.viewer_can_admin)
  return (
    <section className="space-y-5">
      <TriageCatalogHeader active="labels" params={params} />
      {canManage ? <LabelEditor params={params} /> : null}
      {!canManage && repository.hosting.local ? <ReadOnlyNotice /> : null}
      {data.items.length ? (
        <ul className="divide-y rounded-xl border bg-card">
          {data.items.map((label) => (
            <li className="p-4" key={label.uri}>
              <div className="flex flex-wrap items-start justify-between gap-4">
                <div className="min-w-0">
                  <LabelBadge label={label} />
                  {label.description ? (
                    <p className="mt-2 text-sm text-muted-foreground">{label.description}</p>
                  ) : null}
                </div>
                {canManage ? <LabelActions label={label} params={params} /> : null}
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            No visible labels have been published for this repository.
          </CardContent>
        </Card>
      )}
    </section>
  )
}

export function MilestonesPage({ params }: { params: RepositoryRouteParams }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const { data } = useSuspenseQuery(repositoryMilestonesQueryOptions(params.owner, params.repo))
  const canManage = repository.hosting.local && Boolean(repository.viewer_can_admin)
  return (
    <section className="space-y-5">
      <TriageCatalogHeader active="milestones" params={params} />
      {canManage ? <MilestoneEditor params={params} /> : null}
      {!canManage && repository.hosting.local ? <ReadOnlyNotice /> : null}
      {data.items.length ? (
        <ul className="divide-y rounded-xl border bg-card">
          {data.items.map((milestone) => (
            <li className="p-4" key={milestone.uri}>
              <div className="flex flex-wrap items-start justify-between gap-4">
                <div>
                  <div className="flex flex-wrap items-center gap-2">
                    <h3 className="font-medium">{milestone.title}</h3>
                    <Badge variant={milestone.state === 'open' ? 'secondary' : 'outline'}>
                      {milestone.state}
                    </Badge>
                  </div>
                  {milestone.description ? (
                    <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
                      {milestone.description}
                    </p>
                  ) : null}
                  {milestone.due_at ? (
                    <p className="mt-2 flex items-center gap-1 text-xs text-muted-foreground">
                      <CalendarDays className="size-3.5" /> Due{' '}
                      {new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(
                        new Date(milestone.due_at),
                      )}
                    </p>
                  ) : null}
                </div>
                {canManage ? <MilestoneActions milestone={milestone} params={params} /> : null}
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <Card>
          <CardContent className="py-10 text-center text-sm text-muted-foreground">
            No visible milestones have been published for this repository.
          </CardContent>
        </Card>
      )}
    </section>
  )
}

function TriageCatalogHeader({
  active,
  params,
}: {
  active: 'labels' | 'milestones'
  params: RepositoryRouteParams
}) {
  return (
    <header className="flex flex-col gap-4 border-b pb-4 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">
          Work planning
        </p>
        <h2 className="mt-1 font-serif text-3xl">
          {active === 'labels' ? 'Labels' : 'Milestones'}
        </h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Portable repository metadata follows the project across its network lineage.
        </p>
      </div>
      <nav className="flex gap-2" aria-label="Issue planning">
        <Link
          className={cn(
            buttonVariants({ size: 'sm', variant: active === 'labels' ? 'default' : 'outline' }),
          )}
          params={params}
          to="/$owner/$repo/labels"
        >
          <Tag className="size-4" /> Labels
        </Link>
        <Link
          className={cn(
            buttonVariants({
              size: 'sm',
              variant: active === 'milestones' ? 'default' : 'outline',
            }),
          )}
          params={params}
          to="/$owner/$repo/milestones"
        >
          <CalendarDays className="size-4" /> Milestones
        </Link>
      </nav>
    </header>
  )
}

function LabelEditor({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const mutation = useMutation(createRepositoryLabelMutationOptions())
  const form = useForm({
    defaultValues: { name: '', color: '2f6f4e', description: '' },
    onSubmit: async ({ value, formApi }) => {
      const result = await mutation.mutateAsync({ path: params, body: value })
      updateLabelList(queryClient, params, (items) => [result.label, ...items])
      formApi.reset()
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>New label</CardTitle>
        <CardDescription>Use a concise name and a six-digit hexadecimal color.</CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_10rem]"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="name">
            {(field) => (
              <Field htmlFor="new-label-name" label="Name">
                <Input
                  id="new-label-name"
                  maxLength={50}
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="color">
            {(field) => (
              <Field htmlFor="new-label-color" label="Color">
                <Input
                  id="new-label-color"
                  pattern="#?[0-9A-Fa-f]{6}"
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value.replace(/^#/, ''))}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="description">
            {(field) => (
              <Field className="sm:col-span-2" htmlFor="new-label-description" label="Description">
                <Input
                  id="new-label-description"
                  maxLength={255}
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <MutationFooter
            error={mutation.error}
            pending={mutation.isPending}
            submit="Create label"
          />
        </form>
      </CardContent>
    </Card>
  )
}

function LabelActions({
  label,
  params,
}: {
  label: RepositoryLabel
  params: RepositoryRouteParams
}) {
  const queryClient = useQueryClient()
  const update = useMutation(updateRepositoryLabelMutationOptions())
  const remove = useMutation(deleteRepositoryLabelMutationOptions())
  const form = useForm({
    defaultValues: { name: label.name, color: label.color, description: label.description },
    onSubmit: async ({ value }) => {
      const result = await update.mutateAsync({ path: { ...params, label: label.id }, body: value })
      updateLabelList(queryClient, params, (items) =>
        items.map((item) => (item.id === label.id ? result.label : item)),
      )
    },
  })
  return (
    <details className="w-full sm:w-80">
      <summary
        className={cn(
          buttonVariants({ size: 'sm', variant: 'outline' }),
          'ml-auto w-fit cursor-pointer list-none',
        )}
      >
        <Pencil className="size-3.5" /> Edit
      </summary>
      <form
        className="mt-3 grid gap-3 rounded-lg border bg-muted/20 p-3"
        onSubmit={(event) => {
          event.preventDefault()
          void form.handleSubmit()
        }}
      >
        <form.Field name="name">
          {(field) => (
            <Field htmlFor={`label-${label.id}-name`} label="Name">
              <Input
                id={`label-${label.id}-name`}
                maxLength={50}
                required
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="color">
          {(field) => (
            <Field htmlFor={`label-${label.id}-color`} label="Color">
              <Input
                id={`label-${label.id}-color`}
                pattern="#?[0-9A-Fa-f]{6}"
                required
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value.replace(/^#/, ''))}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="description">
          {(field) => (
            <Field htmlFor={`label-${label.id}-description`} label="Description">
              <Input
                id={`label-${label.id}-description`}
                maxLength={255}
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <div className="flex justify-between gap-2">
          <Button
            disabled={remove.isPending}
            onClick={() =>
              void remove
                .mutateAsync({ path: { ...params, label: label.id } })
                .then(() =>
                  updateLabelList(queryClient, params, (items) =>
                    items.filter((item) => item.id !== label.id),
                  ),
                )
            }
            size="sm"
            type="button"
            variant="ghost"
          >
            <Trash2 className="size-3.5" /> Delete
          </Button>
          <Button disabled={update.isPending} size="sm">
            {update.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>
        {update.error || remove.error ? (
          <p className="text-xs text-danger" role="alert">
            {apiErrorMessage(update.error ?? remove.error, 'The label could not be changed.')}
          </p>
        ) : null}
      </form>
    </details>
  )
}

function MilestoneEditor({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const mutation = useMutation(createRepositoryMilestoneMutationOptions())
  const form = useForm({
    defaultValues: { title: '', description: '', state: 'open' as const, dueAt: '' },
    onSubmit: async ({ value, formApi }) => {
      const body: RepositoryMilestoneInput = {
        title: value.title,
        description: value.description,
        state: value.state,
        due_at: dateInput(value.dueAt),
      }
      const result = await mutation.mutateAsync({ path: params, body })
      updateMilestoneList(queryClient, params, (items) => [result.milestone, ...items])
      formApi.reset()
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>New milestone</CardTitle>
        <CardDescription>
          Group related issues and pull requests around a delivery target.
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
          <form.Field name="title">
            {(field) => (
              <Field htmlFor="new-milestone-title" label="Title">
                <Input
                  id="new-milestone-title"
                  maxLength={255}
                  required
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="dueAt">
            {(field) => (
              <Field htmlFor="new-milestone-due" label="Due date">
                <Input
                  id="new-milestone-due"
                  type="date"
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <form.Field name="description">
            {(field) => (
              <Field
                className="sm:col-span-2"
                htmlFor="new-milestone-description"
                label="Description"
              >
                <Textarea
                  id="new-milestone-description"
                  maxLength={65535}
                  value={field.state.value}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
              </Field>
            )}
          </form.Field>
          <MutationFooter
            error={mutation.error}
            pending={mutation.isPending}
            submit="Create milestone"
          />
        </form>
      </CardContent>
    </Card>
  )
}

function MilestoneActions({
  milestone,
  params,
}: {
  milestone: RepositoryMilestone
  params: RepositoryRouteParams
}) {
  const queryClient = useQueryClient()
  const update = useMutation(updateRepositoryMilestoneMutationOptions())
  const remove = useMutation(deleteRepositoryMilestoneMutationOptions())
  const form = useForm({
    defaultValues: {
      title: milestone.title,
      description: milestone.description,
      state: milestone.state,
      dueAt: milestone.due_at?.slice(0, 10) ?? '',
    },
    onSubmit: async ({ value }) => {
      const result = await update.mutateAsync({
        path: { ...params, milestone: milestone.id },
        body: {
          title: value.title,
          description: value.description,
          state: value.state,
          due_at: dateInput(value.dueAt),
        },
      })
      updateMilestoneList(queryClient, params, (items) =>
        items.map((item) => (item.id === milestone.id ? result.milestone : item)),
      )
    },
  })
  return (
    <details className="w-full sm:w-80">
      <summary
        className={cn(
          buttonVariants({ size: 'sm', variant: 'outline' }),
          'ml-auto w-fit cursor-pointer list-none',
        )}
      >
        <Pencil className="size-3.5" /> Edit
      </summary>
      <form
        className="mt-3 grid gap-3 rounded-lg border bg-muted/20 p-3"
        onSubmit={(event) => {
          event.preventDefault()
          void form.handleSubmit()
        }}
      >
        <form.Field name="title">
          {(field) => (
            <Field htmlFor={`milestone-${milestone.id}-title`} label="Title">
              <Input
                id={`milestone-${milestone.id}-title`}
                maxLength={255}
                required
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="state">
          {(field) => (
            <Field htmlFor={`milestone-${milestone.id}-state`} label="State">
              <Select
                id={`milestone-${milestone.id}-state`}
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value as 'open' | 'closed')}
              >
                <option value="open">Open</option>
                <option value="closed">Closed</option>
              </Select>
            </Field>
          )}
        </form.Field>
        <form.Field name="dueAt">
          {(field) => (
            <Field htmlFor={`milestone-${milestone.id}-due`} label="Due date">
              <Input
                id={`milestone-${milestone.id}-due`}
                type="date"
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="description">
          {(field) => (
            <Field htmlFor={`milestone-${milestone.id}-description`} label="Description">
              <Textarea
                id={`milestone-${milestone.id}-description`}
                maxLength={65535}
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <div className="flex justify-between gap-2">
          <Button
            disabled={remove.isPending}
            onClick={() =>
              void remove
                .mutateAsync({ path: { ...params, milestone: milestone.id } })
                .then(() =>
                  updateMilestoneList(queryClient, params, (items) =>
                    items.filter((item) => item.id !== milestone.id),
                  ),
                )
            }
            size="sm"
            type="button"
            variant="ghost"
          >
            <Trash2 className="size-3.5" /> Delete
          </Button>
          <Button disabled={update.isPending} size="sm">
            {update.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>
        {update.error || remove.error ? (
          <p className="text-xs text-danger" role="alert">
            {apiErrorMessage(update.error ?? remove.error, 'The milestone could not be changed.')}
          </p>
        ) : null}
      </form>
    </details>
  )
}

function MutationFooter({
  error,
  pending,
  submit,
}: {
  error: unknown
  pending: boolean
  submit: string
}) {
  return (
    <div className="flex items-center justify-between gap-3 sm:col-span-2">
      {error ? (
        <p className="text-xs text-danger" role="alert">
          {apiErrorMessage(error, `The ${submit.toLowerCase()} request failed.`)}
        </p>
      ) : (
        <span />
      )}
      <Button disabled={pending}>{pending ? 'Publishing…' : submit}</Button>
    </div>
  )
}

function ReadOnlyNotice() {
  return (
    <Alert>
      <AlertTitle>Read-only planning metadata</AlertTitle>
      <AlertDescription>
        You need maintainer permission to manage labels and milestones.
      </AlertDescription>
    </Alert>
  )
}

export function LabelBadge({ label }: { label: Pick<RepositoryLabel, 'name' | 'color'> }) {
  return (
    <span
      className="inline-flex rounded-full border px-2.5 py-1 text-xs font-semibold"
      style={{
        backgroundColor: `#${label.color}22`,
        borderColor: `#${label.color}88`,
        color: `#${label.color}`,
      }}
    >
      {label.name}
    </span>
  )
}

function dateInput(value: string): string | null {
  return value ? new Date(`${value}T00:00:00.000Z`).toISOString() : null
}

function updateLabelList(
  queryClient: ReturnType<typeof useQueryClient>,
  params: RepositoryRouteParams,
  update: (items: RepositoryLabel[]) => RepositoryLabel[],
) {
  const query = repositoryLabelsQueryOptions(params.owner, params.repo)
  queryClient.setQueryData<RepositoryLabelList>(query.queryKey, (current) =>
    current ? { ...current, items: update(current.items) } : current,
  )
}

function updateMilestoneList(
  queryClient: ReturnType<typeof useQueryClient>,
  params: RepositoryRouteParams,
  update: (items: RepositoryMilestone[]) => RepositoryMilestone[],
) {
  const query = repositoryMilestonesQueryOptions(params.owner, params.repo)
  queryClient.setQueryData<RepositoryMilestoneList>(query.queryKey, (current) =>
    current ? { ...current, items: update(current.items) } : current,
  )
}
