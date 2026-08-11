import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { DiffPanel } from './repository-diff'

describe('DiffPanel', () => {
  it('renders semantic file metadata, binary state, and textual line labels', () => {
    const html = renderToStaticMarkup(
      <DiffPanel
        diff={{
          base_sha: 'a'.repeat(40),
          head_sha: 'b'.repeat(40),
          files: [
            {
              status: 'M',
              old_path: 'image.png',
              new_path: 'image.png',
              additions: null,
              deletions: null,
            },
          ],
          patch: '@@ -1 +1 @@\n-old\n+new',
        }}
      />,
    )

    expect(html).toContain('<table')
    expect(html).toContain('binary')
    expect(html).toContain('-old')
    expect(html).toContain('+new')
    expect(html).toContain('<textarea')
    expect(html).toContain('readOnly=""')
  })
})
