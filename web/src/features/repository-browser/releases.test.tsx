// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { renderWithAppProviders } from '@/test/render'

import { ReleaseDetailPage, ReleasesPage } from './releases'
import {
  createReleaseMutationOptions,
  deleteReleaseAssetMutationOptions,
  deleteReleaseMutationOptions,
  releaseAssetsQueryOptions,
  releaseQueryOptions,
  releasesQueryOptions,
  tagsQueryOptions,
  updateReleaseMutationOptions,
  uploadReleaseAssetMutationOptions,
} from './queries'

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
type CreateReleaseMutation = NonNullable<
  ReturnType<typeof createReleaseMutationOptions>['mutationFn']
>
type UpdateReleaseMutation = NonNullable<
  ReturnType<typeof updateReleaseMutationOptions>['mutationFn']
>
type DeleteReleaseMutation = NonNullable<
  ReturnType<typeof deleteReleaseMutationOptions>['mutationFn']
>
type UploadReleaseAssetMutation = NonNullable<
  ReturnType<typeof uploadReleaseAssetMutationOptions>['mutationFn']
>
type DeleteReleaseAssetMutation = NonNullable<
  ReturnType<typeof deleteReleaseAssetMutationOptions>['mutationFn']
>

const createRelease = vi.fn<CreateReleaseMutation>()
const updateRelease = vi.fn<UpdateReleaseMutation>()
const deleteRelease = vi.fn<DeleteReleaseMutation>()
const uploadReleaseAsset = vi.fn<UploadReleaseAssetMutation>()
const deleteReleaseAsset = vi.fn<DeleteReleaseAssetMutation>()

const dependencies = {
  releasesQueryOptions: (params: { owner: string; repo: string }) => ({
    ...releasesQueryOptions(params),
    queryFn: () => ({ items: [release], page: { next_cursor: null }, viewer_can_manage: true }),
  }),
  tagsQueryOptions: (params: { owner: string; repo: string }) => ({
    ...tagsQueryOptions(params),
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
  releaseQueryOptions: (params: { owner: string; repo: string }, releaseId: string) => ({
    ...releaseQueryOptions(params, releaseId),
    queryFn: () => release,
  }),
  releaseAssetsQueryOptions: (params: { owner: string; repo: string }, releaseId: string) => ({
    ...releaseAssetsQueryOptions(params, releaseId),
    queryFn: () => ({ items: [asset], page: { next_cursor: null } }),
  }),
  createReleaseMutationOptions: () => ({
    ...createReleaseMutationOptions(),
    mutationFn: createRelease,
  }),
  updateReleaseMutationOptions: () => ({
    ...updateReleaseMutationOptions(),
    mutationFn: updateRelease,
  }),
  deleteReleaseMutationOptions: () => ({
    ...deleteReleaseMutationOptions(),
    mutationFn: deleteRelease,
  }),
  uploadReleaseAssetMutationOptions: () => ({
    ...uploadReleaseAssetMutationOptions(),
    mutationFn: uploadReleaseAsset,
  }),
  deleteReleaseAssetMutationOptions: () => ({
    ...deleteReleaseAssetMutationOptions(),
    mutationFn: deleteReleaseAsset,
  }),
}

function renderWithQuery(children: ReactNode) {
  return renderWithAppProviders(children)
}

beforeEach(() => {
  createRelease.mockReset()
  updateRelease.mockReset()
  deleteRelease.mockReset()
  uploadReleaseAsset.mockReset()
  deleteReleaseAsset.mockReset()
})
afterEach(cleanup)

describe('repository releases', () => {
  it('creates a release from an existing tag with TanStack Form values', async () => {
    createRelease.mockResolvedValue(release)
    const { router } = renderWithQuery(
      <ReleasesPage dependencies={dependencies} params={{ owner: 'alice', repo: 'adenosine' }} />,
    )

    fireEvent.change(await screen.findByLabelText('Release title'), {
      target: { value: ' Adenosine 1.0 ' },
    })
    fireEvent.change(screen.getByLabelText('Release notes'), { target: { value: 'Ship it' } })
    fireEvent.click(screen.getByLabelText('Mark as pre-release'))
    fireEvent.click(screen.getByRole('button', { name: 'Create release' }))

    await waitFor(() => expect(createRelease).toHaveBeenCalledTimes(1))
    expect(createRelease.mock.calls[0]?.[0]).toEqual({
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
      expect(router.state.location.pathname).toBe(`/alice/adenosine/releases/${release.id}`),
    )
  })

  it('renders safe notes and uploads the selected asset with its media type', async () => {
    uploadReleaseAsset.mockResolvedValue(asset)
    renderWithQuery(
      <ReleaseDetailPage
        dependencies={dependencies}
        params={{ owner: 'alice', repo: 'adenosine' }}
        releaseId={release.id}
      />,
    )

    expect(await screen.findByRole('heading', { name: 'Safer shipping' })).toBeTruthy()
    expect(document.querySelector('script')).toBeNull()
    const file = new File(['archive'], 'adenosine.zip', { type: 'application/zip' })
    const input = screen.getByLabelText<HTMLInputElement>('Attach a file')
    Object.defineProperty(input, 'files', { configurable: true, value: [file] })
    fireEvent.change(input)
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => expect(uploadReleaseAsset).toHaveBeenCalledTimes(1))
    expect(uploadReleaseAsset.mock.calls[0]?.[0]).toMatchObject({
      path: { owner: 'alice', repo: 'adenosine', release: release.id },
      query: { name: 'adenosine.zip' },
      headers: { 'X-Asset-Content-Type': 'application/zip' },
      body: file,
    })
  })
})
