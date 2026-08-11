import { z } from 'zod'

const safeString = (maxLength: number) =>
  z
    .string()
    .min(1)
    .max(maxLength)
    .refine((value) => !hasControlCharacters(value), 'Control characters are not allowed')

export const repositoryParamsSchema = z.object({
  owner: safeString(255),
  repo: z.string().regex(/^[a-z0-9][a-z0-9._-]{0,99}$/),
})

export const revisionSchema = safeString(1024).refine(
  (value) => !value.startsWith('-'),
  'Option-like revisions are not allowed',
)

export const repositoryRevisionParamsSchema = repositoryParamsSchema.extend({
  revision: revisionSchema,
})

export const treeSearchSchema = z.object({
  ref: revisionSchema.optional(),
})

export const commitsSearchSchema = z.object({
  ref: revisionSchema.optional(),
  limit: z.coerce.number().int().min(1).max(100).default(30),
})

export const compareSearchSchema = z.object({
  base: revisionSchema.optional(),
  head: revisionSchema.optional(),
})

export function parseRepositoryPath(value: string | undefined): string {
  if (!value) return ''
  if (value.startsWith('/')) throw new Error('Invalid repository path')
  const path = value.replace(/\/+$/g, '')
  if (
    path.length > 4096 ||
    hasControlCharacters(path) ||
    path.split('/').some((segment) => segment === '' || segment === '.' || segment === '..')
  ) {
    throw new Error('Invalid repository path')
  }
  return path
}

function hasControlCharacters(value: string) {
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0
    if (code <= 31 || code === 127) return true
  }
  return false
}

export function splitBlobPath(value: string | undefined) {
  const path = parseRepositoryPath(value)
  const separator = path.lastIndexOf('/')
  return {
    path,
    parentPath: separator === -1 ? '' : path.slice(0, separator),
    name: separator === -1 ? path : path.slice(separator + 1),
  }
}
