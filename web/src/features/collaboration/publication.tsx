import { useEffect, useState } from 'react'

import { PublicationReconciler, type PublicationState } from '@/db/publication-reconciler'

export function usePublication(options?: { delayMs?: number; pollMs?: number }) {
  const [reconciler] = useState(() => new PublicationReconciler<unknown>(options?.delayMs))
  const [state, setState] = useState<PublicationState | undefined>()

  useEffect(() => {
    const unsubscribe = reconciler.subscribe(() => setState(reconciler.values().at(-1)?.state))
    return () => {
      unsubscribe()
      reconciler.cleanup()
    }
  }, [reconciler])

  async function publish(
    request: () => Promise<{ uri: string; cid: string }>,
    projected: (reference: { uri: string; cid: string }) => Promise<boolean>,
  ) {
    const id = crypto.randomUUID()
    const reference = await reconciler.publish({
      id,
      optimistic: {},
      request,
      rollback: () => undefined,
    })
    const deadline = Date.now() + (options?.delayMs ?? 10_000)
    while (Date.now() < deadline) {
      try {
        if (await projected(reference)) {
          reconciler.observe(reference)
          break
        }
      } catch {
        // Publication remains authoritative even while the local read path is unavailable.
      }
      await wait(options?.pollMs ?? 500)
    }
    return reference
  }

  return { publish, state }
}

function wait(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))
}

export function PublicationNotice({ state }: { state?: PublicationState }) {
  if (!state) return null
  return (
    <output className="block text-xs text-muted-foreground">
      {state === 'publishing'
        ? 'Published; waiting for this instance to index the record.'
        : state === 'sync_delayed'
          ? 'Sync delayed. Publication succeeded and will appear when indexing catches up.'
          : 'Indexed.'}
    </output>
  )
}
