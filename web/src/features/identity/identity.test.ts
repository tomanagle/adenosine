import { describe, expect, it } from 'vitest'

import { classifyIdentityResult } from './identity'

describe('classifyIdentityResult', () => {
  it('classifies a 401 as anonymous', () => {
    expect(classifyIdentityResult({ response: new Response(null, { status: 401 }) })).toBeNull()
  })

  it('validates an authenticated identity with the generated schema', () => {
    expect(
      classifyIdentityResult({ data: { did: 'did:plc:alice', handle: 'alice.test' } }),
    ).toEqual({
      did: 'did:plc:alice',
      handle: 'alice.test',
    })
  })

  it('rejects malformed successful data', () => {
    expect(() => classifyIdentityResult({ data: { handle: 'missing-did.test' } })).toThrow()
  })
})
