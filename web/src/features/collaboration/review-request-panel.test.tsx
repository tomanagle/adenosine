// @vitest-environment jsdom

import type { PullRequestReviewRequest } from '@adenosine/api-client'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ReviewRequestPanel } from './review-request-panel'

vi.mock('./profile-link', () => ({
  ProfileLink: ({ did }: { did: string }) => <span>{did}</span>,
}))

const request: PullRequestReviewRequest = {
  uri: 'at://did:plc:owner/dev.adenosine.pullRequestReviewRequest/request',
  cid: 'bafyrequest',
  author_did: 'did:plc:owner',
  pull_request_uri: 'at://did:plc:author/dev.adenosine.pullRequest/pull',
  pull_request_cid: 'bafypull',
  target_repository_uri: 'at://did:plc:owner/dev.adenosine.repo/repository',
  target_repository_cid: 'bafyrepp',
  reviewer_did: 'did:plc:reviewer',
  requested_by_did: 'did:plc:maintainer',
  created_at: '2026-08-14T00:00:00Z',
  updated_at: '2026-08-14T00:00:00Z',
  indexed_at: '2026-08-14T00:00:01Z',
}

afterEach(cleanup)

describe('ReviewRequestPanel', () => {
  it('validates and submits a stable reviewer DID through the form', async () => {
    const onRequest = vi.fn().mockResolvedValue(undefined)
    render(
      <ReviewRequestPanel
        cancelling={false}
        canTriage
        items={[]}
        onCancel={vi.fn()}
        onRequest={onRequest}
        pullRequestOpen
        requesting={false}
      />,
    )

    fireEvent.change(screen.getByLabelText('Reviewer DID'), { target: { value: 'alice.test' } })
    fireEvent.click(screen.getByRole('button', { name: 'Request review' }))
    expect(await screen.findByText(/Enter a canonical DID/)).toBeTruthy()
    expect(onRequest).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Reviewer DID'), {
      target: { value: ' did:plc:alice ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Request review' }))
    await waitFor(() => expect(onRequest).toHaveBeenCalledWith('did:plc:alice'))
  })

  it('renders active state and lets triagers cancel it', async () => {
    const onCancel = vi.fn().mockResolvedValue(undefined)
    render(
      <ReviewRequestPanel
        cancelling={false}
        canTriage
        items={[request]}
        onCancel={onCancel}
        onRequest={vi.fn()}
        pullRequestOpen={false}
        requesting={false}
      />,
    )

    expect(screen.getByText('1 active')).toBeTruthy()
    expect(screen.queryByLabelText('Reviewer DID')).toBeNull()
    fireEvent.click(
      screen.getByRole('button', { name: 'Cancel review request for did:plc:reviewer' }),
    )
    await waitFor(() => expect(onCancel).toHaveBeenCalledWith('did:plc:reviewer'))
  })
})
