import { z } from 'zod'

const fieldMessageSchema = z.object({ message: z.string() })

/**
 * TanStack Form reports validator output verbatim. Standard Schema validators
 * such as the generated Zod request schemas emit issue objects, while field
 * validators emit strings written for the reader, so strings are preferred and
 * schema issue text is the fallback.
 */
export function fieldErrorMessage<T>(
  errors: readonly T[] | undefined,
  submitted = true,
): string | undefined {
  if (!submitted) return undefined
  const list = errors ?? []
  for (const error of list) {
    const parsed = z.string().safeParse(error)
    if (parsed.success && parsed.data) return parsed.data
  }
  for (const error of list) {
    const parsed = fieldMessageSchema.safeParse(error)
    if (parsed.success && parsed.data.message) return parsed.data.message
  }
  return undefined
}
