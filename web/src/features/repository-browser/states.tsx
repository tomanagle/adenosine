import { AlertTriangle, FileQuestion, LockKeyhole, ServerOff } from 'lucide-react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

type BrowserErrorKind = 'forbidden' | 'missing' | 'oversized' | 'unavailable' | 'unknown'

export function classifyBrowserError(error: unknown): BrowserErrorKind {
  const value = error as {
    status?: number
    response?: { status?: number }
    error?: { code?: string }
    message?: string
  }
  const status = value?.status ?? value?.response?.status
  const code = value?.error?.code ?? ''
  const message = value?.message ?? ''
  if (
    status === 401 ||
    status === 403 ||
    /unauthorized|forbidden|authentication_required|permission_denied/.test(code)
  ) {
    return 'forbidden'
  }
  if (status === 404 || /not_found/.test(code)) return 'missing'
  if (status === 413 || code === 'git_output_too_large') return 'oversized'
  if (
    status === 502 ||
    status === 503 ||
    /unavailable|upstream/.test(code) ||
    /fetch|network|unavailable/i.test(message)
  ) {
    return 'unavailable'
  }
  return 'unknown'
}

const errorContent = {
  forbidden: {
    icon: LockKeyhole,
    title: 'This content is not available to you',
    description: 'Sign in with repository access, or ask the repository host for permission.',
  },
  missing: {
    icon: FileQuestion,
    title: 'Repository content not found',
    description: 'The repository, revision, or path may have moved or no longer exists.',
  },
  oversized: {
    icon: AlertTriangle,
    title: 'Diff exceeds the safe display limit',
    description:
      'Adenosine stopped this bounded read instead of returning an incomplete patch. Compare a narrower range.',
  },
  unavailable: {
    icon: ServerOff,
    title: 'Repository host unavailable',
    description:
      'The canonical host could not serve this content. Try again later or visit the host.',
  },
  unknown: {
    icon: AlertTriangle,
    title: 'Repository content could not be loaded',
    description:
      'The server rejected or could not complete this Git read. Check the URL and try again.',
  },
} as const

export function RepositoryError({ error }: { error: unknown }) {
  const content = errorContent[classifyBrowserError(error)]
  const Icon = content.icon
  return (
    <div className="mx-auto w-full max-w-6xl px-5 py-10 sm:px-8">
      <Alert className="bg-card">
        <Icon className="mb-3 size-5" aria-hidden="true" />
        <AlertTitle>{content.title}</AlertTitle>
        <AlertDescription>{content.description}</AlertDescription>
      </Alert>
    </div>
  )
}

export function RepositoryPending({ label = 'Loading repository content' }: { label?: string }) {
  return (
    <output className="mx-auto block w-full max-w-6xl px-5 py-10 sm:px-8">
      <div className="h-36 animate-pulse rounded-lg border bg-card" />
      <span className="sr-only">{label}</span>
    </output>
  )
}

export function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-lg border border-dashed px-5 py-12 text-center">
      <p className="font-medium">{title}</p>
      <p className="mt-1 text-sm text-muted-foreground">{description}</p>
    </div>
  )
}
