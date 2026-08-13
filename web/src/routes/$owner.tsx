import { createFileRoute, Outlet } from '@tanstack/react-router'

import { ownerPathSchema } from '@/features/repository-browser/validation'

export const Route = createFileRoute('/$owner')({
  params: { parse: (params) => ({ owner: ownerPathSchema.parse(params.owner) }) },
  component: Outlet,
})
