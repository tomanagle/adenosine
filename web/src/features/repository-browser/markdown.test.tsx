import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { SafeMarkdown } from './markdown'

describe('SafeMarkdown', () => {
  it('renders conservative Markdown without interpreting repository HTML', () => {
    const html = renderToStaticMarkup(
      <SafeMarkdown
        source={'# Hello\n<img src="https://tracker.example/pixel">\n<script>alert(1)</script>'}
      />,
    )

    expect(html).toContain('&lt;img src=&quot;https://tracker.example/pixel&quot;&gt;')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<script>')
  })

  it('allows only strict external web links with safe tab behavior', () => {
    const html = renderToStaticMarkup(
      <SafeMarkdown source={'[safe](https://example.com/docs) [attack](javascript:alert(1))'} />,
    )

    expect(html).toContain('href="https://example.com/docs"')
    expect(html).toContain('rel="nofollow noopener noreferrer"')
    expect(html).not.toContain('javascript:')
    expect(html).toContain('attack')
  })
})
