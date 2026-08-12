import { describe, expect, it } from 'vitest'

import { parseExploreSearch } from './explore-search'
import { profileSearchQueryOptions, repositorySearchQueryOptions } from './explore.query'

describe('explore URL search', () => {
  it.each([
    {
      name: 'defaults absent values',
      input: {},
      expected: { q: '', type: 'repositories', sort: 'relevance' },
    },
    {
      name: 'preserves valid state',
      input: { q: ' forge ', type: 'profiles', sort: 'recent', cursor: 'next' },
      expected: { q: 'forge', type: 'profiles', sort: 'recent', cursor: 'next' },
    },
    {
      name: 'recovers unsupported filters',
      input: { q: 'forge', type: 'issues', sort: 'popular' },
      expected: { q: 'forge', type: 'repositories', sort: 'relevance' },
    },
    {
      name: 'drops oversized cursor',
      input: { q: 'forge', cursor: 'x'.repeat(4097) },
      expected: { q: 'forge', type: 'repositories', sort: 'relevance' },
    },
  ])('$name', ({ input, expected }) => {
    expect(parseExploreSearch(input)).toEqual(expected)
  })
})

describe('explore query cache identity', () => {
  it('includes operation, query, sort, and cursor', () => {
    const base = parseExploreSearch({ q: 'forge' })
    const repositoryKey = repositorySearchQueryOptions(base).queryKey
    const nextRepositoryKey = repositorySearchQueryOptions({ ...base, cursor: 'next' }).queryKey
    const recentRepositoryKey = repositorySearchQueryOptions({ ...base, sort: 'recent' }).queryKey
    const profileKey = profileSearchQueryOptions({ ...base, type: 'profiles' }).queryKey

    expect(repositoryKey).not.toEqual(nextRepositoryKey)
    expect(repositoryKey).not.toEqual(recentRepositoryKey)
    expect(repositoryKey).not.toEqual(profileKey)
  })
})
