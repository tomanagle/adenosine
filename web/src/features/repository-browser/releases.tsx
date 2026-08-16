import { useForm } from '@tanstack/react-form'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Download, FileArchive, PackageOpen, Plus, Save, ShieldCheck, Trash2 } from 'lucide-react'

import type { Release } from '@adenosine/api-client'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field } from '@/components/ui/field'
import { Input, Select, Textarea } from '@/components/ui/input'
import { apiErrorMessage } from '@/lib/api-error'
import { cn } from '@/lib/utils'

import { SafeMarkdown } from './markdown'
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
import type { RepositoryRouteParams } from './queries'

export function ReleasesPage({ params }: { params: RepositoryRouteParams }) {
  const { data } = useSuspenseQuery(releasesQueryOptions(params))
  return (
    <div className="space-y-7">
      <header className="flex flex-wrap items-end justify-between gap-4 border-b pb-5">
        <div>
          <p className="text-xs font-medium uppercase tracking-[0.16em] text-primary">Ship log</p>
          <h2 className="mt-1 font-serif text-3xl">Releases</h2>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
            Versioned notes and verified downloads pinned to immutable Git targets.
          </p>
        </div>
        <Badge variant="outline">{data.items.length} loaded</Badge>
      </header>

      {data.viewer_can_manage ? <CreateReleaseForm params={params} /> : null}

      {data.items.length ? (
        <ol className="relative space-y-5 before:absolute before:bottom-6 before:left-[0.7rem] before:top-6 before:w-px before:bg-border">
          {data.items.map((item) => (
            <li className="relative pl-10" key={item.id}>
              <span className="absolute left-0 top-5 grid size-6 place-items-center rounded-full border bg-card text-primary shadow-sm">
                <PackageOpen className="size-3.5" />
              </span>
              <ReleaseCard item={item} params={params} />
            </li>
          ))}
        </ol>
      ) : (
        <Card className="border-dashed">
          <CardContent className="py-12 text-center">
            <PackageOpen className="mx-auto size-8 text-muted-foreground" />
            <p className="mt-3 font-medium">No releases published yet</p>
            <p className="mt-1 text-sm text-muted-foreground">
              A maintainer can turn an existing Git tag into a release.
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function ReleaseCard({ item, params }: { item: Release; params: RepositoryRouteParams }) {
  return (
    <Card className="overflow-hidden">
      <CardHeader className="bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <CardTitle className="font-serif text-2xl">
              <Link
                className="underline-offset-4 hover:underline"
                params={{ ...params, release: item.id }}
                to="/$owner/$repo/releases/$release"
              >
                {item.name}
              </Link>
            </CardTitle>
            <CardDescription className="mt-1 font-mono text-xs">{item.tag_name}</CardDescription>
          </div>
          <div className="flex flex-wrap gap-2">
            {item.state === 'draft' ? <Badge variant="outline">Draft</Badge> : null}
            {item.prerelease ? <Badge variant="secondary">Pre-release</Badge> : null}
            {item.state === 'published' && !item.prerelease ? <Badge>Latest-ready</Badge> : null}
          </div>
        </div>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3 py-4 text-xs text-muted-foreground">
        <span>
          {item.published_at
            ? `Published ${formatDate(item.published_at)}`
            : `Created ${formatDate(item.created_at)}`}
        </span>
        <code title={item.target_sha}>{item.target_sha.slice(0, 12)}</code>
      </CardContent>
    </Card>
  )
}

function CreateReleaseForm({ params }: { params: RepositoryRouteParams }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { data: tags } = useSuspenseQuery(tagsQueryOptions(params))
  const mutation = useMutation(createReleaseMutationOptions())
  const form = useForm({
    defaultValues: {
      tagName: tags.items[0]?.name ?? '',
      name: '',
      body: '',
      draft: false,
      prerelease: false,
    },
    onSubmit: async ({ value }) => {
      const created = await mutation.mutateAsync({
        path: params,
        body: {
          tag_name: value.tagName,
          name: value.name.trim(),
          body: value.body,
          draft: value.draft,
          prerelease: value.prerelease,
        },
      })
      await queryClient.invalidateQueries({ queryKey: releasesQueryOptions(params).queryKey })
      await navigate({
        to: '/$owner/$repo/releases/$release',
        params: { ...params, release: created.id },
      })
    },
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Plus className="size-4" /> Draft a release
        </CardTitle>
        <CardDescription>
          The selected tag target is snapshotted when you create the release.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {tags.items.length ? (
          <form
            className="grid gap-4 sm:grid-cols-2"
            onSubmit={(event) => {
              event.preventDefault()
              void form.handleSubmit()
            }}
          >
            <form.Field name="tagName">
              {(field) => (
                <Field htmlFor="release-tag" label="Existing tag">
                  <Select
                    id="release-tag"
                    required
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                  >
                    {tags.items.map((tag) => (
                      <option key={tag.name} value={tag.name}>
                        {tag.name}
                      </option>
                    ))}
                  </Select>
                </Field>
              )}
            </form.Field>
            <form.Field name="name">
              {(field) => (
                <Field htmlFor="release-name" label="Release title">
                  <Input
                    id="release-name"
                    maxLength={255}
                    placeholder="Adenosine 1.0"
                    required
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                  />
                </Field>
              )}
            </form.Field>
            <form.Field name="body">
              {(field) => (
                <Field
                  className="sm:col-span-2"
                  hint="Markdown is rendered with repository HTML disabled."
                  htmlFor="release-notes"
                  label="Release notes"
                >
                  <Textarea
                    className="min-h-40"
                    id="release-notes"
                    maxLength={1_048_576}
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                  />
                </Field>
              )}
            </form.Field>
            <div className="flex flex-wrap gap-5 text-sm sm:col-span-2">
              <form.Field name="draft">
                {(field) => (
                  <CheckField
                    checked={field.state.value}
                    label="Keep as draft"
                    onChange={field.handleChange}
                  />
                )}
              </form.Field>
              <form.Field name="prerelease">
                {(field) => (
                  <CheckField
                    checked={field.state.value}
                    label="Mark as pre-release"
                    onChange={field.handleChange}
                  />
                )}
              </form.Field>
            </div>
            <div className="flex items-center gap-3 sm:col-span-2">
              <Button disabled={mutation.isPending} type="submit">
                {mutation.isPending ? 'Creating…' : 'Create release'}
              </Button>
              <span className="text-xs text-muted-foreground">
                Published releases are visible immediately.
              </span>
            </div>
            {mutation.isError ? (
              <Alert className="sm:col-span-2">
                <AlertTitle>Release not created</AlertTitle>
                <AlertDescription>
                  {apiErrorMessage(mutation.error, 'The release could not be created.')}
                </AlertDescription>
              </Alert>
            ) : null}
          </form>
        ) : (
          <Alert>
            <AlertTitle>A tag is required</AlertTitle>
            <AlertDescription>Push a Git tag before creating a release.</AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  )
}

export function ReleaseDetailPage({
  params,
  releaseId,
}: {
  params: RepositoryRouteParams
  releaseId: string
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { data: item } = useSuspenseQuery(releaseQueryOptions(params, releaseId))
  const { data: assets } = useSuspenseQuery(releaseAssetsQueryOptions(params, releaseId))
  const { data: releases } = useSuspenseQuery(releasesQueryOptions(params))
  const update = useMutation(updateReleaseMutationOptions())
  const remove = useMutation(deleteReleaseMutationOptions())
  const removeAsset = useMutation(deleteReleaseAssetMutationOptions())
  const canManage = releases.viewer_can_manage
  const form = useForm({
    defaultValues: {
      name: item.name,
      body: item.body,
      draft: item.state === 'draft',
      prerelease: item.prerelease,
    },
    onSubmit: async ({ value }) => {
      await update.mutateAsync({
        path: { ...params, release: releaseId },
        body: {
          name: value.name.trim(),
          body: value.body,
          draft: value.draft,
          prerelease: value.prerelease,
        },
      })
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: releaseQueryOptions(params, releaseId).queryKey,
        }),
        queryClient.invalidateQueries({ queryKey: releasesQueryOptions(params).queryKey }),
      ])
    },
  })
  const refreshAssets = () =>
    queryClient.invalidateQueries({
      queryKey: releaseAssetsQueryOptions(params, releaseId).queryKey,
    })
  return (
    <div className="space-y-7">
      <header className="border-b pb-5">
        <Link
          className="text-xs text-muted-foreground underline underline-offset-4"
          params={params}
          to="/$owner/$repo/releases"
        >
          All releases
        </Link>
        <div className="mt-3 flex flex-wrap items-start justify-between gap-4">
          <div>
            <div className="flex flex-wrap gap-2">
              <Badge variant="outline">{item.tag_name}</Badge>
              {item.state === 'draft' ? <Badge variant="secondary">Draft</Badge> : null}
              {item.prerelease ? <Badge variant="secondary">Pre-release</Badge> : null}
            </div>
            <h2 className="mt-2 font-serif text-3xl">{item.name}</h2>
            <p className="mt-2 text-xs text-muted-foreground">
              Pinned to <code>{item.target_sha}</code>
            </p>
          </div>
          {item.state === 'published' ? (
            <span className="inline-flex items-center gap-2 text-xs text-muted-foreground">
              <ShieldCheck className="size-4 text-primary" /> Immutable target
            </span>
          ) : null}
        </div>
      </header>

      <Card>
        <CardContent className="py-6">
          {item.body ? (
            <SafeMarkdown source={item.body} />
          ) : (
            <p className="text-sm text-muted-foreground">No release notes were provided.</p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FileArchive className="size-4" /> Assets
          </CardTitle>
          <CardDescription>
            Streaming downloads include a SHA-256 checksum and immutable cache identity.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {assets.items.length ? (
            <ul className="divide-y rounded-lg border">
              {assets.items.map((asset) => (
                <li className="flex flex-wrap items-center gap-3 p-4" key={asset.id}>
                  <FileArchive className="size-4 text-muted-foreground" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{asset.name}</span>
                    <span className="block font-mono text-[0.68rem] text-muted-foreground">
                      {formatBytes(asset.size_bytes)} · sha256:{asset.sha256.slice(0, 16)}…
                    </span>
                  </span>
                  <a
                    className={cn(buttonVariants({ size: 'sm', variant: 'outline' }))}
                    href={asset.download_url}
                  >
                    <Download className="size-3.5" /> Download
                  </a>
                  {canManage ? (
                    <Button
                      disabled={removeAsset.isPending}
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        void removeAsset
                          .mutateAsync({ path: { ...params, release: releaseId, asset: asset.id } })
                          .then(refreshAssets)
                      }}
                    >
                      <Trash2 className="size-3.5" /> Remove
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">No downloadable assets attached.</p>
          )}
          {canManage ? (
            <AssetUploadForm params={params} releaseId={releaseId} onUploaded={refreshAssets} />
          ) : null}
        </CardContent>
      </Card>

      {canManage ? (
        <Card>
          <CardHeader>
            <CardTitle>Edit release</CardTitle>
            <CardDescription>
              The tag and target SHA never change; notes and publication state can.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form
              className="grid gap-4"
              onSubmit={(event) => {
                event.preventDefault()
                void form.handleSubmit()
              }}
            >
              <form.Field name="name">
                {(field) => (
                  <Field htmlFor="edit-release-name" label="Release title">
                    <Input
                      id="edit-release-name"
                      maxLength={255}
                      required
                      value={field.state.value}
                      onChange={(event) => field.handleChange(event.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
              <form.Field name="body">
                {(field) => (
                  <Field htmlFor="edit-release-body" label="Release notes">
                    <Textarea
                      className="min-h-40"
                      id="edit-release-body"
                      maxLength={1_048_576}
                      value={field.state.value}
                      onChange={(event) => field.handleChange(event.target.value)}
                    />
                  </Field>
                )}
              </form.Field>
              <div className="flex flex-wrap gap-5 text-sm">
                <form.Field name="draft">
                  {(field) => (
                    <CheckField
                      checked={field.state.value}
                      label="Keep as draft"
                      onChange={field.handleChange}
                    />
                  )}
                </form.Field>
                <form.Field name="prerelease">
                  {(field) => (
                    <CheckField
                      checked={field.state.value}
                      label="Mark as pre-release"
                      onChange={field.handleChange}
                    />
                  )}
                </form.Field>
              </div>
              <div className="flex flex-wrap items-center gap-3">
                <Button disabled={update.isPending} type="submit">
                  <Save className="size-4" /> {update.isPending ? 'Saving…' : 'Save release'}
                </Button>
                <Button
                  disabled={remove.isPending}
                  type="button"
                  variant="outline"
                  onClick={() => {
                    void remove
                      .mutateAsync({ path: { ...params, release: releaseId } })
                      .then(async () => {
                        await queryClient.invalidateQueries({
                          queryKey: releasesQueryOptions(params).queryKey,
                        })
                        await navigate({ to: '/$owner/$repo/releases', params })
                      })
                  }}
                >
                  <Trash2 className="size-4" /> Delete release
                </Button>
              </div>
              {update.isError || remove.isError ? (
                <Alert>
                  <AlertTitle>Release not changed</AlertTitle>
                  <AlertDescription>
                    {apiErrorMessage(
                      update.error ?? remove.error,
                      'The release could not be changed.',
                    )}
                  </AlertDescription>
                </Alert>
              ) : null}
            </form>
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}

function AssetUploadForm({
  params,
  releaseId,
  onUploaded,
}: {
  params: RepositoryRouteParams
  releaseId: string
  onUploaded: () => Promise<unknown>
}) {
  const mutation = useMutation(uploadReleaseAssetMutationOptions())
  const form = useForm({
    defaultValues: { file: null as File | null },
    onSubmit: async ({ value }) => {
      if (!value.file) return
      await mutation.mutateAsync({
        path: { ...params, release: releaseId },
        query: { name: value.file.name },
        headers: { 'X-Asset-Content-Type': value.file.type || 'application/octet-stream' },
        body: value.file,
      })
      form.reset()
      await onUploaded()
    },
  })
  return (
    <form
      className="rounded-lg border border-dashed bg-muted/10 p-4"
      onSubmit={(event) => {
        event.preventDefault()
        void form.handleSubmit()
      }}
    >
      <div className="flex flex-wrap items-end gap-3">
        <form.Field name="file">
          {(field) => (
            <Field
              className="min-w-0 flex-1"
              hint="The server verifies the declared size and computes SHA-256 while streaming."
              htmlFor="release-asset"
              label="Attach a file"
            >
              <Input
                id="release-asset"
                required
                type="file"
                onChange={(event) => field.handleChange(event.target.files?.[0] ?? null)}
              />
            </Field>
          )}
        </form.Field>
        <Button disabled={mutation.isPending} type="submit">
          {mutation.isPending ? 'Uploading…' : 'Upload asset'}
        </Button>
      </div>
      {mutation.isError ? (
        <p className="mt-3 text-xs text-danger" role="alert">
          {apiErrorMessage(mutation.error, 'The asset could not be uploaded.')}
        </p>
      ) : null}
    </form>
  )
}

function CheckField({
  checked,
  label,
  onChange,
}: {
  checked: boolean
  label: string
  onChange: (value: boolean) => void
}) {
  return (
    <label className="flex items-center gap-2">
      <Input
        checked={checked}
        className="size-4"
        type="checkbox"
        onChange={(event) => onChange(event.target.checked)}
      />{' '}
      {label}
    </label>
  )
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(value))
}
function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}
