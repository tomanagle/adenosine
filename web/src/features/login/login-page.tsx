import { useState, type FormEvent } from 'react'
import { startAtProtoLogin } from '@adenosine/api-client'
import { ArrowLeft } from 'lucide-react'
import { Link } from '@tanstack/react-router'

import { browserApiClient } from '@/api/browser-client'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'

export function LoginPage() {
  const [identifier, setIdentifier] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    setPending(true)
    const result = await startAtProtoLogin({ client: browserApiClient, body: { identifier } })
    setPending(false)
    if (result.data) window.location.assign(result.data.authorization_url)
    else setError('Sign-in could not be started. Check the handle and try again.')
  }

  return (
    <main className="grid min-h-screen place-items-center px-5 py-12">
      <div className="w-full max-w-md">
        <Link
          to="/"
          className="mb-5 inline-flex items-center gap-2 text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
        >
          <ArrowLeft className="size-4" />
          Back to Adenosine
        </Link>
        <Card>
          <CardHeader>
            <CardTitle className="font-serif text-2xl">Sign in with AT Protocol</CardTitle>
            <CardDescription>
              Use your handle or DID. You will continue with your identity provider.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={submit}>
              <div className="space-y-2">
                <label className="text-sm font-medium" htmlFor="identifier">
                  Handle or DID
                </label>
                <input
                  id="identifier"
                  name="identifier"
                  autoComplete="username"
                  required
                  value={identifier}
                  onChange={(event) => setIdentifier(event.target.value)}
                  placeholder="alice.example"
                  className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none placeholder:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              {error && (
                <Alert>
                  <AlertTitle>Unable to sign in</AlertTitle>
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
              <Button className="w-full" disabled={pending}>
                {pending ? 'Starting sign-in...' : 'Continue'}
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    </main>
  )
}
