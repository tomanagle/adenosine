import * as React from 'react'

import { cn } from '@/lib/utils'

const controlClassName =
  'w-full rounded-md border border-input bg-background px-3 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-danger'

function Input({ className, ...props }: React.ComponentProps<'input'>) {
  return <input className={cn(controlClassName, 'h-10', className)} {...props} />
}

function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return (
    <textarea className={cn(controlClassName, 'min-h-24 py-2 leading-6', className)} {...props} />
  )
}

function Select({ className, ...props }: React.ComponentProps<'select'>) {
  return <select className={cn(controlClassName, 'h-10 pr-8', className)} {...props} />
}

export { Input, Select, Textarea, controlClassName }
