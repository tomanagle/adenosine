import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { RepositoryError, classifyBrowserError } from './states'

describe('repository browser errors', () => {
  it('classifies documented missing, forbidden, unavailable, and bounded states', () => {
    expect(classifyBrowserError({ response: { status: 404 } })).toBe('missing')
    expect(classifyBrowserError({ status: 403 })).toBe('forbidden')
    expect(classifyBrowserError({ response: { status: 503 } })).toBe('unavailable')
    expect(classifyBrowserError({ error: { code: 'authentication_required' } })).toBe('forbidden')
    expect(classifyBrowserError({ error: { code: 'permission_denied' } })).toBe('forbidden')
    expect(classifyBrowserError({ error: { code: 'git_output_too_large' } })).toBe('oversized')
  })

  it('renders a non-color-only bounded diff explanation', () => {
    const html = renderToStaticMarkup(
      <RepositoryError error={{ error: { code: 'git_output_too_large' } }} />,
    )
    expect(html).toContain('Diff exceeds the safe display limit')
    expect(html).toContain('incomplete patch')
    expect(html).toContain('role="alert"')
  })
})
