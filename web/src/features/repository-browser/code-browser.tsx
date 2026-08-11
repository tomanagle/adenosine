import { useEffect, useState } from 'react'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Download, File, FileCode2, FileQuestion, Folder, GitCommitHorizontal } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { MAX_README, MAX_RENDERED_BLOB } from './loaders'
import { SafeMarkdown } from './markdown'
import {
  blobQueryOptions,
  repositoryQueryOptions,
  treeQueryOptions,
  type RepositoryRouteParams,
} from './queries'
import { classifyBrowserError, EmptyState, RepositoryError } from './states'
import { blobBytes, findReadme, formatBytes, isProbablyBinary, shortSha } from './view-models'
import { splitBlobPath } from './validation'

export function RepositoryOverview({ params }: { params: RepositoryRouteParams }) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const tree = useQuery(treeQueryOptions(params, repository.default_branch))
  if (tree.isPending)
    return <output className="block p-6 text-sm text-muted-foreground">Loading files...</output>
  if (tree.isError) {
    return classifyBrowserError(tree.error) === 'missing' ? (
      <EmptyState
        title="Empty repository"
        description={`The ${repository.default_branch} branch does not have any commits yet.`}
      />
    ) : (
      <RepositoryError error={tree.error} />
    )
  }
  const readme = findReadme(tree.data.entries)

  return (
    <div className="space-y-5">
      <TreePanel params={params} path="" revision={repository.default_branch} />
      {readme ? (
        readme.size == null || readme.size > MAX_README ? (
          <section className="rounded-lg border bg-card p-6" aria-labelledby="readme-title">
            <h2 className="font-semibold" id="readme-title">
              README
            </h2>
            <p className="mt-2 text-sm text-muted-foreground">
              This README has no safe preview
              {readme.size != null ? ` (${formatBytes(readme.size)})` : ''}. Clone the repository to
              inspect it.
            </p>
          </section>
        ) : (
          <ReadmePanel params={params} sha={readme.sha} />
        )
      ) : (
        <EmptyState title="No README" description="This branch does not have a root README file." />
      )}
    </div>
  )
}

export function TreeBrowser({
  params,
  path,
  ref,
}: {
  params: RepositoryRouteParams
  path: string
  ref?: string
}) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const revision = ref ?? repository.default_branch
  return (
    <div className="space-y-4">
      <BrowserToolbar params={params} path={path} revision={revision} />
      <TreePanel params={params} path={path} revision={revision} />
    </div>
  )
}

function TreePanel({
  params,
  path,
  revision,
}: {
  params: RepositoryRouteParams
  path: string
  revision: string
}) {
  const { data: tree } = useSuspenseQuery(treeQueryOptions(params, revision, path))
  const parent = path.includes('/') ? path.slice(0, path.lastIndexOf('/')) : ''

  return (
    <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="files-title">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <h2 className="font-medium" id="files-title">
          {path || 'Files'}
        </h2>
        <Link
          className="inline-flex items-center gap-1 font-mono text-xs text-muted-foreground hover:text-foreground"
          params={{ ...params, revision: tree.commit_sha }}
          to="/$owner/$repo/commit/$revision"
        >
          <GitCommitHorizontal className="size-3.5" /> {shortSha(tree.commit_sha)}
        </Link>
      </div>
      {tree.entries.length === 0 ? (
        <EmptyState title="Empty directory" description="There are no entries at this path." />
      ) : (
        <ul className="divide-y" aria-label={`Files in ${path || 'repository root'}`}>
          {path ? (
            <li>
              <Link
                className="flex min-h-11 items-center gap-3 px-4 py-2 text-sm hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                params={{ ...params, _splat: parent }}
                search={{ ref: revision }}
                to="/$owner/$repo/tree/$"
              >
                <Folder className="size-4 text-muted-foreground" /> ..
              </Link>
            </li>
          ) : null}
          {tree.entries.map((entry) => {
            const isTree = entry.type === 'tree'
            const isSubmodule = entry.type === 'commit'
            const Icon = isTree ? Folder : isSubmodule ? FileQuestion : File
            return (
              <li key={`${entry.mode}-${entry.name}`}>
                {isSubmodule ? (
                  <div className="flex min-h-11 items-center gap-3 px-4 py-2 text-sm text-muted-foreground">
                    <Icon className="size-4" />
                    <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                    <Badge variant="outline">submodule</Badge>
                  </div>
                ) : (
                  <Link
                    className="flex min-h-11 items-center gap-3 px-4 py-2 text-sm hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                    params={{ ...params, _splat: entry.path }}
                    search={{ ref: revision }}
                    to={isTree ? '/$owner/$repo/tree/$' : '/$owner/$repo/blob/$'}
                  >
                    <Icon className="size-4 text-muted-foreground" aria-hidden="true" />
                    <span className="min-w-0 flex-1 truncate">{entry.name}</span>
                    {entry.size != null ? (
                      <span className="text-xs tabular-nums text-muted-foreground">
                        {formatBytes(entry.size)}
                      </span>
                    ) : null}
                  </Link>
                )}
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

function BrowserToolbar({
  params,
  path,
  revision,
}: {
  params: RepositoryRouteParams
  path: string
  revision: string
}) {
  const navigate = useNavigate()
  const segments = path ? path.split('/') : []
  return (
    <div className="flex flex-col gap-3 rounded-lg border bg-card p-3 sm:flex-row sm:items-center">
      <label className="flex items-center gap-2 text-sm">
        <span className="sr-only">Revision</span>
        <input
          className="h-9 w-full rounded-md border bg-background px-3 font-mono text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring sm:w-52"
          defaultValue={revision}
          key={revision}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              void navigate({
                to: '/$owner/$repo/tree/$',
                params: { ...params, _splat: path },
                search: { ref: event.currentTarget.value },
              })
            }
          }}
        />
      </label>
      <nav
        className="flex min-w-0 items-center gap-1 overflow-x-auto text-sm"
        aria-label="File path"
      >
        <Link
          params={{ ...params, _splat: '' }}
          search={{ ref: revision }}
          to="/$owner/$repo/tree/$"
        >
          {params.repo}
        </Link>
        {segments.map((segment, index) => (
          <span className="flex items-center gap-1" key={`${segment}-${index}`}>
            <span className="text-muted-foreground">/</span>
            <Link
              className="whitespace-nowrap hover:underline"
              params={{ ...params, _splat: segments.slice(0, index + 1).join('/') }}
              search={{ ref: revision }}
              to="/$owner/$repo/tree/$"
            >
              {segment}
            </Link>
          </span>
        ))}
      </nav>
    </div>
  )
}

function ReadmePanel({ params, sha }: { params: RepositoryRouteParams; sha: string }) {
  const { data: blob } = useSuspenseQuery(blobQueryOptions(params, sha))
  return (
    <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="readme-title">
      <div className="flex items-center gap-2 border-b px-5 py-3">
        <FileCode2 className="size-4" />
        <h2 className="font-medium" id="readme-title">
          README
        </h2>
      </div>
      <DecodedBlob blob={blob} markdown />
    </section>
  )
}

export function BlobBrowser({
  params,
  routePath,
  ref,
}: {
  params: RepositoryRouteParams
  routePath: string
  ref?: string
}) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const revision = ref ?? repository.default_branch
  const file = splitBlobPath(routePath)
  const { data: tree } = useSuspenseQuery(treeQueryOptions(params, revision, file.parentPath))
  const entry = tree.entries.find((candidate) => candidate.name === file.name)
  if (!entry || entry.type !== 'blob') {
    return (
      <EmptyState
        title="File not found"
        description="This path is not a file at the selected revision."
      />
    )
  }
  return (
    <div className="space-y-4">
      <BrowserToolbar params={params} path={file.parentPath} revision={revision} />
      <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="blob-title">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate font-medium" id="blob-title">
              {file.name}
            </h2>
            <p className="mt-0.5 font-mono text-xs text-muted-foreground">
              {entry.size != null ? formatBytes(entry.size) : 'Unknown size'} ·{' '}
              {shortSha(entry.sha)}
            </p>
          </div>
          {entry.size != null && entry.size <= MAX_RENDERED_BLOB ? (
            <BlobActions name={file.name} params={params} sha={entry.sha} />
          ) : null}
        </div>
        {entry.size == null || entry.size > MAX_RENDERED_BLOB ? (
          <div className="p-8 text-center">
            <p className="font-medium">File is too large to display safely</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Clone the repository to inspect this
              {entry.size != null ? ` ${formatBytes(entry.size)}` : ''} file.
            </p>
          </div>
        ) : (
          <BlobContent params={params} sha={entry.sha} />
        )}
      </section>
    </div>
  )
}

function BlobContent({ params, sha }: { params: RepositoryRouteParams; sha: string }) {
  const { data: blob } = useSuspenseQuery(blobQueryOptions(params, sha))
  return <DecodedBlob blob={blob} />
}

function BlobActions({
  name,
  params,
  sha,
}: {
  name: string
  params: RepositoryRouteParams
  sha: string
}) {
  const { data: blob } = useSuspenseQuery(blobQueryOptions(params, sha))
  function download() {
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = name
    anchor.hidden = true
    document.body.append(anchor)
    anchor.click()
    anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 0)
  }
  return (
    <Button onClick={download} size="sm" variant="outline">
      <Download className="size-3.5" /> Download file
    </Button>
  )
}

function DecodedBlob({ blob, markdown = false }: { blob: Blob | File; markdown?: boolean }) {
  const [content, setContent] = useState<
    | { state: 'loading' }
    | { state: 'binary'; size: number }
    | { state: 'text'; text: string }
    | { state: 'error' }
  >({ state: 'loading' })

  useEffect(() => {
    let active = true
    void blobBytes(blob)
      .then((bytes) => {
        if (!active) return
        if (isProbablyBinary(bytes)) {
          setContent({ state: 'binary', size: bytes.length })
          return
        }
        setContent({
          state: 'text',
          text: new TextDecoder('utf-8', { fatal: false }).decode(bytes),
        })
      })
      .catch(() => {
        if (active) setContent({ state: 'error' })
      })
    return () => {
      active = false
    }
  }, [blob])

  if (content.state === 'loading') {
    return <output className="block p-6 text-sm text-muted-foreground">Decoding file...</output>
  }
  if (content.state === 'binary') {
    return (
      <div className="p-8 text-center">
        <p className="font-medium">Binary file</p>
        <p className="mt-1 text-sm text-muted-foreground">
          Preview is unavailable for this {formatBytes(content.size)} file. Use Download file above.
        </p>
      </div>
    )
  }
  if (content.state === 'error') {
    return <p className="p-6 text-sm text-muted-foreground">This file could not be decoded.</p>
  }
  if (markdown) return <SafeMarkdown source={content.text} />
  return (
    <textarea
      aria-label="File contents"
      className="block h-[70vh] w-full resize-none overflow-auto whitespace-pre border-0 bg-transparent p-4 font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring sm:text-sm"
      readOnly
      spellCheck={false}
      value={content.text}
    />
  )
}
