import type { Handle } from '@sveltejs/kit'
import { redirect } from '@sveltejs/kit'
import { verifyToken } from '$lib/auth'
import { env } from '$env/dynamic/private'

const PUBLIC = ['/login', '/api/health', '/logout']

export const handle: Handle = async ({ event, resolve }) => {
  const path = event.url.pathname
  if (PUBLIC.some((p) => path.startsWith(p))) {
    return resolve(event)
  }

  const token = event.cookies.get('token')
  const refreshToken = event.cookies.get('refresh_token')

  let user: App.Locals['user'] = null

  if (token) {
    user = await verifyToken(token)
  }

  if (!user && refreshToken) {
    const newToken = await refreshAccessToken(refreshToken)
    if (newToken) {
      user = await verifyToken(newToken)
      if (user) {
        event.cookies.set('token', newToken, cookieOpts(60 * 15))
      }
    }
  }

  event.locals.user = user

  if (!user) {
    redirect(303, '/login')
  }

  return resolve(event)
}

function cookieOpts(maxAge: number) {
  return {
    httpOnly: true,
    path: '/',
    sameSite: 'strict' as const,
    secure: process.env.NODE_ENV === 'production',
    maxAge,
  }
}

async function refreshAccessToken(refreshToken: string): Promise<string | null> {
  const base = env.API_BASE ?? 'http://caddy'
  try {
    const res = await fetch(`${base}/api/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: refreshToken }),
    })
    if (!res.ok) return null
    const { access_token } = (await res.json()) as { access_token?: string }
    return access_token ?? null
  } catch {
    return null
  }
}
