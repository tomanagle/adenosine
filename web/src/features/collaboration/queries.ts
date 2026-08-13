import {
  createIssueCommentMutation,
  createIssueMutation,
  createPullRequestMutation,
  createPullRequestReviewMutation,
  getIssueCommentsOptions,
  getIssueOptions,
  getIssuesOptions,
  getPullRequestDiffOptions,
  getPullRequestOptions,
  getStarsOptions,
  listPullRequestReviewsOptions,
  listPullRequestReviewRequestsOptions,
  listPullRequestsOptions,
  mergePullRequestMutation,
  putIssueStatusMutation,
  putPullRequestStatusMutation,
  putPullRequestReviewRequestMutation,
  deletePullRequestReviewRequestMutation,
} from '@adenosine/api-client/query'

import { browserApiClient } from '@/api/browser-client'

export const issuesQueryOptions = (repositoryUri: string) =>
  getIssuesOptions({ client: browserApiClient, query: { repository_uri: repositoryUri } })
export const issueQueryOptions = (repositoryUri: string, issueUri: string) =>
  getIssueOptions({
    client: browserApiClient,
    query: { repository_uri: repositoryUri, issue_uri: issueUri },
  })
export const commentsQueryOptions = (issueUri: string) =>
  getIssueCommentsOptions({ client: browserApiClient, query: { issue_uri: issueUri } })
export const pullRequestsQueryOptions = (repositoryUri: string) =>
  listPullRequestsOptions({ client: browserApiClient, query: { repository_uri: repositoryUri } })
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
export const reviewRequestsQueryOptions = (pullRequestUri: string) =>
  listPullRequestReviewRequestsOptions({
    client: browserApiClient,
    query: { pull_request_uri: pullRequestUri, limit: 100 },
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
export const putReviewRequestMutationOptions = () =>
  putPullRequestReviewRequestMutation({ client: browserApiClient })
export const deleteReviewRequestMutationOptions = () =>
  deletePullRequestReviewRequestMutation({ client: browserApiClient })
export const pullStatusMutationOptions = () =>
  putPullRequestStatusMutation({ client: browserApiClient })
export const mergeMutationOptions = () => mergePullRequestMutation({ client: browserApiClient })
