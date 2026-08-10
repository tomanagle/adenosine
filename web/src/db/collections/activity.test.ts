import type { SyncIssueRow, SyncStarRow } from '@adenosine/api-client/schemas'
import { describe, expect, it } from 'vitest'

import { composeRouteActivity } from './activity'

describe('composeRouteActivity', () => {
  it('composes explicit resources newest-first without an activity endpoint', () => {
    const star = {
      uri: 'at://star',
      cid: 'star-cid',
      author_did: 'did:plc:alice',
      repository_uri: 'at://repo',
      indexed_at: '2026-08-10T10:00:00Z',
    } as SyncStarRow
    const issue = {
      uri: 'at://issue',
      cid: 'issue-cid',
      author_did: 'did:plc:bob',
      repository_uri: 'at://repo',
      indexed_at: '2026-08-10T11:00:00Z',
    } as SyncIssueRow

    expect(composeRouteActivity({ stars: [star], issues: [issue] }, 2)).toEqual([
      expect.objectContaining({ kind: 'issue', uri: 'at://issue', subjectUri: 'at://repo' }),
      expect.objectContaining({ kind: 'star', uri: 'at://star', subjectUri: 'at://repo' }),
    ])
  })

  it('bounds composed activity even when a caller requests more', () => {
    const stars = Array.from({ length: 60 }, (_, index) => ({
      uri: `at://star/${index}`,
      cid: `cid-${index}`,
      author_did: 'did:plc:alice',
      repository_uri: 'at://repo',
      indexed_at: new Date(index * 1000).toISOString(),
    })) as SyncStarRow[]

    expect(composeRouteActivity({ stars }, 1_000)).toHaveLength(50)
  })
})
