import { useEffect, useState } from 'react'

import {
  createRouteElectricCollection,
  type RouteElectricCollection,
  type RouteElectricResource,
} from './route-electric'

export type RouteElectricCollections<R extends readonly RouteElectricResource[]> = {
  [K in R[number]]: RouteElectricCollection<K>
}

type CollectionFactory = <R extends RouteElectricResource>(
  scope: string,
  resource: R,
) => RouteElectricCollection<R>

export type RouteCollectionLifecycle<R extends readonly RouteElectricResource[]> = {
  routeScope: string
  resources: R
  collections: RouteElectricCollections<R>
  cleanup: () => Promise<void>
}

export function createRouteCollectionLifecycle<R extends readonly RouteElectricResource[]>(
  routeScope: string,
  resources: R,
  factory: CollectionFactory = createRouteElectricCollection,
): RouteCollectionLifecycle<R> {
  const collections: Record<string, unknown> = {}
  const created: Array<{ cleanup: () => Promise<void> }> = []

  try {
    for (const resource of new Set(resources)) {
      const collection = factory(routeScope, resource)
      collections[resource] = collection
      created.push(collection)
    }
  } catch (error) {
    for (const collection of created) void collection.cleanup()
    throw error
  }

  return {
    routeScope,
    resources,
    collections: collections as RouteElectricCollections<R>,
    cleanup: async () => {
      await Promise.all(created.map((collection) => collection.cleanup()))
    },
  }
}

type OwnedCollections<R extends readonly RouteElectricResource[]> = {
  key: string
  lifecycle?: RouteCollectionLifecycle<R>
  error?: Error
}

export function tryCreateRouteCollectionLifecycle<R extends readonly RouteElectricResource[]>(
  routeScope: string,
  resources: R,
  factory: CollectionFactory = createRouteElectricCollection,
): OwnedCollections<R> {
  const key = `${routeScope}\u0000${resources.join('\u0000')}`
  try {
    return { key, lifecycle: createRouteCollectionLifecycle(routeScope, resources, factory) }
  } catch (error) {
    return { key, error: error instanceof Error ? error : new Error(String(error)) }
  }
}

export function useRouteElectricCollections<R extends readonly RouteElectricResource[]>(
  routeScope: string,
  resources: R,
) {
  const key = `${routeScope}\u0000${resources.join('\u0000')}`
  const [owned, setOwned] = useState<OwnedCollections<R> | null>(null)

  useEffect(() => {
    const next = tryCreateRouteCollectionLifecycle(routeScope, resources)
    setOwned(next)
    return () => {
      if (next.lifecycle) void next.lifecycle.cleanup()
    }
    // The stable value key deliberately controls collection ownership; array identity must not.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  if (owned?.key !== key) return { collections: null, error: null }
  return { collections: owned.lifecycle?.collections ?? null, error: owned.error ?? null }
}
