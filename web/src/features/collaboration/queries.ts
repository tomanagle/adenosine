import {
  createIssueCommentMutation,
  createIssueMutation,
  createPullRequestMutation,
  createPullRequestReviewMutation,
  getIssueCommentsOptions,
  getIssueOptions,
  getIssueTriageOptions,
  getIssuesOptions,
  getPullRequestDiffOptions,
  getPullRequestOptions,
  getPullRequestTriageOptions,
  getStarsOptions,
  listPullRequestReviewsOptions,
  listPullRequestsOptions,
  listRepositoryLabelsOptions,
  listRepositoryMilestonesOptions,
  mergePullRequestMutation,
  putIssueStatusMutation,
  putPullRequestStatusMutation,
  putIssueTriageMutation,
  putPullRequestTriageMutation,
  searchProfilesOptions,
  createRepositoryLabelMutation,
  updateRepositoryLabelMutation,
  deleteRepositoryLabelMutation,
  createRepositoryMilestoneMutation,
  updateRepositoryMilestoneMutation,
  deleteRepositoryMilestoneMutation,
} from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export type CollaborationFilters = {
  state?: 'open' | 'closed' | 'merged'
  label?: string
  assignee?: string
  milestone?: string
}

export const issuesQueryOptions = (repositoryUri: string, filters: CollaborationFilters = {}) =>
  getIssuesOptions({
    client: browserApiClient,
    query: {
      repository_uri: repositoryUri,
      state: filters.state === 'merged' ? undefined : filters.state,
      label: filters.label,
      assignee: filters.assignee,
      milestone: filters.milestone,
    },
  })
export const issueQueryOptions = (repositoryUri: string, issueUri: string) =>
  getIssueOptions({
    client: browserApiClient,
    query: { repository_uri: repositoryUri, issue_uri: issueUri },
  })
export const commentsQueryOptions = (issueUri: string) =>
  getIssueCommentsOptions({ client: browserApiClient, query: { issue_uri: issueUri } })
export const pullRequestsQueryOptions = (
  repositoryUri: string,
  filters: CollaborationFilters = {},
) =>
  listPullRequestsOptions({
    client: browserApiClient,
    query: { repository_uri: repositoryUri, ...filters },
  })
export const pullRequestQueryOptions = (pullRequestUri: string) =>
  getPullRequestOptions({ client: browserApiClient, query: { pull_request_uri: pullRequestUri } })
export const pullRequestDiffQueryOptions = (pullRequestUri: string) =>
  getPullRequestDiffOptions({
    client: browserApiClient,
    query: { pull_request_uri: pullRequestUri },
  })
export const reviewsQueryOptions = (pullRequestUri: string) =>
  listPullRequestReviewsOptions({
    client: browserApiClient,
    query: { pull_request_uri: pullRequestUri },
  })
export const activityStarsQueryOptions = (repositoryUri: string) =>
  getStarsOptions({ client: browserApiClient, query: { repository_uri: repositoryUri } })

export const createIssueMutationOptions = () => createIssueMutation({ client: browserApiClient })
export const issueStatusMutationOptions = () => putIssueStatusMutation({ client: browserApiClient })
export const createCommentMutationOptions = () =>
  createIssueCommentMutation({ client: browserApiClient })
export const createPullMutationOptions = () =>
  createPullRequestMutation({ client: browserApiClient })
export const createReviewMutationOptions = () =>
  createPullRequestReviewMutation({ client: browserApiClient })
export const pullStatusMutationOptions = () =>
  putPullRequestStatusMutation({ client: browserApiClient })
export const mergeMutationOptions = () => mergePullRequestMutation({ client: browserApiClient })

export const repositoryLabelsQueryOptions = (owner: string, repo: string) =>
  listRepositoryLabelsOptions({
    client: browserApiClient,
    path: { owner, repo },
    query: { limit: 100 },
  })
export const repositoryMilestonesQueryOptions = (owner: string, repo: string) =>
  listRepositoryMilestonesOptions({
    client: browserApiClient,
    path: { owner, repo },
    query: { limit: 100 },
  })
export const issueTriageQueryOptions = (owner: string, repo: string, encodedSubject: string) =>
  getIssueTriageOptions({
    client: browserApiClient,
    path: { owner, repo, subject: encodedSubject },
  })
export const pullRequestTriageQueryOptions = (
  owner: string,
  repo: string,
  encodedSubject: string,
) =>
  getPullRequestTriageOptions({
    client: browserApiClient,
    path: { owner, repo, subject: encodedSubject },
  })

export const createRepositoryLabelMutationOptions = () =>
  createRepositoryLabelMutation({ client: browserApiClient })
export const updateRepositoryLabelMutationOptions = () =>
  updateRepositoryLabelMutation({ client: browserApiClient })
export const deleteRepositoryLabelMutationOptions = () =>
  deleteRepositoryLabelMutation({ client: browserApiClient })
export const createRepositoryMilestoneMutationOptions = () =>
  createRepositoryMilestoneMutation({ client: browserApiClient })
export const updateRepositoryMilestoneMutationOptions = () =>
  updateRepositoryMilestoneMutation({ client: browserApiClient })
export const deleteRepositoryMilestoneMutationOptions = () =>
  deleteRepositoryMilestoneMutation({ client: browserApiClient })
export const putIssueTriageMutationOptions = () =>
  putIssueTriageMutation({ client: browserApiClient })
export const putPullRequestTriageMutationOptions = () =>
  putPullRequestTriageMutation({ client: browserApiClient })
export const visibleProfilesQueryOptions = (query: string) =>
  searchProfilesOptions({
    client: browserApiClient,
    query: { q: query, sort: 'relevance', limit: 10 },
  })
