// @vitest-environment jsdom

import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { AppShell } from './app-shell'
import { notificationsQueryOptions } from '@/features/notifications/queries'
import { createTestQueryClient, renderWithAppProviders } from '@/test/render'

afterEach(cleanup)

describe('AppShell', () => {
  const testCases = [
    {
      name: 'gives signed-in developers primary navigation and profile access',
      identity: { did: 'did:plc:viewer', handle: 'viewer.example' },
      wantHome: true,
      wantProfile: true,
      wantSignIn: false,
    },
    {
      name: 'gives visitors a clear path to sign in',
      identity: null,
      wantHome: false,
      wantProfile: false,
      wantSignIn: true,
    },
  ]

  for (const testCase of testCases) {
    it(testCase.name, () => {
      const client = createTestQueryClient()
      client.setQueryData(notificationsQueryOptions(true).queryKey, {
        items: [],
        page: { next_cursor: null },
      })
      const { container } = renderWithAppProviders(
        <AppShell identity={testCase.identity}>
          <main>Page content</main>
        </AppShell>,
        { queryClient: client },
      )

      const brandMarks = container.querySelectorAll('picture img')
      const darkBrandMarks = container.querySelectorAll(
        'picture source[media="(prefers-color-scheme: dark)"]',
      )
      expect(brandMarks.length).toBe(2)
      expect(darkBrandMarks.length).toBe(2)
      expect(brandMarks[0]?.getAttribute('src')).not.toBe(darkBrandMarks[0]?.getAttribute('srcset'))
      expect(screen.getByRole('navigation', { name: 'Primary' })).toBeTruthy()
      expect(screen.getByRole('navigation', { name: 'Footer' })).toBeTruthy()
      expect(screen.getAllByRole('link', { name: 'Explore' }).length).toBeGreaterThan(0)
      expect(Boolean(screen.queryAllByRole('link', { name: 'Home' }).length)).toBe(
        testCase.wantHome,
      )
      expect(Boolean(screen.queryByRole('link', { name: /viewer\.example/i }))).toBe(
        testCase.wantProfile,
      )
      expect(Boolean(screen.queryAllByRole('link', { name: 'Sign in' }).length)).toBe(
        testCase.wantSignIn,
      )
    })
  }
})
