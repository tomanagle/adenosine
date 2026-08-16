// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ReleaseDetailPage, ReleasesPage } from './releases'

const release = {
  id: '0198aaaa-0000-7000-8000-000000000001',
  tag_name: 'v1.0.0',
  target_sha: 'a'.repeat(40),
  name: 'Adenosine 1.0',
  body: '## Safer shipping\n\n<script>alert(1)</script>',
  state: 'published' as const,
  prerelease: false,
  created_by_did: 'did:plc:alice',
  created_at: '2026-08-16T00:00:00Z',
  updated_at: '2026-08-16T00:00:00Z',
  published_at: '2026-08-16T00:00:00Z',
}
const asset = {
  id: '0198aaaa-0000-7000-8000-000000000002',
  name: 'adenosine.tar.gz',
  content_type: 'application/gzip',
  size_bytes: 2048,
  sha256: 'b'.repeat(64),
  download_url: '/download',
  created_at: '2026-08-16T00:00:00Z',
}
const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  upload: vi.fn(),
  removeAsset: vi.fn(),
  navigate: vi.fn(),
}))

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to }: { children: ReactNode; to: string }) => <a href={to}>{children}</a>,
  useNavigate: () => mocks.navigate,
}))

vi.mock('./queries', () => ({
  releasesQueryOptions: () => ({
    queryKey: ['releases'],
    queryFn: () => ({ items: [release], page: { next_cursor: null }, viewer_can_manage: true }),
  }),
  tagsQueryOptions: () => ({
    queryKey: ['tags'],
    queryFn: () => ({
      items: [
        {
          name: 'v1.0.0',
          sha: 'a'.repeat(40),
          object_type: 'commit',
          target_sha: 'a'.repeat(40),
          target_type: 'commit',
        },
      ],
      page: { next_cursor: null },
    }),
  }),
  releaseQueryOptions: () => ({ queryKey: ['release'], queryFn: () => release }),
  releaseAssetsQueryOptions: () => ({
    queryKey: ['release-assets'],
    queryFn: () => ({ items: [asset], page: { next_cursor: null } }),
  }),
  createReleaseMutationOptions: () => ({ mutationFn: mocks.create }),
  updateReleaseMutationOptions: () => ({ mutationFn: mocks.update }),
  deleteReleaseMutationOptions: () => ({ mutationFn: mocks.remove }),
  uploadReleaseAssetMutationOptions: () => ({ mutationFn: mocks.upload }),
  deleteReleaseAssetMutationOptions: () => ({ mutationFn: mocks.removeAsset }),
}))

function renderWithQuery(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>)
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset()
})
afterEach(cleanup)

describe('repository releases', () => {
  it('creates a release from an existing tag with TanStack Form values', async () => {
    mocks.create.mockResolvedValue(release)
    renderWithQuery(<ReleasesPage params={{ owner: 'alice', repo: 'adenosine' }} />)

    fireEvent.change(await screen.findByLabelText('Release title'), {
      target: { value: ' Adenosine 1.0 ' },
    })
    fireEvent.change(screen.getByLabelText('Release notes'), { target: { value: 'Ship it' } })
    fireEvent.click(screen.getByLabelText('Mark as pre-release'))
    fireEvent.click(screen.getByRole('button', { name: 'Create release' }))

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1))
    expect(mocks.create.mock.calls[0]?.[0]).toEqual({
      path: { owner: 'alice', repo: 'adenosine' },
      body: {
        tag_name: 'v1.0.0',
        name: 'Adenosine 1.0',
        body: 'Ship it',
        draft: false,
        prerelease: true,
      },
    })
    await waitFor(() =>
      expect(mocks.navigate).toHaveBeenCalledWith({
        to: '/$owner/$repo/releases/$release',
        params: { owner: 'alice', repo: 'adenosine', release: release.id },
      }),
    )
  })

  it('renders safe notes and uploads the selected asset with its media type', async () => {
    mocks.upload.mockResolvedValue(asset)
    renderWithQuery(
      <ReleaseDetailPage params={{ owner: 'alice', repo: 'adenosine' }} releaseId={release.id} />,
    )

    expect(await screen.findByRole('heading', { name: 'Safer shipping' })).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()
    const file = new File(['archive'], 'adenosine.zip', { type: 'application/zip' })
    const input = screen.getByLabelText<HTMLInputElement>('Attach a file')
    Object.defineProperty(input, 'files', { configurable: true, value: [file] })
    fireEvent.change(input)
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(mocks.upload).toHaveBeenCalledTimes(1))
    expect(mocks.upload.mock.calls[0]?.[0]).toMatchObject({
      path: { owner: 'alice', repo: 'adenosine', release: release.id },
      query: { name: 'adenosine.zip' },
      headers: { 'X-Asset-Content-Type': 'application/zip' },
      body: file,
    })
  })
})
