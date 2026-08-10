import { describe, expect, it, vi } from 'vitest'

import type { RouteElectricCollection, RouteElectricResource } from './route-electric'
import {
  createRouteCollectionLifecycle,
  tryCreateRouteCollectionLifecycle,
} from './route-lifecycle'

describe('route collection lifecycle', () => {
  it('constructs only requested resources and cleans all of them up', async () => {
    const created: string[] = []
    const cleanups: Array<ReturnType<typeof vi.fn>> = []
    const factory = <R extends RouteElectricResource>(scope: string, resource: R) => {
      created.push(`${scope}:${resource}`)
      const cleanup = vi.fn(async () => {})
      cleanups.push(cleanup)
      return { cleanup } as unknown as RouteElectricCollection<R>
    }

    const lifecycle = createRouteCollectionLifecycle(
      'home',
      ['repositories', 'profiles'] as const,
      factory,
    )
    expect(created).toEqual(['home:repositories', 'home:profiles'])

    await lifecycle.cleanup()
    expect(cleanups.every((cleanup) => cleanup.mock.calls.length === 1)).toBe(true)
  })

  it('contains Electric construction errors so the REST-backed route can remain usable', () => {
    const electricFailure = new Error('Electric unavailable')
    const factory = <R extends RouteElectricResource>(_scope: string, _resource: R) => {
      throw electricFailure
    }

    const result = tryCreateRouteCollectionLifecycle('home', ['repositories'] as const, factory)
    expect(result.lifecycle).toBeUndefined()
    expect(result.error).toBe(electricFailure)
  })
})
