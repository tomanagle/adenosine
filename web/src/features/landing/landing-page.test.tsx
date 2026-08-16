// @vitest-environment jsdom

import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { renderWithAppProviders } from '@/test/render'

import { LandingPage } from './landing-page'

afterEach(cleanup)

describe('LandingPage', () => {
  it('links to the installed API documentation route', () => {
    renderWithAppProviders(<LandingPage />)

    expect(screen.getByRole('link', { name: 'Read the API' }).getAttribute('href')).toBe(
      '/docs/api',
    )
  })
})
