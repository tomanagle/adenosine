import { ArrowUpRight, Clock3, GitBranch, Search, Star, UserRound } from 'lucide-react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { profileSearchQueryOptions, repositorySearchQueryOptions } from './explore.query'
import type { ExploreSearch } from './explore-search'

function exploreSort(value: string): ExploreSearch['sort'] {
  return value === 'recent' ? 'recent' : 'relevance'
}

export function ExplorePage({ search }: { search: ExploreSearch }) {
  return (
    <main className="bg-muted/30">
      <section className="border-b bg-background">
        <div className="mx-auto max-w-6xl px-5 py-10 sm:px-8 sm:py-14">
          <div className="grid gap-8 lg:grid-cols-[0.7fr_1.3fr] lg:items-end">
            <div>
              <p className="text-xs font-medium uppercase tracking-[0.2em] text-muted-foreground">
                Network desk
              </p>
              <div className="mt-3 flex flex-wrap items-center gap-3">
                <h1 className="font-serif text-4xl tracking-tight sm:text-5xl">
                  Explore the network.
                </h1>
                <Badge variant="outline">Local index</Badge>
              </div>
            </div>
            <div>
              <p className="max-w-2xl text-pretty leading-7 text-muted-foreground">
                Search public work and people known to this server. Results are an eventually
                consistent index, not a complete global directory.
              </p>
              <SearchForm key={search.q} search={search} />
            </div>
          </div>
        </div>
      </section>

      <div className="mx-auto max-w-6xl px-5 py-8 sm:px-8 sm:py-10">
        <div className="mb-6 flex flex-col gap-4 border-b pb-5 sm:flex-row sm:items-center sm:justify-between">
          <nav className="flex gap-1" aria-label="Search result type">
            <ResultTypeLink type="repositories" search={search}>
              Repositories
            </ResultTypeLink>
            <ResultTypeLink type="profiles" search={search}>
              Profiles
            </ResultTypeLink>
          </nav>
          <SortControl search={search} />
        </div>

        {!search.q ? (
          <SearchInvitation />
        ) : search.type === 'repositories' ? (
          <RepositoryResults search={search} />
        ) : (
          <ProfileResults search={search} />
        )}
      </div>
    </main>
  )
}

function SearchForm({ search }: { search: ExploreSearch }) {
  const navigate = useNavigate({ from: '/explore' })
  const [query, setQuery] = useState(search.q)
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void navigate({ search: (previous) => ({ ...previous, q: query.trim(), cursor: undefined }) })
  }
  return (
    <search>
      <form className="mt-5 flex gap-2" onSubmit={submit}>
        <label className="sr-only" htmlFor="explore-query">
          Search repositories and profiles
        </label>
        <div className="relative min-w-0 flex-1">
          <Search
            className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <input
            id="explore-query"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            maxLength={200}
            placeholder="Repository, description, or handle"
            className="h-11 w-full rounded-md border bg-background pl-10 pr-3 text-base outline-none transition-shadow placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
          />
        </div>
        <Button type="submit" size="lg">
          Search
        </Button>
      </form>
    </search>
  )
}

function ResultTypeLink({
  type,
  search,
  children,
}: {
  type: ExploreSearch['type']
  search: ExploreSearch
  children: React.ReactNode
}) {
  const active = search.type === type
  return (
    <Link
      to="/explore"
      search={{ ...search, type, cursor: undefined }}
      aria-current={active ? 'page' : undefined}
      className={cn(buttonVariants({ variant: active ? 'default' : 'ghost', size: 'sm' }))}
    >
      {children}
    </Link>
  )
}

function SortControl({ search }: { search: ExploreSearch }) {
  const navigate = useNavigate({ from: '/explore' })
  return (
    <label className="flex items-center gap-2 text-sm text-muted-foreground">
      Sort
      <select
        value={search.sort}
        onChange={(event) =>
          void navigate({
            search: (previous) => ({
              ...previous,
              sort: exploreSort(event.target.value),
              cursor: undefined,
            }),
          })
        }
        className="h-9 rounded-md border bg-background px-3 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <option value="relevance">Best match</option>
        <option value="recent">Recently indexed</option>
      </select>
    </label>
  )
}

function SearchInvitation() {
  return (
    <div className="grid min-h-72 place-items-center rounded-lg border border-dashed bg-background px-6 text-center">
      <div className="max-w-md">
        <Search className="mx-auto size-6 text-muted-foreground" aria-hidden="true" />
        <h2 className="mt-4 font-serif text-2xl">Read across the network</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          Try a project name, its description, or an owner handle. Profile search covers indexed
          handles and display names.
        </p>
      </div>
    </div>
  )
}

function RepositoryResults({ search }: { search: ExploreSearch }) {
  const { data } = useSuspenseQuery(repositorySearchQueryOptions(search))
  if (data.items.length === 0) return <EmptyResults kind="repositories" />
  return (
    <div>
      <p className="mb-3 text-xs uppercase tracking-[0.16em] text-muted-foreground">
        Repository matches
      </p>
      <ul className="divide-y border-y bg-background">
        {data.items.map((repository) => (
          <li key={repository.uri ?? repository.id} className="group py-5 sm:px-4">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Link
                    className="font-serif text-xl underline-offset-4 group-hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    params={{
                      owner: repository.owner.handle ?? repository.owner.did,
                      repo: repository.slug,
                    }}
                    to="/$owner/$repo"
                  >
                    {repository.owner.handle ?? repository.owner.did} /{' '}
                    {repository.display_name ?? repository.slug}
                  </Link>
                  <ArrowUpRight className="size-4 text-muted-foreground" aria-hidden="true" />
                </div>
                <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground">
                  {repository.description ?? 'No description provided.'}
                </p>
                <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1.5">
                    <Star className="size-3.5" aria-hidden="true" />
                    {repository.star_count}
                  </span>
                  <span className="flex items-center gap-1.5">
                    <GitBranch className="size-3.5" aria-hidden="true" />
                    {repository.default_branch}
                  </span>
                  <span className="font-mono">{repository.owner.did}</span>
                </div>
              </div>
              <div className="shrink-0 sm:text-right">
                <Badge variant="outline">
                  {repository.hosting.local ? 'Hosted here' : 'Hosted elsewhere'}
                </Badge>
                <p className="mt-2 max-w-56 text-xs leading-5 text-muted-foreground">
                  Opens the canonical host. Clone destinations come from indexed metadata.
                </p>
              </div>
            </div>
          </li>
        ))}
      </ul>
      <NextPage search={search} cursor={data.page.next_cursor} />
    </div>
  )
}

function ProfileResults({ search }: { search: ExploreSearch }) {
  const { data } = useSuspenseQuery(profileSearchQueryOptions(search))
  if (data.items.length === 0) return <EmptyResults kind="profiles" />
  return (
    <div>
      <p className="mb-3 text-xs uppercase tracking-[0.16em] text-muted-foreground">
        Profile matches
      </p>
      <ul className="grid gap-px overflow-hidden rounded-lg border bg-border sm:grid-cols-2">
        {data.items.map((profile) => (
          <li key={profile.did} className="bg-background p-5">
            <div className="flex items-start gap-4">
              <span className="grid size-10 shrink-0 place-items-center rounded-full border bg-muted">
                <UserRound className="size-4" aria-hidden="true" />
              </span>
              <div className="min-w-0">
                <h2 className="truncate font-serif text-xl">
                  <Link
                    className="underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    params={profile.handle ? { owner: profile.handle } : { identity: profile.did }}
                    to={profile.handle ? '/$owner' : '/profiles/$identity'}
                  >
                    {profile.display_name ?? profile.handle ?? 'Unnamed developer'}
                  </Link>
                </h2>
                <p className="truncate text-sm text-muted-foreground">
                  {profile.handle ? `@${profile.handle}` : profile.did}
                </p>
              </div>
            </div>
            <p className="mt-4 line-clamp-2 min-h-10 text-sm leading-5 text-muted-foreground">
              {profile.bio ?? 'No profile note published.'}
            </p>
            <div className="mt-5 flex items-center justify-between gap-3 border-t pt-4 text-xs text-muted-foreground">
              <span>{profile.repository_count} repositories</span>
              <span className="truncate font-mono">{profile.did}</span>
            </div>
          </li>
        ))}
      </ul>
      <NextPage search={search} cursor={data.page.next_cursor} />
    </div>
  )
}

function EmptyResults({ kind }: { kind: string }) {
  return (
    <Alert>
      <AlertTitle>No matching {kind}</AlertTitle>
      <AlertDescription>
        Try a shorter query or switch the result type. The local index may still be catching up with
        network events.
      </AlertDescription>
    </Alert>
  )
}

function NextPage({ search, cursor }: { search: ExploreSearch; cursor?: string | null }) {
  if (!cursor) return null
  return (
    <div className="mt-6 flex justify-end">
      <Link
        to="/explore"
        search={{ ...search, cursor }}
        className={cn(buttonVariants({ variant: 'outline' }))}
      >
        Next page
      </Link>
    </div>
  )
}

export function ExplorePending() {
  return (
    <main className="grid min-h-screen place-items-center bg-muted/30">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <Clock3 className="size-4 animate-pulse" aria-hidden="true" />
        Reading the local index...
      </div>
    </main>
  )
}

export function ExploreError({ reset }: { reset: () => void }) {
  return (
    <main className="grid min-h-screen place-items-center bg-muted/30 px-5">
      <Alert className="max-w-lg bg-background">
        <AlertTitle>Search is temporarily unavailable</AlertTitle>
        <AlertDescription>
          The REST index could not be read. Electric is not required for this page.
        </AlertDescription>
        <Button className="mt-4" variant="outline" onClick={reset}>
          Try again
        </Button>
      </Alert>
    </main>
  )
}
