import { z } from 'zod'

const recordKey = z
  .string()
  .min(1)
  .max(512)
  .regex(/^[A-Za-z0-9._:~-]+$/)

export const issueFiltersSchema = z.object({
  state: z.enum(['open', 'closed']).optional(),
  label: recordKey.optional(),
  assignee: z.string().min(1).max(2048).startsWith('did:').optional(),
  milestone: recordKey.optional(),
})

export const pullRequestFiltersSchema = z.object({
  state: z.enum(['open', 'closed', 'merged']).optional(),
  label: recordKey.optional(),
  assignee: z.string().min(1).max(2048).startsWith('did:').optional(),
  milestone: recordKey.optional(),
})

export type IssueFilters = z.infer<typeof issueFiltersSchema>
export type PullRequestFilters = z.infer<typeof pullRequestFiltersSchema>
