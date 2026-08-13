import * as React from 'react'

import { cn } from '@/lib/utils'

/**
 * Presentational wrapper that ties a label, hint, and validation message to one
 * control so every form on the site reports errors the same way.
 */
function Field({
  children,
  className,
  error,
  hint,
  htmlFor,
  label,
}: {
  children: React.ReactNode
  className?: string
  error?: string
  hint?: string
  htmlFor: string
  label: string
}) {
  return (
    <div className={cn('space-y-1.5', className)}>
      <label className="block text-sm font-medium" htmlFor={htmlFor}>
        {label}
      </label>
      {children}
      {error ? (
        <p className="text-xs text-danger" id={`${htmlFor}-error`}>
          {error}
        </p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground" id={`${htmlFor}-hint`}>
          {hint}
        </p>
      ) : null}
    </div>
  )
}

export { Field }
