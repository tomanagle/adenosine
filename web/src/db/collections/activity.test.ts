import { zSyncIssue, zSyncStar } from '@adenosine/api-client/schemas'
import { describe, expect, it } from 'vitest'

import { composeRouteActivity } from './activity'

describe('composeRouteActivity', () => {
  it('composes explicit resources newest-first without an activity endpoint', () => {
    const star = zSyncStar.parse({
      uri: 'at://star',
      cid: 'star-cid',
      author_did: 'did:plc:alice',
      repository_uri: 'at://repo',
      repository_cid: 'repo-cid',
      record_created_at: '2026-08-10T10:00:00Z',
      indexed_at: '2026-08-10T10:00:00Z',
    })
    const issue = zSyncIssue.parse({
      uri: 'at://issue',
      cid: 'issue-cid',
      author_did: 'did:plc:bob',
      repository_uri: 'at://repo',
      repository_cid: 'repo-cid',
      title: 'Issue',
      body: '',
      state: 'open',
      comment_count: 0,
      record_created_at: '2026-08-10T11:00:00Z',
      record_updated_at: '2026-08-10T11:00:00Z',
      indexed_at: '2026-08-10T11:00:00Z',
    })

    expect(composeRouteActivity({ stars: [star], issues: [issue] }, 2)).toEqual([
      expect.objectContaining({ kind: 'issue', uri: 'at://issue', subjectUri: 'at://repo' }),
      expect.objectContaining({ kind: 'star', uri: 'at://star', subjectUri: 'at://repo' }),
    ])
  })

  it('bounds composed activity even when a caller requests more', () => {
    const stars = Array.from({ length: 60 }, (_, index) =>
      zSyncStar.parse({
        uri: `at://star/${index}`,
        cid: `cid-${index}`,
        author_did: 'did:plc:alice',
        repository_uri: 'at://repo',
        repository_cid: 'repo-cid',
        record_created_at: new Date(index * 1000).toISOString(),
        indexed_at: new Date(index * 1000).toISOString(),
      }),
    )

    expect(composeRouteActivity({ stars }, 1_000)).toHaveLength(50)
  })
})
