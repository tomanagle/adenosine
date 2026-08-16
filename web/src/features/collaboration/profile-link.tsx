import { Link } from '@tanstack/react-router'

export function ProfileLink({ did }: { did: string }) {
  return (
    <Link
      className="font-mono text-xs underline-offset-4 hover:underline"
      params={{ identity: did }}
      to="/profiles/$identity"
    >
      {did}
    </Link>
  )
}
