// @vitest-environment jsdom

import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { AppShell } from './app-shell'

vi.mock('@adenosine/api-client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@adenosine/api-client')>()),
  logout: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  useNavigate: () => vi.fn(),
}))

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
      const { container } = render(
        <AppShell identity={testCase.identity}>
          <main>Page content</main>
        </AppShell>,
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
