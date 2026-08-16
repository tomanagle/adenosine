import { z } from 'zod'

export const exploreSearchSchema = z.object({
  q: z.string().trim().max(200).catch('').default(''),
  type: z.enum(['repositories', 'profiles']).catch('repositories').default('repositories'),
  sort: z.enum(['relevance', 'recent']).catch('relevance').default('relevance'),
  cursor: z.string().max(4096).optional().catch(undefined),
})

export type ExploreSearch = z.infer<typeof exploreSearchSchema>

export function parseExploreSearch<T>(value: T): ExploreSearch {
  return exploreSearchSchema.parse(value)
}
