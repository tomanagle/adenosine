import type { z } from 'zod'
import {
  zSyncIssue,
  zSyncIssueComment,
  zSyncProfile,
  zSyncPullRequest,
  zSyncPullRequestReview,
  zSyncRepository,
  zSyncStar,
} from './generated/zod.gen'

export * from './generated/zod.gen'

// Electric parses PostgreSQL int8 counters to bigint; this is the validated
// collection-row type rather than the JSON wire type exported by the SDK.
export type SyncRepositoryRow = z.output<typeof zSyncRepository>
export type SyncProfileRow = z.output<typeof zSyncProfile>
export type SyncStarRow = z.output<typeof zSyncStar>
export type SyncIssueRow = z.output<typeof zSyncIssue>
export type SyncIssueCommentRow = z.output<typeof zSyncIssueComment>
export type SyncPullRequestRow = z.output<typeof zSyncPullRequest>
export type SyncPullRequestReviewRow = z.output<typeof zSyncPullRequestReview>
