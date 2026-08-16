import { describe, expect, it } from 'vitest'

import { shouldLoadVerifiedPullRequestDiff } from './remote-pr'

describe('shouldLoadVerifiedPullRequestDiff', () => {
  const testCases: Array<{
    name: string
    source: 'local' | 'canonical_host'
    want: boolean
  }> = [
    { name: 'local target fetches verified diff', source: 'local', want: true },
    { name: 'remote target omits verified diff', source: 'canonical_host', want: false },
  ]
  for (const testCase of testCases) {
    it(testCase.name, () =>
      expect(shouldLoadVerifiedPullRequestDiff(testCase.source)).toBe(testCase.want),
    )
  }
})
