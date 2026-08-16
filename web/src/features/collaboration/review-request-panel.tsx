import type { PullRequestReviewRequest } from '@adenosine/api-client'
import { useForm } from '@tanstack/react-form'
import { UserPlus, X } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { fieldErrorMessage } from '@/lib/form'

import { ProfileLink } from './profile-link'

type ReviewRequestPanelProps = {
  items: PullRequestReviewRequest[]
  canTriage: boolean
  pullRequestOpen: boolean
  requesting: boolean
  cancelling: boolean
  onRequest: (reviewerDID: string) => Promise<void>
  onCancel: (reviewerDID: string) => Promise<void>
}

export function reviewerDIDError(value: string) {
  const did = value.trim()
  if (!did) return 'Enter the reviewer’s DID.'
  if (did.length > 2048 || !/^did:[a-z0-9]+:[A-Za-z0-9._:%-]+$/.test(did)) {
    return 'Enter a canonical DID such as did:plc:reviewer.'
  }
  return undefined
}

export function ReviewRequestPanel({
  items,
  canTriage,
  pullRequestOpen,
  requesting,
  cancelling,
  onRequest,
  onCancel,
}: ReviewRequestPanelProps) {
  const form = useForm({
    defaultValues: { reviewer: '' },
    onSubmit: async ({ value }) => {
      await onRequest(value.reviewer.trim())
      form.reset()
    },
  })

  return (
    <section className="rounded-lg border bg-card p-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.14em] text-primary">
            Review queue
          </p>
          <h3 className="mt-1 font-serif text-2xl">Requested reviewers</h3>
        </div>
        <Badge variant="outline">{items.length} active</Badge>
      </div>
      {items.length ? (
        <ul className="mt-4 divide-y rounded-md border">
          {items.map((request) => (
            <li className="flex items-center justify-between gap-3 px-3 py-2.5" key={request.uri}>
              <div className="min-w-0 text-sm">
                <ProfileLink did={request.reviewer_did} />
                <span className="ml-2 text-xs text-muted-foreground">
                  requested by {request.requested_by_did}
                </span>
              </div>
              {canTriage ? (
                <Button
                  aria-label={`Cancel review request for ${request.reviewer_did}`}
                  disabled={cancelling}
                  size="sm"
                  variant="ghost"
                  onClick={() => void onCancel(request.reviewer_did)}
                >
                  <X aria-hidden="true" className="size-4" /> Cancel
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-4 text-sm text-muted-foreground">No reviewers have been requested.</p>
      )}
      {canTriage && pullRequestOpen ? (
        <form
          className="mt-4 flex flex-col items-end gap-2 border-t pt-4 sm:flex-row"
          onSubmit={(event) => {
            event.preventDefault()
            void form.handleSubmit()
          }}
        >
          <form.Field
            name="reviewer"
            validators={{
              onBlur: ({ value }) => reviewerDIDError(value),
              onSubmit: ({ value }) => reviewerDIDError(value),
            }}
          >
            {(field) => {
              const error = fieldErrorMessage(field.state.meta.errors, field.state.meta.isTouched)
              return (
                <Field
                  className="min-w-0 flex-1"
                  error={error}
                  hint="Use the account’s stable DID; handles can change."
                  htmlFor="requested-reviewer"
                  label="Reviewer DID"
                >
                  <Input
                    aria-invalid={Boolean(error)}
                    autoComplete="off"
                    id="requested-reviewer"
                    name="reviewer"
                    onBlur={field.handleBlur}
                    onChange={(event) => field.handleChange(event.target.value)}
                    placeholder="did:plc:reviewer"
                    value={field.state.value}
                  />
                </Field>
              )
            }}
          </form.Field>
          <Button disabled={requesting} type="submit">
            <UserPlus aria-hidden="true" className="size-4" />
            {requesting ? 'Requesting…' : 'Request review'}
          </Button>
        </form>
      ) : null}
    </section>
  )
}
