import * as React from 'react'

import { cn } from '@/lib/utils'

function Alert({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      role="alert"
      className={cn('relative w-full rounded-lg border p-4 text-sm', className)}
      {...props}
    />
  )
}

function AlertTitle({ className, children, ...props }: React.ComponentProps<'h5'>) {
  return (
    <h5 className={cn('mb-1 font-medium leading-none tracking-tight', className)} {...props}>
      {children}
    </h5>
  )
}

function AlertDescription({ className, ...props }: React.ComponentProps<'div'>) {
  return <div className={cn('text-sm text-muted-foreground', className)} {...props} />
}

export { Alert, AlertDescription, AlertTitle }
