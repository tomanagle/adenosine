export type PublicationState = 'publishing' | 'indexed' | 'sync_delayed'

export type PublicationReference = {
  uri: string
  cid: string
}

export type Publication<T> = {
  id: string
  optimistic: T
  state: PublicationState
  reference?: PublicationReference
}

export type PublishPublication<T> = {
  id: string
  optimistic: T
  request: () => Promise<PublicationReference>
  rollback: () => void
}

export class PublicationReconciler<T> {
  readonly #delayMs: number
  readonly #publications = new Map<string, Publication<T>>()
  readonly #timers = new Map<string, ReturnType<typeof setTimeout>>()
  readonly #observedReferences = new Set<string>()
  readonly #listeners = new Set<() => void>()

  constructor(delayMs = 10_000) {
    if (!Number.isFinite(delayMs) || delayMs < 0)
      throw new Error('Publication sync delay must be non-negative')
    this.#delayMs = delayMs
  }

  get(id: string) {
    return this.#publications.get(id)
  }

  values() {
    return [...this.#publications.values()]
  }

  subscribe(listener: () => void) {
    this.#listeners.add(listener)
    return () => this.#listeners.delete(listener)
  }

  async publish(publication: PublishPublication<T>): Promise<PublicationReference> {
    if (this.#publications.has(publication.id))
      throw new Error(`Publication already exists: ${publication.id}`)
    this.#publications.set(publication.id, {
      id: publication.id,
      optimistic: publication.optimistic,
      state: 'publishing',
    })
    this.#notify()

    let reference: PublicationReference
    try {
      reference = await publication.request()
    } catch (error) {
      this.#publications.delete(publication.id)
      this.#notify()
      publication.rollback()
      throw error
    }

    const current = this.#publications.get(publication.id)
    if (!current) return reference
    const referenceKey = this.#referenceKey(reference)
    if (this.#observedReferences.delete(referenceKey)) {
      this.#publications.set(publication.id, { ...current, reference, state: 'indexed' })
      this.#notify()
      return reference
    }
    this.#publications.set(publication.id, { ...current, reference })
    this.#notify()
    this.#timers.set(
      publication.id,
      setTimeout(() => {
        const pending = this.#publications.get(publication.id)
        if (pending?.state === 'publishing') {
          this.#publications.set(publication.id, { ...pending, state: 'sync_delayed' })
          this.#notify()
        }
        this.#timers.delete(publication.id)
      }, this.#delayMs),
    )
    return reference
  }

  observe(reference: PublicationReference) {
    for (const [id, publication] of this.#publications) {
      if (
        publication.reference?.uri !== reference.uri ||
        publication.reference.cid !== reference.cid
      )
        continue
      const timer = this.#timers.get(id)
      if (timer) clearTimeout(timer)
      this.#timers.delete(id)
      this.#publications.set(id, { ...publication, state: 'indexed' })
      this.#notify()
      return true
    }
    const referenceKey = this.#referenceKey(reference)
    this.#observedReferences.add(referenceKey)
    if (this.#observedReferences.size > 100) {
      const oldest = this.#observedReferences.values().next().value
      if (oldest) this.#observedReferences.delete(oldest)
    }
    return false
  }

  cleanup() {
    for (const timer of this.#timers.values()) clearTimeout(timer)
    this.#timers.clear()
    this.#observedReferences.clear()
    this.#publications.clear()
    this.#listeners.clear()
  }

  #referenceKey(reference: PublicationReference) {
    return `${reference.uri}\u0000${reference.cid}`
  }

  #notify() {
    for (const listener of this.#listeners) listener()
  }
}
