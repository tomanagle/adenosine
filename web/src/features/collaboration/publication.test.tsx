// @vitest-environment jsdom

import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { usePublication } from './publication'

afterEach(() => vi.useRealTimers())

describe('usePublication', () => {
  const testCases = [
    { name: 'observes matching projection', projected: [false, true], wantState: 'indexed' },
    {
      name: 'settles as delayed when projection never appears',
      projected: [false, false, false],
      wantState: 'sync_delayed',
    },
  ] as const

  for (const testCase of testCases) {
    it(testCase.name, async () => {
      vi.useFakeTimers()
      const checks = [...testCase.projected]
      const { result } = renderHook(() => usePublication({ delayMs: 100, pollMs: 25 }))
      let publishing: Promise<unknown> | undefined
      act(() => {
        publishing = result.current.publish(
          async () => ({ uri: 'at://did:plc:a/dev.adenosine.issue/x', cid: 'cid' }),
          async () => checks.shift() ?? false,
        )
      })
      await act(async () => vi.advanceTimersByTimeAsync(150))
      await publishing
      expect(result.current.state).toBe(testCase.wantState)
    })
  }
})
