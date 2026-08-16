import type { SubjectTriage } from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { CalendarDays, Check, Search, Tag, UserRound, X } from 'lucide-react'
import { useState } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field } from '@/components/ui/field'
import { Input, Select } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'
import type { RepositoryRouteParams } from '@/features/repository-browser/queries'

import { encodeRecordIdentity } from './identity'
import {
  issueTriageQueryOptions,
  pullRequestTriageQueryOptions,
  putIssueTriageMutationOptions,
  putPullRequestTriageMutationOptions,
  repositoryLabelsQueryOptions,
  repositoryMilestonesQueryOptions,
  visibleProfilesQueryOptions,
} from './queries'
import { LabelBadge } from './triage-management'

export function SubjectTriagePanel({
  canManage,
  kind,
  params,
  subjectUri,
}: {
  canManage: boolean
  kind: 'issue' | 'pull_request'
  params: RepositoryRouteParams
  subjectUri: string
}) {
  const encodedSubject = encodeRecordIdentity(subjectUri)
  const triageQuery =
    kind === 'issue'
      ? issueTriageQueryOptions(params.owner, params.repo, encodedSubject)
      : pullRequestTriageQueryOptions(params.owner, params.repo, encodedSubject)
  const { data: triage } = useSuspenseQuery(triageQuery)
  const { data: labels } = useSuspenseQuery(repositoryLabelsQueryOptions(params.owner, params.repo))
  const { data: milestones } = useSuspenseQuery(
    repositoryMilestonesQueryOptions(params.owner, params.repo),
  )
  if (!canManage) {
    return <TriageSummary triage={triage} />
  }
  return (
    <TriageForm
      encodedSubject={encodedSubject}
      kind={kind}
      labels={labels.items}
      milestones={milestones.items}
      params={params}
      query={triageQuery}
      triage={triage}
    />
  )
}

function TriageForm({
  encodedSubject,
  kind,
  labels,
  milestones,
  params,
  query,
  triage,
}: {
  encodedSubject: string
  kind: 'issue' | 'pull_request'
  labels: Array<{ id: string; name: string; color: string }>
  milestones: Array<{ id: string; title: string; state: 'open' | 'closed' }>
  params: RepositoryRouteParams
  query:
    | ReturnType<typeof issueTriageQueryOptions>
    | ReturnType<typeof pullRequestTriageQueryOptions>
  triage: SubjectTriage
}) {
  const queryClient = useQueryClient()
  const mutation = useMutation(
    kind === 'issue' ? putIssueTriageMutationOptions() : putPullRequestTriageMutationOptions(),
  )
  const [profileQuery, setProfileQuery] = useState('')
  const profiles = useQuery({
    ...visibleProfilesQueryOptions(profileQuery.trim()),
    enabled: profileQuery.trim().length > 0,
  })
  const form = useForm({
    defaultValues: {
      labelIds: [...triage.label_ids],
      assigneeDids: [...triage.assignee_dids],
      milestoneId: triage.milestone_id ?? '',
    },
    onSubmit: async ({ value }) => {
      const result = await mutation.mutateAsync({
        path: { ...params, subject: encodedSubject },
        body: {
          label_ids: value.labelIds,
          assignee_dids: value.assigneeDids,
          milestone_id: value.milestoneId || null,
        },
      })
      queryClient.setQueryData(query.queryKey, result.triage)
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>Planning</CardTitle>
        <CardDescription>
          Labels, assignees, and one milestone are saved together as a portable snapshot.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field name="labelIds">
            {(field) => (
              <fieldset>
                <legend className="mb-2 flex items-center gap-2 text-sm font-medium">
                  <Tag className="size-4" /> Labels
                </legend>
                <div className="flex flex-wrap gap-2">
                  {labels.map((label) => {
                    const selected = field.state.value.includes(label.id)
                    return (
                      <label
                        className="relative cursor-pointer rounded-full focus-within:ring-2 focus-within:ring-ring"
                        key={label.id}
                      >
                        <input
                          checked={selected}
                          className="sr-only"
                          onChange={() =>
                            field.handleChange(
                              selected
                                ? field.state.value.filter((id) => id !== label.id)
                                : [...field.state.value, label.id],
                            )
                          }
                          type="checkbox"
                        />
                        <span className="inline-flex items-center gap-1">
                          <LabelBadge label={label} />
                          {selected ? <Check className="size-3.5 text-primary" /> : null}
                        </span>
                      </label>
                    )
                  })}
                  {!labels.length ? (
                    <span className="text-xs text-muted-foreground">No labels are available.</span>
                  ) : null}
                </div>
              </fieldset>
            )}
          </form.Field>
          <form.Field name="assigneeDids">
            {(field) => (
              <div className="space-y-2">
                <Field
                  hint="Searches visible accounts indexed by this server."
                  htmlFor="triage-assignee-search"
                  label="Assignees"
                >
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-3 top-3 size-4 text-muted-foreground" />
                    <Input
                      className="pl-9"
                      id="triage-assignee-search"
                      maxLength={200}
                      onChange={(event) => setProfileQuery(event.target.value)}
                      placeholder="Search handle or display name"
                      value={profileQuery}
                    />
                  </div>
                </Field>
                {field.state.value.length ? (
                  <div className="flex flex-wrap gap-2" aria-label="Selected assignees">
                    {field.state.value.map((did) => {
                      const known = triage.assignees.find((assignee) => assignee.did === did)
                      return (
                        <Badge className="gap-1" key={did} variant="secondary">
                          <UserRound className="size-3" /> {known?.handle || did}
                          <button
                            aria-label={`Remove ${known?.handle || did}`}
                            onClick={() =>
                              field.handleChange(field.state.value.filter((value) => value !== did))
                            }
                            type="button"
                          >
                            <X className="size-3" />
                          </button>
                        </Badge>
                      )
                    })}
                  </div>
                ) : null}
                {profileQuery.trim() && profiles.data ? (
                  <ul className="max-h-48 divide-y overflow-auto rounded-lg border bg-background">
                    {profiles.data.items.map((profile) => {
                      const selected = field.state.value.includes(profile.did)
                      return (
                        <li
                          className="flex items-center justify-between gap-3 p-2"
                          key={profile.did}
                        >
                          <span className="min-w-0 truncate text-sm">
                            {profile.display_name || profile.handle || profile.did}
                            {profile.handle ? (
                              <span className="ml-2 text-xs text-muted-foreground">
                                @{profile.handle}
                              </span>
                            ) : null}
                          </span>
                          <Button
                            disabled={!selected && field.state.value.length >= 10}
                            onClick={() =>
                              field.handleChange(
                                selected
                                  ? field.state.value.filter((did) => did !== profile.did)
                                  : [...field.state.value, profile.did],
                              )
                            }
                            size="sm"
                            type="button"
                            variant={selected ? 'default' : 'outline'}
                          >
                            {selected ? 'Remove' : 'Assign'}
                          </Button>
                        </li>
                      )
                    })}
                  </ul>
                ) : null}
              </div>
            )}
          </form.Field>
          <form.Field name="milestoneId">
            {(field) => (
              <Field htmlFor="triage-milestone" label="Milestone">
                <Select
                  id="triage-milestone"
                  onChange={(event) => field.handleChange(event.target.value)}
                  value={field.state.value}
                >
                  <option value="">No milestone</option>
                  {milestones.map((milestone) => (
                    <option key={milestone.id} value={milestone.id}>
                      {milestone.title} {milestone.state === 'closed' ? '(closed)' : ''}
                    </option>
                  ))}
                </Select>
              </Field>
            )}
          </form.Field>
          <div className="flex items-center justify-between gap-3 border-t pt-4">
            {mutation.error ? (
              <p className="text-xs text-danger" role="alert">
                {apiErrorMessage(mutation.error, 'Planning metadata could not be saved.')}
              </p>
            ) : mutation.isSuccess ? (
              <output className="text-xs text-muted-foreground">
                Published. This view is updated while federation catches up.
              </output>
            ) : (
              <span />
            )}
            <Button disabled={mutation.isPending}>
              {mutation.isPending ? 'Publishing…' : 'Save planning'}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function TriageSummary({ triage }: { triage: SubjectTriage }) {
  if (!triage.labels.length && !triage.assignees.length && !triage.milestone) return null
  return (
    <Card>
      <CardHeader>
        <CardTitle>Planning</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        {triage.labels.length ? (
          <div className="flex flex-wrap gap-2">
            {triage.labels.map((label) => (
              <LabelBadge key={label.uri} label={label} />
            ))}
          </div>
        ) : null}
        {triage.assignees.length ? (
          <p className="flex flex-wrap items-center gap-2">
            <UserRound className="size-4 text-muted-foreground" />
            {triage.assignees.map((assignee) => assignee.handle || assignee.did).join(', ')}
          </p>
        ) : null}
        {triage.milestone ? (
          <p className="flex items-center gap-2">
            <CalendarDays className="size-4 text-muted-foreground" /> {triage.milestone.title}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}
