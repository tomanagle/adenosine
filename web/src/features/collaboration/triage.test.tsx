// @vitest-environment jsdom

import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { issuesQueryOptions, pullRequestsQueryOptions } from './queries'
import { LabelBadge } from './triage-management'
import { issueFiltersSchema, pullRequestFiltersSchema } from './validation'

describe('triage URL filters', () => {
  it.each([
    {
      name: 'accepts issue workflow filters',
      schema: issueFiltersSchema,
      input: { state: 'closed', label: 'bug', assignee: 'did:plc:alice', milestone: 'v1' },
      success: true,
    },
    {
      name: 'rejects merged issue state',
      schema: issueFiltersSchema,
      input: { state: 'merged' },
      success: false,
    },
    {
      name: 'accepts merged pull request state',
      schema: pullRequestFiltersSchema,
      input: { state: 'merged' },
      success: true,
    },
    {
      name: 'rejects non-DID assignee',
      schema: pullRequestFiltersSchema,
      input: { assignee: 'alice' },
      success: false,
    },
  ])('$name', ({ schema, input, success }) => {
    expect(schema.safeParse(input).success).toBe(success)
  })
})

describe('triage query cache identity', () => {
  it('binds every workflow filter and resource kind into the key', () => {
    const repositoryUri = 'at://did:plc:alice/dev.adenosine.repo/project'
    const base = issuesQueryOptions(repositoryUri).queryKey
    const label = issuesQueryOptions(repositoryUri, { label: 'bug' }).queryKey
    const assignee = issuesQueryOptions(repositoryUri, { assignee: 'did:plc:bob' }).queryKey
    const milestone = issuesQueryOptions(repositoryUri, { milestone: 'v1' }).queryKey
    const pulls = pullRequestsQueryOptions(repositoryUri).queryKey

    expect(base).not.toEqual(label)
    expect(base).not.toEqual(assignee)
    expect(base).not.toEqual(milestone)
    expect(base).not.toEqual(pulls)
  })
})

describe('label presentation', () => {
  it('renders the portable name and color without injecting markup', () => {
    render(<LabelBadge label={{ color: 'd73a4a', name: '<script>bug</script>' }} />)

    const badge = screen.getByText('<script>bug</script>')
    expect(badge.getAttribute('style')).toContain('background-color: rgba(215, 58, 74, 0.133)')
    expect(badge.getAttribute('style')).toContain('border-color: rgba(215, 58, 74, 0.533)')
    expect((badge as HTMLElement).style.color).toBe('rgb(215, 58, 74)')
    expect(document.querySelector('script')).toBeNull()
  })
})
