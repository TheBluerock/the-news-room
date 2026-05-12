import type { PageServerLoad } from './$types'
import { apiFetch, ApiError } from '$lib/api'
import type { MarketAnalytics } from '$lib/types'
import { MARKETS } from '$lib/types'

export const load: PageServerLoad = async ({ cookies }) => {
  const token = cookies.get('token') ?? ''

  const results = await Promise.allSettled(
    MARKETS.map((m) => apiFetch<MarketAnalytics>(`/api/analytics/market/${m}`, token)),
  )

  const markets = results.map((r, i) => {
    if (r.status === 'fulfilled') return r.value
    const notImpl = r.reason instanceof ApiError && r.reason.status === 404
    return {
      market: MARKETS[i],
      article_count_30d: 0,
      avg_quality_score: 0,
      pending_queue: 0,
      top_rejection_reasons: [],
      unimplemented: notImpl,
    } satisfies MarketAnalytics & { unimplemented: boolean }
  })

  const unimplemented = results.every(
    (r) => r.status === 'rejected' && r.reason instanceof ApiError && r.reason.status === 404,
  )

  return { markets, unimplemented }
}
