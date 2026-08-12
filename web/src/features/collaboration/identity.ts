import { z } from 'zod'

const atUri = z
  .string()
  .regex(/^at:\/\/did:[a-z0-9]+:[A-Za-z0-9._:%-]+\/[a-zA-Z0-9.-]+\/[A-Za-z0-9._~:@!$&'()*+,;=-]+$/)

export const didSchema = z.string().regex(/^did:[a-z0-9]+:[A-Za-z0-9._:%-]+$/)

export function encodeRecordIdentity(uri: string) {
  return btoa(atUri.parse(uri)).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/u, '')
}

export function decodeRecordIdentity(value: string) {
  if (!/^[A-Za-z0-9_-]+$/u.test(value) || value.length > 4096)
    throw new Error('Invalid record identity')
  const base64 = value.replaceAll('-', '+').replaceAll('_', '/')
  return atUri.parse(atob(base64.padEnd(Math.ceil(base64.length / 4) * 4, '=')))
}
