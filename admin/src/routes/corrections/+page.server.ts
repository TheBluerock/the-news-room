import type { PageServerLoad, Actions } from './$types'
import { apiFetch, ApiError } from '$lib/api'
import { fail } from '@sveltejs/kit'

export const load: PageServerLoad = () => ({})

export const actions: Actions = {
  default: async ({ cookies, request }) => {
    const token = cookies.get('token') ?? ''
    const data = await request.formData()
    const article_id = String(data.get('article_id') ?? '').trim()
    const market = String(data.get('market') ?? '').trim()
    const correction = String(data.get('correction') ?? '').trim()

    if (!article_id || !market || !correction) {
      return fail(400, { error: 'All fields required', article_id, market, correction })
    }

    try {
      await apiFetch('/api/corrections', token, {
        method: 'POST',
        body: JSON.stringify({ article_id, market, correction }),
      })
      return { success: true }
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        return fail(501, {
          error: 'Corrections API not yet implemented — REF-01 pending',
          article_id,
          market,
          correction,
        })
      }
      return fail(500, {
        error: e instanceof ApiError ? e.message : 'Unknown error',
        article_id,
        market,
        correction,
      })
    }
  },
}
