import { describe, expect, it } from 'vitest'

import { apiErrorMessage } from './api-error'

describe('apiErrorMessage', () => {
  const testCases = [
    {
      name: 'reads the API error envelope',
      error: {
        error: {
          code: 'conflict',
          message: 'A repository with that name exists.',
          request_id: 'r',
        },
      },
      want: 'A repository with that name exists.',
    },
    {
      name: 'falls back to a thrown transport error',
      error: new Error('Failed to fetch'),
      want: 'Failed to fetch',
    },
    { name: 'uses the fallback for unknown shapes', error: { status: 500 }, want: 'Fallback' },
    {
      name: 'uses the fallback for an empty envelope message',
      error: { error: {} },
      want: 'Fallback',
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(apiErrorMessage(testCase.error, 'Fallback')).toBe(testCase.want)
    })
  }
})
