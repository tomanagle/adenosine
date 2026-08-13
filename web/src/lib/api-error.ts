import type { ErrorResponse } from '@adenosine/api-client'

/**
 * The API answers failures with an ErrorResponse envelope. Mutations throw that
 * envelope, so surface its message and keep transport failures readable.
 */
export function apiErrorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'error' in error) {
    const detail = (error as ErrorResponse).error
    if (detail && typeof detail.message === 'string' && detail.message) return detail.message
  }
  if (error instanceof Error && error.message) return error.message
  return fallback
}
