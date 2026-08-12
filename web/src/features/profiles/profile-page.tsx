import { useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ExternalLink, MapPin, UserRound } from 'lucide-react'

import { Badge } from '@/components/ui/badge'

import { profileQueryOptions } from './profile.query'

export function ProfilePage({ did }: { did: string }) {
  const { data: profile } = useSuspenseQuery(profileQueryOptions(did))
  const website = safeWebsite(profile.website)
  return (
    <main className="min-h-screen bg-muted/20 px-5 py-10 sm:px-8">
      <article className="mx-auto max-w-3xl overflow-hidden rounded-xl border bg-card">
        <div className="border-b p-6 sm:p-9">
          <Link
            className="text-sm text-muted-foreground hover:text-foreground"
            search={{ q: '', type: 'repositories', sort: 'relevance' }}
            to="/explore"
          >
            Explore
          </Link>
          <div className="mt-8 flex flex-col gap-5 sm:flex-row sm:items-start">
            <span className="grid size-20 shrink-0 place-items-center rounded-full border bg-muted">
              <UserRound className="size-8" aria-hidden="true" />
            </span>
            <div className="min-w-0">
              <h1 className="font-serif text-4xl tracking-tight">
                {profile.display_name ?? profile.handle ?? 'Unnamed developer'}
              </h1>
              <p className="mt-1 break-all font-mono text-sm text-muted-foreground">
                {profile.handle ? `@${profile.handle} · ` : ''}
                {profile.did}
              </p>
              {profile.bio ? (
                <p className="mt-5 whitespace-pre-wrap leading-7">{profile.bio}</p>
              ) : null}
            </div>
          </div>
        </div>
        <div className="grid gap-px bg-border sm:grid-cols-3">
          <ProfileStat label="Repositories" value={profile.repository_count} />
          <ProfileStat label="Contributions" value={profile.contribution_count} />
          <div className="bg-card p-5 text-sm">
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Profile state</p>
            <Badge className="mt-2" variant="outline">
              Indexed projection
            </Badge>
          </div>
        </div>
        <div className="space-y-3 p-6 text-sm sm:p-9">
          {profile.location ? (
            <p className="flex items-center gap-2">
              <MapPin className="size-4" /> {profile.location}
            </p>
          ) : null}
          {website ? (
            <a
              className="inline-flex items-center gap-2 underline underline-offset-4"
              href={website}
              rel="nofollow noopener noreferrer"
              target="_blank"
            >
              Website <ExternalLink className="size-3.5" />
              <span className="sr-only"> (opens in a new tab)</span>
            </a>
          ) : null}
          <p className="pt-3 text-xs leading-5 text-muted-foreground">
            Counts are derived by this instance and may lag the network or differ under local
            moderation. The DID above is the authoritative identity.
          </p>
        </div>
      </article>
    </main>
  )
}

function ProfileStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="bg-card p-5">
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      <p className="text-xs uppercase tracking-wider text-muted-foreground">{label}</p>
    </div>
  )
}

function safeWebsite(value?: string | null) {
  if (!value) return undefined
  try {
    const url = new URL(value)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined
  } catch {
    return undefined
  }
}
