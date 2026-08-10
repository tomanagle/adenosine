import { ArrowRight, GitBranch, Network, ShieldCheck } from 'lucide-react'
import { Link } from '@tanstack/react-router'

import { Badge } from '@/components/ui/badge'
import { buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const principles = [
  {
    icon: GitBranch,
    title: 'Git, without the island',
    copy: 'Host repositories locally while discovering work across a wider developer network.',
  },
  {
    icon: Network,
    title: 'Portable identity',
    copy: 'Your developer identity is not trapped inside one forge or one domain.',
  },
  {
    icon: ShieldCheck,
    title: 'API-first by default',
    copy: 'The public API is the same contract used by the first-party interface.',
  },
]

export function LandingPage() {
  return (
    <main>
      <header className="border-b bg-card/70">
        <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-5 sm:px-8">
          <Link
            to="/"
            className="font-semibold tracking-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            Adenosine
          </Link>
          <Link to="/login" className={cn(buttonVariants({ variant: 'outline', size: 'sm' }))}>
            Sign in
          </Link>
        </div>
      </header>
      <section className="mx-auto grid max-w-6xl gap-12 px-5 py-20 sm:px-8 sm:py-28 lg:grid-cols-[1.15fr_0.85fr] lg:items-center">
        <div>
          <Badge variant="secondary">A public, federated Git forge</Badge>
          <h1 className="mt-6 max-w-3xl text-balance font-serif text-5xl leading-[1.02] tracking-tight sm:text-6xl">
            Build in public without giving up your home server.
          </h1>
          <p className="mt-6 max-w-xl text-pretty text-lg leading-8 text-muted-foreground">
            Adenosine combines familiar Git collaboration with portable developer identity and
            network-wide repository discovery.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            <Link to="/login" className={cn(buttonVariants({ size: 'lg' }))}>
              Sign in to your forge <ArrowRight className="size-4" />
            </Link>
            <a href="/docs/api" className={cn(buttonVariants({ variant: 'outline', size: 'lg' }))}>
              Read the API
            </a>
          </div>
        </div>
        <div className="grid gap-3" aria-label="Adenosine principles">
          {principles.map(({ icon: Icon, title, copy }) => (
            <Card key={title}>
              <CardHeader className="flex-row items-center gap-3 pb-3">
                <span className="grid size-9 shrink-0 place-items-center rounded-md bg-secondary">
                  <Icon className="size-4" aria-hidden="true" />
                </span>
                <CardTitle className="text-base">{title}</CardTitle>
              </CardHeader>
              <CardContent className="pl-[4.5rem] text-sm leading-6 text-muted-foreground">
                {copy}
              </CardContent>
            </Card>
          ))}
        </div>
      </section>
    </main>
  )
}
