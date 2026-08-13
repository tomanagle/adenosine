import type { StarEnvelope, StarList } from '@adenosine/api-client'

export function optimisticStarState({
  deleting,
  putting,
  starCount,
  starred,
}: {
  deleting: boolean
  putting: boolean
  starCount: number
  starred: boolean
}): { starCount: number; starred: boolean } {
  if (putting && !starred) return { starCount: starCount + 1, starred: true }
  if (deleting && starred) return { starCount: Math.max(0, starCount - 1), starred: false }
  return { starCount, starred }
}

export function addAcceptedStar(projection: StarList, star: StarEnvelope): StarList {
  if (projection.items.some((candidate) => candidate.author_did === star.author_did)) {
    return projection
  }

  return {
    items: [{ ...star, indexed_at: star.created_at }, ...projection.items],
    page: projection.page,
    star_count: projection.star_count + 1,
  }
}

export function removeAcceptedStar(projection: StarList, authorDid: string): StarList {
  const items = projection.items.filter((star) => star.author_did !== authorDid)
  if (items.length === projection.items.length) return projection

  return { items, page: projection.page, star_count: Math.max(0, projection.star_count - 1) }
}
