import { describe, expect, it } from 'vitest'

import { fieldErrorMessage } from './form'

describe('fieldErrorMessage', () => {
  const testCases = [
    { name: 'no errors', errors: [], submitted: true, want: undefined },
    { name: 'undefined errors', errors: undefined, submitted: true, want: undefined },
    { name: 'string validator output', errors: ['Required'], submitted: true, want: 'Required' },
    {
      name: 'standard schema issue',
      errors: [{ message: 'Too small: expected string to have >=1 characters' }],
      submitted: true,
      want: 'Too small: expected string to have >=1 characters',
    },
    {
      name: 'first usable schema message wins',
      errors: [undefined, { message: '' }, { message: 'Invalid slug' }],
      submitted: true,
      want: 'Invalid slug',
    },
    {
      name: 'a field validator message is preferred over schema issue text',
      errors: [{ message: 'Invalid string: must match pattern' }, 'Enter a repository name.'],
      submitted: true,
      want: 'Enter a repository name.',
    },
    {
      name: 'hidden until the field has been submitted or blurred',
      errors: ['Required'],
      submitted: false,
      want: undefined,
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      expect(fieldErrorMessage(testCase.errors, testCase.submitted)).toBe(testCase.want)
    })
  }
})
