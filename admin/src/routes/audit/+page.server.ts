import type { PageServerLoad } from './$types'
import { apiFetch, ApiError } from '$lib/api'
import type { AuditPage } from '$lib/types'

export const load: PageServerLoad = async ({ cookies, url }) => {
  const token = cookies.get('token') ?? ''
  const page = Number(url.searchParams.get('page') ?? '1')
  const event_type = url.searchParams.get('event_type') ?? ''
  const market = url.searchParams.get('market') ?? ''

  const qs = new URLSearchParams({
    page: String(page),
    limit: '25',
    ...(event_type && { event_type }),
    ...(market && { market }),
  })

  try {
    const data = await apiFetch<AuditPage>(`/api/admin/audit?${qs}`, token)
    return { ...data, unimplemented: false, event_type, market }
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return { entries: [], total: 0, page: 1, limit: 25, unimplemented: true, event_type, market }
    }
    throw e
  }
}
