import { describe, expect, it } from 'vitest'

import { shouldLoadVerifiedPullRequestDiff } from './remote-pr'

describe('shouldLoadVerifiedPullRequestDiff', () => {
  const testCases = [
    { name: 'local target fetches verified diff', source: 'local', want: true },
    { name: 'remote target omits verified diff', source: 'canonical_host', want: false },
  ]
  for (const testCase of testCases) {
    it(testCase.name, () =>
      expect(shouldLoadVerifiedPullRequestDiff(testCase.source as 'local' | 'canonical_host')).toBe(
        testCase.want,
      ),
    )
  }
})
