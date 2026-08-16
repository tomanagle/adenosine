import { useForm } from '@tanstack/react-form'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { CalendarDays, Filter, Tag, X } from 'lucide-react'

import { Button, buttonVariants } from '@/components/ui/button'
import { Input, Select } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { RepositoryRouteParams } from '@/features/repository-browser/queries'

import { repositoryLabelsQueryOptions, repositoryMilestonesQueryOptions } from './queries'
import type { IssueFilters, PullRequestFilters } from './validation'

export function IssueFilterBar({
  filters,
  params,
}: {
  filters: IssueFilters
  params: RepositoryRouteParams
}) {
  const navigate = useNavigate({ from: '/$owner/$repo/issues' })
  return (
    <FilterBar
      filters={filters}
      params={params}
      states={['open', 'closed']}
      onChange={(search) =>
        navigate({
          search: { ...search, state: search.state === 'merged' ? undefined : search.state },
        })
      }
    />
  )
}

export function PullRequestFilterBar({
  filters,
  params,
}: {
  filters: PullRequestFilters
  params: RepositoryRouteParams
}) {
  const navigate = useNavigate({ from: '/$owner/$repo/pulls' })
  return (
    <FilterBar
      filters={filters}
      params={params}
      states={['open', 'closed', 'merged']}
      onChange={(search) => navigate({ search })}
    />
  )
}

function FilterBar({
  filters,
  onChange,
  params,
  states,
}: {
  filters: PullRequestFilters
  onChange: (search: PullRequestFilters) => Promise<void>
  params: RepositoryRouteParams
  states: Array<'open' | 'closed' | 'merged'>
}) {
  const { data: labels } = useSuspenseQuery(repositoryLabelsQueryOptions(params.owner, params.repo))
  const { data: milestones } = useSuspenseQuery(
    repositoryMilestonesQueryOptions(params.owner, params.repo),
  )
  const form = useForm({
    defaultValues: { assignee: filters.assignee ?? '' },
    onSubmit: ({ value }) => onChange({ ...filters, assignee: value.assignee.trim() || undefined }),
  })
  const active = Boolean(filters.state || filters.label || filters.assignee || filters.milestone)
  return (
    <div className="rounded-xl border bg-card p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="mr-1 flex items-center gap-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
          <Filter className="size-3.5" /> Filter
        </span>
        <Select
          aria-label="State"
          className="h-9 w-auto min-w-28"
          onChange={(event) =>
            void onChange({
              ...filters,
              state: (event.target.value || undefined) as 'open' | 'closed' | 'merged' | undefined,
            })
          }
          value={filters.state ?? ''}
        >
          <option value="">Any state</option>
          {states.map((state) => (
            <option key={state} value={state}>
              {state}
            </option>
          ))}
        </Select>
        <Select
          aria-label="Label"
          className="h-9 w-auto min-w-36"
          onChange={(event) =>
            void onChange({ ...filters, label: event.target.value || undefined })
          }
          value={filters.label ?? ''}
        >
          <option value="">Any label</option>
          {labels.items.map((label) => (
            <option key={label.id} value={label.id}>
              {label.name}
            </option>
          ))}
        </Select>
        <Select
          aria-label="Milestone"
          className="h-9 w-auto min-w-40"
          onChange={(event) =>
            void onChange({ ...filters, milestone: event.target.value || undefined })
          }
          value={filters.milestone ?? ''}
        >
          <option value="">Any milestone</option>
          {milestones.items.map((milestone) => (
            <option key={milestone.id} value={milestone.id}>
              {milestone.title}
            </option>
          ))}
        </Select>
        <form
          className="flex min-w-52 flex-1 gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="assignee">
            {(field) => (
              <Input
                aria-label="Assignee DID"
                className="h-9"
                onChange={(event) => field.handleChange(event.target.value)}
                placeholder="Assignee DID"
                value={field.state.value}
              />
            )}
          </form.Field>
          <Button size="sm" variant="outline">
            Apply
          </Button>
        </form>
        {active ? (
          <Button
            aria-label="Clear filters"
            onClick={() => void onChange({})}
            size="sm"
            variant="ghost"
          >
            <X className="size-4" /> Clear
          </Button>
        ) : null}
      </div>
      <div className="mt-3 flex gap-3 border-t pt-3 text-xs">
        <Link
          className={cn(buttonVariants({ size: 'sm', variant: 'ghost' }), 'h-7')}
          params={params}
          to="/$owner/$repo/labels"
        >
          <Tag className="size-3.5" /> Manage labels
        </Link>
        <Link
          className={cn(buttonVariants({ size: 'sm', variant: 'ghost' }), 'h-7')}
          params={params}
          to="/$owner/$repo/milestones"
        >
          <CalendarDays className="size-3.5" /> Manage milestones
        </Link>
      </div>
    </div>
  )
}
