export interface AuditEntry {
  id: string
  event_type: string
  actor_id: string
  market: string
  payload: Record<string, unknown>
  created_at: string
}

export interface AuditPage {
  entries: AuditEntry[]
  total: number
  page: number
  limit: number
}

export interface ModerationItem {
  id: string
  article_id: string
  market: string
  topic: string
  status: 'pending' | 'approved' | 'rejected'
  score: number
  rejection_reasons: string[]
  created_at: string
}

export interface CalendarEntry {
  id: string
  market: string
  topic_name: string
  source_url: string | null
  angle: string | null
  journalist_profile_id: string | null
  scheduled_at: string
  dispatched: boolean
  created_at: string
}

export interface MarketAnalytics {
  market: string
  article_count_30d: number
  avg_quality_score: number
  pending_queue: number
  top_rejection_reasons: { reason: string; count: number }[]
}

export const MARKETS = ['italy', 'usa', 'china'] as const
export type Market = (typeof MARKETS)[number]
