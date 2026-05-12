// @ts-nocheck
import type { PageServerLoad, Actions } from './$types'
import { apiFetch, ApiError } from '$lib/api'
import { fail } from '@sveltejs/kit'
import type { CalendarEntry } from '$lib/types'

export const load = async ({ cookies, url }: Parameters<PageServerLoad>[0]) => {
  const token = cookies.get('token') ?? ''
  const market = url.searchParams.get('market') ?? 'italy'

  try {
    const entries = await apiFetch<CalendarEntry[]>(`/api/calendar/${market}`, token)
    return { entries, market, unimplemented: false }
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return { entries: [], market, unimplemented: true }
    }
    throw e
  }
}

export const actions = {
  create: async ({ cookies, request }: import('./$types').RequestEvent) => {
    const token = cookies.get('token') ?? ''
    const data = await request.formData()
    const market = String(data.get('market') ?? '')
    const topic_name = String(data.get('topic_name') ?? '').trim()
    const scheduled_at = String(data.get('scheduled_at') ?? '').trim()
    const angle = String(data.get('angle') ?? '').trim()
    const source_url = String(data.get('source_url') ?? '').trim()

    if (!market || !topic_name || !scheduled_at) {
      return fail(400, { error: 'Market, topic, and scheduled date required' })
    }

    try {
      await apiFetch(`/api/calendar/${market}`, token, {
        method: 'POST',
        body: JSON.stringify({
          topic_name,
          scheduled_at: new Date(scheduled_at).toISOString(),
          ...(angle && { angle }),
          ...(source_url && { source_url }),
        }),
      })
      return { success: true }
    } catch (e) {
      return fail(500, { error: e instanceof ApiError ? e.message : 'Unknown error' })
    }
  },
  delete: async ({ cookies, request }: import('./$types').RequestEvent) => {
    const token = cookies.get('token') ?? ''
    const data = await request.formData()
    const market = String(data.get('market') ?? '')
    const id = String(data.get('id') ?? '')
    try {
      await apiFetch(`/api/calendar/${market}/${id}`, token, { method: 'DELETE' })
      return { success: true }
    } catch (e) {
      return fail(500, { error: e instanceof ApiError ? e.message : 'Unknown error' })
    }
  },
}
;null as any as Actions;