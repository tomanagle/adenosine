import { afterEach, describe, expect, it, vi } from 'vitest'

import { PublicationReconciler } from './publication-reconciler'

afterEach(() => {
  vi.useRealTimers()
})

describe('PublicationReconciler', () => {
  it('moves a successful ATProto write from publishing to indexed by URI and CID', async () => {
    vi.useFakeTimers()
    const reconciler = new PublicationReconciler<{ title: string }>(1_000)
    const rollback = vi.fn()

    await reconciler.publish({
      id: 'new-issue',
      optimistic: { title: 'Issue' },
      request: async () => ({ uri: 'at://issue', cid: 'cid-1' }),
      rollback,
    })
    expect(reconciler.get('new-issue')?.state).toBe('publishing')
    expect(reconciler.observe({ uri: 'at://issue', cid: 'wrong-cid' })).toBe(false)
    expect(reconciler.observe({ uri: 'at://issue', cid: 'cid-1' })).toBe(true)
    expect(reconciler.get('new-issue')?.state).toBe('indexed')
    expect(rollback).not.toHaveBeenCalled()
    reconciler.cleanup()
  })

  it('marks successful publication as sync delayed without rolling it back', async () => {
    vi.useFakeTimers()
    const reconciler = new PublicationReconciler(1_000)
    const rollback = vi.fn()

    await reconciler.publish({
      id: 'new-star',
      optimistic: {},
      request: async () => ({ uri: 'at://star', cid: 'cid-1' }),
      rollback,
    })
    await vi.advanceTimersByTimeAsync(1_000)
    expect(reconciler.get('new-star')?.state).toBe('sync_delayed')
    expect(rollback).not.toHaveBeenCalled()
    reconciler.cleanup()
  })

  it('reconciles an Electric observation that arrives before the REST response', async () => {
    let resolveRequest: ((reference: { uri: string; cid: string }) => void) | undefined
    const request = new Promise<{ uri: string; cid: string }>((resolve) => { resolveRequest = resolve })
    const reconciler = new PublicationReconciler(1_000)
    const publishing = reconciler.publish({
      id: 'fast-index',
      optimistic: {},
      request: () => request,
      rollback: vi.fn(),
    })

    expect(reconciler.observe({ uri: 'at://fast', cid: 'cid-fast' })).toBe(false)
    resolveRequest?.({ uri: 'at://fast', cid: 'cid-fast' })
    await publishing
    expect(reconciler.get('fast-index')?.state).toBe('indexed')
    reconciler.cleanup()
  })

  it('rolls back only when the authoritative REST request fails', async () => {
    const reconciler = new PublicationReconciler(1_000)
    const rollback = vi.fn()
    const failure = new Error('REST rejected publication')

    await expect(reconciler.publish({
      id: 'new-comment',
      optimistic: {},
      request: async () => { throw failure },
      rollback,
    })).rejects.toBe(failure)
    expect(reconciler.get('new-comment')).toBeUndefined()
    expect(rollback).toHaveBeenCalledOnce()
  })
})
