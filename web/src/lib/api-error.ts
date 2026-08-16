import { zErrorResponse } from '@adenosine/api-client/schemas'

/**
 * The API answers failures with an ErrorResponse envelope. Mutations throw that
 * envelope, so surface its message and keep transport failures readable.
 */
export function apiErrorMessage<T>(error: T, fallback: string): string {
  const response = zErrorResponse.safeParse(error)
  if (response.success && response.data.error.message) return response.data.error.message
  if (error instanceof Error && error.message) return error.message
  return fallback
}
