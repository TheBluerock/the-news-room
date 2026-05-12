import type { PageServerLoad, Actions } from './$types'
import { apiFetch, ApiError } from '$lib/api'
import { fail } from '@sveltejs/kit'
import type { ModerationItem } from '$lib/types'

export const load: PageServerLoad = async ({ cookies, url }) => {
  const token = cookies.get('token') ?? ''
  const market = url.searchParams.get('market') ?? ''

  const qs = new URLSearchParams({ status: 'pending', ...(market && { market }) })

  try {
    const items = await apiFetch<ModerationItem[]>(`/api/moderation/queue?${qs}`, token)
    return { items, unimplemented: false, market }
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return { items: [], unimplemented: true, market }
    }
    throw e
  }
}

export const actions: Actions = {
  approve: async ({ cookies, request }) => {
    const token = cookies.get('token') ?? ''
    const data = await request.formData()
    const id = String(data.get('id') ?? '')
    try {
      await apiFetch(`/api/moderation/approve/${id}`, token, { method: 'POST' })
      return { success: true }
    } catch (e) {
      return fail(500, { error: e instanceof ApiError ? e.message : 'Unknown error' })
    }
  },
  reject: async ({ cookies, request }) => {
    const token = cookies.get('token') ?? ''
    const data = await request.formData()
    const id = String(data.get('id') ?? '')
    const reason = String(data.get('reason') ?? '')
    try {
      await apiFetch(`/api/moderation/reject/${id}`, token, {
        method: 'POST',
        body: JSON.stringify({ reason }),
      })
      return { success: true }
    } catch (e) {
      return fail(500, { error: e instanceof ApiError ? e.message : 'Unknown error' })
    }
  },
}
