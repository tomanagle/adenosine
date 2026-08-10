// @vitest-environment jsdom

import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { LandingPage } from './landing-page'

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to, ...props }: { children: ReactNode; to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}))

afterEach(cleanup)

describe('LandingPage', () => {
  it('links to the installed API documentation route', () => {
    render(<LandingPage />)

    expect(screen.getByRole('link', { name: 'Read the API' }).getAttribute('href')).toBe(
      '/docs/api',
    )
  })
})
