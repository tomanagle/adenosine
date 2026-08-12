import { describe, expect, it } from 'vitest'

import { decodeRecordIdentity, didSchema, encodeRecordIdentity } from './identity'

describe('collaboration identity', () => {
  const testCases = [
    { name: 'canonical DID', value: 'did:plc:alice', valid: true },
    { name: 'handle is not authority', value: 'alice.test', valid: false },
    { name: 'path injection', value: 'did:plc:alice/../../admin', valid: false },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(didSchema.safeParse(testCase.value).success).toBe(testCase.valid)
    })
  }

  it('round trips a stable AT URI through one route segment', () => {
    const uri = 'at://did:plc:alice/dev.adenosine.issue/3kexample'
    expect(decodeRecordIdentity(encodeRecordIdentity(uri))).toBe(uri)
  })
})
