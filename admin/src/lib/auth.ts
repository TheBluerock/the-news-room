import { importSPKI, jwtVerify, type JWTPayload, type KeyLike } from 'jose'
import { readFileSync } from 'node:fs'
import { env } from '$env/dynamic/private'

interface NewsClaims extends JWTPayload {
  market?: string
  role?: string
}

let _key: KeyLike | null = null

async function getPublicKey(): Promise<KeyLike> {
  if (_key !== null) return _key
  let pem = ''
  try {
    pem = readFileSync('/vault/secrets/jwt_public_key', 'utf-8').trim()
  } catch {
    pem = (env.JWT_PUBLIC_KEY ?? '').trim()
  }
  if (!pem) throw new Error('JWT_PUBLIC_KEY not configured')
  _key = await importSPKI(pem, 'RS256')
  return _key
}

export async function verifyToken(token: string): Promise<App.Locals['user']> {
  try {
    const key = await getPublicKey()
    const { payload } = await jwtVerify<NewsClaims>(token, key, { algorithms: ['RS256'] })
    if (payload.role !== 'admin') return null
    return {
      id: payload.sub ?? '',
      market: payload.market ?? '',
      role: payload.role,
    }
  } catch {
    return null
  }
}
