import type { CurrentIdentity } from '@adenosine/api-client'
import { logout } from '@adenosine/api-client'
import { Link, useNavigate } from '@tanstack/react-router'
import { ArrowUpRight, LogOut, Search } from 'lucide-react'
import { useState, type FormEvent, type ReactNode } from 'react'

import { browserApiClient } from '@/api/browser-client'
import adenosineMarkDark from '@/assets/adenosine-mark-dark.svg?url'
import adenosineMarkLight from '@/assets/adenosine-mark-light.svg?url'
import { Button, buttonVariants } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

export function AppShell({
  children,
  identity,
}: {
  children: ReactNode
  identity?: CurrentIdentity | null
}) {
  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader identity={identity} />
      <div className="flex-1">{children}</div>
      <SiteFooter identity={identity} />
    </div>
  )
}

function SiteHeader({ identity }: { identity?: CurrentIdentity | null }) {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [signingOut, setSigningOut] = useState(false)

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const value = query.trim()
    if (!value) return
    void navigate({
      search: { q: value, sort: 'relevance', type: 'repositories' },
      to: '/explore',
    })
  }

  async function signOut() {
    setSigningOut(true)
    await logout({ client: browserApiClient })
    window.location.assign('/')
  }

  return (
    <header className="sticky top-0 z-40 border-b bg-background/92 backdrop-blur-xl">
      <div className="mx-auto flex min-h-16 max-w-6xl flex-wrap items-center gap-x-5 gap-y-3 px-5 py-3 sm:px-8">
        <Brand />

        <nav
          aria-label="Primary"
          className="order-3 flex w-full items-center gap-1 sm:order-none sm:w-auto"
        >
          {identity ? <PrimaryLink exact label="Home" to="/" /> : null}
          {identity ? <PrimaryLink label="Organizations" to="/organizations" /> : null}
          <PrimaryLink label="Explore" to="/explore" />
          <a
            className="inline-flex h-9 items-center gap-1.5 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            href="/docs/api"
          >
            API <ArrowUpRight aria-hidden="true" className="size-3.5" />
          </a>
        </nav>

        <form className="ml-auto hidden min-w-44 max-w-64 flex-1 lg:block" onSubmit={search}>
          <div className="relative">
            <Search
              aria-hidden="true"
              className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            />
            <Input
              aria-label="Search repositories and profiles"
              className="h-9 bg-muted/35 pl-9"
              maxLength={200}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search the network"
              type="search"
              value={query}
            />
          </div>
        </form>

        {identity ? (
          <div className="ml-auto flex min-w-0 items-center gap-1.5 lg:ml-0">
            <Link
              className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              params={identity.handle ? { owner: identity.handle } : { identity: identity.did }}
              to={identity.handle ? '/$owner' : '/profiles/$identity'}
            >
              <span
                aria-hidden="true"
                className="grid size-8 shrink-0 place-items-center rounded-full bg-primary/15 text-xs font-semibold text-primary"
              >
                {(identity.handle ?? identity.did).slice(0, 1).toUpperCase()}
              </span>
              <span className="hidden min-w-0 sm:block">
                <span className="block max-w-40 truncate text-xs font-medium">
                  {identity.handle ?? 'Your profile'}
                </span>
                <span className="block text-[0.65rem] text-muted-foreground">View profile</span>
              </span>
            </Link>
            <Button
              aria-label="Sign out"
              disabled={signingOut}
              onClick={() => void signOut()}
              size="sm"
              title={signingOut ? 'Signing out' : 'Sign out'}
              variant="ghost"
            >
              <LogOut aria-hidden="true" className="size-4" />
              <span className="sr-only sm:not-sr-only">
                {signingOut ? 'Signing out...' : 'Sign out'}
              </span>
            </Button>
          </div>
        ) : (
          <Link className={cn(buttonVariants({ size: 'sm' }), 'ml-auto lg:ml-0')} to="/login">
            Sign in
          </Link>
        )}
      </div>
    </header>
  )
}

function Brand() {
  return (
    <Link
      aria-label="Adenosine home"
      className="flex shrink-0 items-center gap-2.5 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      to="/"
    >
      <picture className="block size-9 shrink-0">
        <source media="(prefers-color-scheme: dark)" srcSet={adenosineMarkDark} />
        <img
          alt=""
          className="size-9"
          draggable="false"
          height="36"
          src={adenosineMarkLight}
          width="36"
        />
      </picture>
      <span className="font-semibold tracking-tight">Adenosine</span>
    </Link>
  )
}

function PrimaryLink({
  exact,
  label,
  to,
}: {
  exact?: boolean
  label: string
  to: '/' | '/explore' | '/organizations'
}) {
  return (
    <Link
      activeOptions={{ exact }}
      activeProps={{ className: 'bg-accent text-foreground' }}
      className="inline-flex h-9 items-center rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      search={to === '/explore' ? { q: '', sort: 'relevance', type: 'repositories' } : undefined}
      to={to}
    >
      {label}
    </Link>
  )
}

function SiteFooter({ identity }: { identity?: CurrentIdentity | null }) {
  return (
    <footer className="border-t bg-card/45">
      <div className="mx-auto grid max-w-6xl gap-8 px-5 py-10 sm:px-8 md:grid-cols-[1fr_auto] md:items-end">
        <div>
          <Brand />
          <p className="mt-4 max-w-md text-sm leading-6 text-muted-foreground">
            Git hosting and collaboration built around portable identity and an open network.
          </p>
          <p className="mt-3 text-xs text-muted-foreground">
            Your server. Your identity. Your code.
          </p>
        </div>
        <nav aria-label="Footer" className="flex flex-wrap gap-x-5 gap-y-3 text-sm">
          {identity ? (
            <Link className="text-muted-foreground hover:text-foreground" to="/">
              Home
            </Link>
          ) : null}
          <Link
            className="text-muted-foreground hover:text-foreground"
            search={{ q: '', sort: 'relevance', type: 'repositories' }}
            to="/explore"
          >
            Explore
          </Link>
          <a className="text-muted-foreground hover:text-foreground" href="/docs/api">
            API documentation
          </a>
          {!identity ? (
            <Link className="text-muted-foreground hover:text-foreground" to="/login">
              Sign in
            </Link>
          ) : null}
        </nav>
      </div>
    </footer>
  )
}
