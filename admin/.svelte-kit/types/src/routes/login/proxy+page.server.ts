// @ts-nocheck
import { fail, redirect } from '@sveltejs/kit'
import type { Actions, PageServerLoad } from './$types'
import { env } from '$env/dynamic/private'

export const load = ({ locals }: Parameters<PageServerLoad>[0]) => {
  if (locals.user) redirect(302, '/audit')
  return {}
}

export const actions = {
  default: async ({ request, cookies }: import('./$types').RequestEvent) => {
    const data = await request.formData()
    const email = String(data.get('email') ?? '')
    const password = String(data.get('password') ?? '')

    if (!email || !password) {
      return fail(400, { error: 'Email and password required' })
    }

    const base = env.API_BASE ?? 'http://caddy'
    let res: Response
    try {
      res = await fetch(`${base}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      })
    } catch {
      return fail(503, { error: 'Auth service unavailable' })
    }

    if (!res.ok) {
      return fail(401, { error: 'Invalid credentials' })
    }

    const { access_token, refresh_token } = (await res.json()) as {
      access_token: string
      refresh_token: string
    }

    const base_opts = {
      httpOnly: true,
      path: '/',
      sameSite: 'strict' as const,
      secure: process.env.NODE_ENV === 'production',
    }
    cookies.set('token', access_token, { ...base_opts, maxAge: 60 * 15 })
    cookies.set('refresh_token', refresh_token, { ...base_opts, maxAge: 60 * 60 * 24 * 7 })

    redirect(303, '/audit')
  },
}
;null as any as Actions;