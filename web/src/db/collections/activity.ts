import type {
  SyncIssueCommentRow,
  SyncIssueRow,
  SyncPullRequestReviewRow,
  SyncPullRequestRow,
  SyncStarRow,
} from '@adenosine/api-client/schemas'

export type RouteActivityItem = {
  kind: 'star' | 'issue' | 'issue-comment' | 'pull-request' | 'pull-request-review'
  uri: string
  cid: string
  actorDid: string
  subjectUri: string
  indexedAt: string
}

export type RouteActivitySources = Partial<{
  stars: readonly SyncStarRow[]
  issues: readonly SyncIssueRow[]
  issueComments: readonly SyncIssueCommentRow[]
  pullRequests: readonly SyncPullRequestRow[]
  pullRequestReviews: readonly SyncPullRequestReviewRow[]
}>

const MAX_ROUTE_ACTIVITY_ITEMS = 50

// Activity is a bounded client composition over approved resources. There is
// deliberately no activity shape or network.records collection.
export function composeRouteActivity(sources: RouteActivitySources, requestedLimit = 20): RouteActivityItem[] {
  const limit = Math.max(0, Math.min(Math.trunc(requestedLimit), MAX_ROUTE_ACTIVITY_ITEMS))
  const items: RouteActivityItem[] = []

  for (const star of sources.stars ?? []) {
    items.push({ kind: 'star', uri: star.uri, cid: star.cid, actorDid: star.author_did, subjectUri: star.repository_uri, indexedAt: star.indexed_at })
  }
  for (const issue of sources.issues ?? []) {
    items.push({ kind: 'issue', uri: issue.uri, cid: issue.cid, actorDid: issue.author_did, subjectUri: issue.repository_uri, indexedAt: issue.indexed_at })
  }
  for (const comment of sources.issueComments ?? []) {
    items.push({ kind: 'issue-comment', uri: comment.uri, cid: comment.cid, actorDid: comment.author_did, subjectUri: comment.issue_uri, indexedAt: comment.indexed_at })
  }
  for (const pullRequest of sources.pullRequests ?? []) {
    items.push({ kind: 'pull-request', uri: pullRequest.uri, cid: pullRequest.cid, actorDid: pullRequest.author_did, subjectUri: pullRequest.target_repository_uri, indexedAt: pullRequest.indexed_at })
  }
  for (const review of sources.pullRequestReviews ?? []) {
    items.push({ kind: 'pull-request-review', uri: review.uri, cid: review.cid, actorDid: review.author_did, subjectUri: review.pull_request_uri, indexedAt: review.indexed_at })
  }

  return [...items].sort((left, right) => right.indexedAt.localeCompare(left.indexedAt)).slice(0, limit)
}
