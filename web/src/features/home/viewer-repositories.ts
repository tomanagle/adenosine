import type { Repository } from '@adenosine/api-client'

export type RepositoryParams = { owner: string; repo: string }

/** Route params prefer the handle so shared links stay readable. */
export function repositoryParams(repository: Repository): RepositoryParams {
  return {
    owner: repository.owner.organization_slug ?? repository.owner.handle ?? repository.owner.did,
    repo: repository.slug,
  }
}

export function repositoryKey(repository: Repository): string {
  return repository.uri ?? repository.id ?? `${repository.owner.did}/${repository.slug}`
}

/**
 * The REST network list is the only repository listing this server exposes, so
 * the viewer's own repositories are the owned subset of that projection.
 */
export function ownedRepositories(repositories: Repository[], did: string | undefined) {
  if (!did) return []
  return repositories
    .filter((repository) => repository.owner.did === did)
    .sort((first, second) => Date.parse(second.updated_at) - Date.parse(first.updated_at))
}

export function filterRepositories(repositories: Repository[], query: string) {
  const needle = query.trim().toLowerCase()
  if (!needle) return repositories
  return repositories.filter((repository) =>
    [repository.slug, repository.display_name, repository.description].some((value) =>
      value?.toLowerCase().includes(needle),
    ),
  )
}

/**
 * Proposals can only be composed here for repositories whose Git objects this
 * server can read, because branch heads come from the local Git backend.
 */
export function proposalRepositories(repositories: Repository[]) {
  return repositories
    .filter(
      (repository) =>
        repository.hosting.local && repository.state === 'active' && Boolean(repository.uri),
    )
    .sort((first, second) => first.slug.localeCompare(second.slug))
}

export type RepositorySummary = {
  repositories: number
  openIssues: number
  openPullRequests: number
}

export function repositorySummary(repositories: Repository[]): RepositorySummary {
  return repositories.reduce<RepositorySummary>(
    (summary, repository) => ({
      repositories: summary.repositories + 1,
      openIssues: summary.openIssues + repository.open_issue_count,
      openPullRequests: summary.openPullRequests + repository.open_pull_request_count,
    }),
    { repositories: 0, openIssues: 0, openPullRequests: 0 },
  )
}

export function pluralize(value: number, singular: string, plural: string) {
  return `${value} ${value === 1 ? singular : plural}`
}

export function summarySentence(summary: RepositorySummary) {
  return [
    pluralize(summary.repositories, 'repository', 'repositories'),
    pluralize(summary.openIssues, 'open issue', 'open issues'),
    pluralize(summary.openPullRequests, 'open pull request', 'open pull requests'),
  ].join(' · ')
}
