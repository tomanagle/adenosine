import { useState } from 'react'
import type { Diff } from '@adenosine/api-client'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ArrowRight, Binary, GitCompareArrows, GitMerge } from 'lucide-react'

import { Button } from '@/components/ui/button'

import {
  diffQueryOptions,
  mergeBaseQueryOptions,
  repositoryQueryOptions,
  type RepositoryRouteParams,
} from './queries'
import { classifyBrowserError, EmptyState, RepositoryError } from './states'
import { shortSha } from './view-models'

export function CompareView({
  params,
  base,
  head,
}: {
  params: RepositoryRouteParams
  base?: string
  head?: string
}) {
  const { data: repository } = useSuspenseQuery(repositoryQueryOptions(params))
  const [draftBase, setDraftBase] = useState(base ?? repository.default_branch)
  const [draftHead, setDraftHead] = useState(head ?? '')
  const navigate = useNavigate()

  return (
    <div className="space-y-5">
      <form
        className="rounded-lg border bg-card p-4 sm:p-5"
        onSubmit={(event) => {
          event.preventDefault()
          if (draftBase && draftHead) {
            void navigate({
              to: '/$owner/$repo/compare',
              params,
              search: { base: draftBase, head: draftHead },
            })
          }
        }}
      >
        <div className="flex items-center gap-2">
          <GitCompareArrows className="size-5" />
          <h2 className="font-semibold">Compare revisions</h2>
        </div>
        <div className="mt-4 grid gap-3 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-end">
          <label className="text-xs font-medium text-muted-foreground">
            Base
            <input
              className="mt-1 block h-10 w-full rounded-md border bg-background px-3 font-mono text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
              onChange={(event) => setDraftBase(event.target.value)}
              required
              value={draftBase}
            />
          </label>
          <ArrowRight
            className="mb-3 hidden size-4 text-muted-foreground sm:block"
            aria-hidden="true"
          />
          <label className="text-xs font-medium text-muted-foreground">
            Head
            <input
              className="mt-1 block h-10 w-full rounded-md border bg-background px-3 font-mono text-sm text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
              onChange={(event) => setDraftHead(event.target.value)}
              required
              value={draftHead}
            />
          </label>
          <Button type="submit">Compare</Button>
        </div>
        <p className="mt-3 text-xs text-muted-foreground">
          The server resolves revisions and enforces a bounded patch size.
        </p>
      </form>
      {base && head ? <ComparisonResult base={base} head={head} params={params} /> : null}
    </div>
  )
}

function ComparisonResult({
  params,
  base,
  head,
}: {
  params: RepositoryRouteParams
  base: string
  head: string
}) {
  const diff = useQuery(diffQueryOptions(params, base, head))
  const mergeBase = useQuery(mergeBaseQueryOptions(params, base, head))
  if (diff.isPending || mergeBase.isPending) {
    return (
      <output className="block p-6 text-sm text-muted-foreground">Resolving comparison...</output>
    )
  }
  if (diff.isError) return <RepositoryError error={diff.error} />
  if (mergeBase.isError && classifyBrowserError(mergeBase.error) !== 'missing') {
    return <RepositoryError error={mergeBase.error} />
  }
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-card px-4 py-3 text-sm">
        <GitMerge className="size-4" aria-hidden="true" />
        <span className="font-medium">Merge base</span>
        {mergeBase.isError ? (
          <span className="text-muted-foreground">
            No common commit or matching revision was found.
          </span>
        ) : (
          <code className="text-muted-foreground" title={mergeBase.data.sha}>
            {shortSha(mergeBase.data.sha)}
          </code>
        )}
      </div>
      <DiffPanel diff={diff.data} />
    </div>
  )
}

export function DiffPanel({ diff }: { diff: Diff }) {
  return (
    <section className="overflow-hidden rounded-lg border bg-card" aria-labelledby="diff-title">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <h2 className="font-semibold" id="diff-title">
          Bounded diff
        </h2>
        <p className="font-mono text-xs text-muted-foreground">
          {shortSha(diff.base_sha)} <span aria-hidden="true">→</span>
          <span className="sr-only"> to </span> {shortSha(diff.head_sha)}
        </p>
      </div>
      {diff.files.length === 0 ? (
        <EmptyState title="No changes" description="These revisions have no file differences." />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[32rem] text-left text-sm">
            <caption className="sr-only">Changed files</caption>
            <thead className="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium" scope="col">
                  Status
                </th>
                <th className="px-4 py-2 font-medium" scope="col">
                  Path
                </th>
                <th className="px-4 py-2 text-right font-medium" scope="col">
                  Lines
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {diff.files.map((file, index) => (
                <tr key={`${file.old_path}-${file.new_path}-${index}`}>
                  <td className="px-4 py-2 font-mono text-xs">{file.status}</td>
                  <td className="px-4 py-2 font-mono text-xs">
                    {file.old_path === file.new_path
                      ? file.new_path
                      : `${file.old_path} → ${file.new_path}`}
                  </td>
                  <td className="px-4 py-2 text-right font-mono text-xs">
                    {file.additions == null || file.deletions == null ? (
                      <span className="inline-flex items-center gap-1">
                        <Binary className="size-3" /> binary
                      </span>
                    ) : (
                      <>
                        <span aria-label={`${file.additions} additions`}>+{file.additions}</span>{' '}
                        <span aria-label={`${file.deletions} deletions`}>-{file.deletions}</span>
                      </>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {diff.patch ? <Patch patch={diff.patch} /> : null}
    </section>
  )
}

function Patch({ patch }: { patch: string }) {
  return (
    <textarea
      aria-label="Patch contents"
      className="block h-[70vh] w-full resize-none overflow-auto whitespace-pre border-0 border-t bg-transparent p-4 font-mono text-xs leading-5 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      readOnly
      spellCheck={false}
      value={patch}
    />
  )
}
