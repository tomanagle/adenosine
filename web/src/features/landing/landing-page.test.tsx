/* @vitest-environment jsdom */

import type { AnchorHTMLAttributes, ReactNode } from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { LandingPage } from './landing-page'

vi.mock('@tanstack/react-router', () => ({
  Link: ({
    children,
    to,
    ...props
  }: AnchorHTMLAttributes<HTMLAnchorElement> & { children: ReactNode; to: string }) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}))

afterEach(cleanup)

describe('LandingPage', () => {
  it('links to the API documentation', () => {
    render(<LandingPage />)

    const link = screen.getByRole('link', { name: 'Read the API' })

    expect(link.getAttribute('href')).toBe('/docs/api')
  })
})
