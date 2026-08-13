import type { StarEnvelope, StarList } from '@adenosine/api-client'
import { describe, expect, it } from 'vitest'

import { addAcceptedStar, optimisticStarState, removeAcceptedStar } from './star-cache'

const star: StarEnvelope = {
  uri: 'at://did:plc:viewer/dev.adenosine.star/ledger',
  cid: 'bafystar',
  author_did: 'did:plc:viewer',
  repository_uri: 'at://did:plc:owner/dev.adenosine.repo/ledger',
  repository_cid: 'bafyrepository',
  created_at: '2026-08-12T00:00:00Z',
}

describe('accepted star cache', () => {
  const testCases = [
    {
      name: 'adds an accepted star before projection catches up',
      apply: () => addAcceptedStar({ items: [], page: { next_cursor: null }, star_count: 4 }, star),
      wantCount: 5,
      wantAuthors: ['did:plc:viewer'],
    },
    {
      name: 'does not duplicate an already visible author',
      apply: () =>
        addAcceptedStar(
          {
            items: [{ ...star, indexed_at: star.created_at }],
            page: { next_cursor: null },
            star_count: 5,
          },
          star,
        ),
      wantCount: 5,
      wantAuthors: ['did:plc:viewer'],
    },
    {
      name: 'removes the viewer star before projection catches up',
      apply: () =>
        removeAcceptedStar(
          {
            items: [
              { ...star, indexed_at: star.created_at },
              {
                ...star,
                author_did: 'did:plc:other',
                indexed_at: star.created_at,
                uri: 'at://did:plc:other/dev.adenosine.star/ledger',
              },
            ],
            page: { next_cursor: null },
            star_count: 5,
          },
          star.author_did,
        ),
      wantCount: 4,
      wantAuthors: ['did:plc:other'],
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      const result: StarList = testCase.apply()
      expect(result.star_count).toBe(testCase.wantCount)
      expect(result.items.map((candidate) => candidate.author_did)).toEqual(testCase.wantAuthors)
    })
  }
})

describe('optimistic star state', () => {
  const testCases = [
    {
      name: 'shows a pending star as successful immediately',
      input: { deleting: false, putting: true, starCount: 4, starred: false },
      want: { starCount: 5, starred: true },
    },
    {
      name: 'shows a pending unstar as successful immediately',
      input: { deleting: true, putting: false, starCount: 4, starred: true },
      want: { starCount: 3, starred: false },
    },
    {
      name: 'does not double count an already projected star',
      input: { deleting: false, putting: true, starCount: 4, starred: true },
      want: { starCount: 4, starred: true },
    },
    {
      name: 'never makes the visible count negative',
      input: { deleting: true, putting: false, starCount: 0, starred: true },
      want: { starCount: 0, starred: false },
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(optimisticStarState(testCase.input)).toEqual(testCase.want)
    })
  }
})
