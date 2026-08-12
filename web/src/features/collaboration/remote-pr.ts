export function shouldLoadVerifiedPullRequestDiff(sourceBrowsing: 'local' | 'canonical_host') {
  return sourceBrowsing === 'local'
}
