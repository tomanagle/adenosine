/**
 * TanStack Form reports validator output verbatim. Standard Schema validators
 * such as the generated Zod request schemas emit issue objects, while field
 * validators emit strings written for the reader, so strings are preferred and
 * schema issue text is the fallback.
 */
export function fieldErrorMessage(
  errors: readonly unknown[] | undefined,
  submitted = true,
): string | undefined {
  if (!submitted) return undefined
  const list = errors ?? []
  for (const error of list) {
    if (typeof error === 'string' && error) return error
  }
  for (const error of list) {
    if (error && typeof error === 'object' && 'message' in error) {
      const message = (error as { message?: unknown }).message
      if (typeof message === 'string' && message) return message
    }
  }
  return undefined
}
