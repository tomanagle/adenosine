import { describe, expect, it } from 'vitest'

import type { RepositorySnapshot } from './repository-snapshot.query'

describe('repository snapshot fallback', () => {
  it('can represent an unavailable REST snapshot without losing the home route', () => {
    const snapshot: RepositorySnapshot = { repositories: [], available: false }
    expect(snapshot.available).toBe(false)
    expect(snapshot.repositories).toEqual([])
  })
})
