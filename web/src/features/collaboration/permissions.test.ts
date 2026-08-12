import { describe, expect, it } from 'vitest'

import { canTriageIssue } from './permissions'

describe('canTriageIssue', () => {
  const testCases = [
    { name: 'local owner', local: true, viewer: 'did:plc:alice', want: true },
    { name: 'remote owner', local: false, viewer: 'did:plc:alice', want: false },
    { name: 'local non-owner', local: true, viewer: 'did:plc:bob', want: false },
  ]
  for (const testCase of testCases) {
    it(testCase.name, () =>
      expect(
        canTriageIssue({
          local: testCase.local,
          ownerDid: 'did:plc:alice',
          viewerDid: testCase.viewer,
        }),
      ).toBe(testCase.want),
    )
  }
})
