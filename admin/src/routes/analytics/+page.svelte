<script lang="ts">
  import type { PageData } from './$types'

  const { data }: { data: PageData } = $props()

  function scoreColor(s: number) {
    if (s >= 0.8) return '#065f46'
    if (s >= 0.6) return '#92400e'
    return '#991b1b'
  }
  function scoreBg(s: number) {
    if (s >= 0.8) return '#d1fae5'
    if (s >= 0.6) return '#fef3c7'
    return '#fee2e2'
  }
</script>

<div class="page">
  <h1>Analytics</h1>

  {#if data.unimplemented}
    <div class="notice">
      <strong>API not yet implemented</strong> — <code>GET /api/analytics/market/:market</code>
      needs to be added to the analytics service. See ADMIN-01.
    </div>
  {:else}
    <div class="grid">
      {#each data.markets as m}
        <div class="card">
          <div class="market-name">{m.market}</div>
          <div class="stats">
            <div class="stat">
              <div class="stat-value">{m.article_count_30d}</div>
              <div class="stat-label">Articles (30d)</div>
            </div>
            <div class="stat">
              <div
                class="stat-value"
                style="color: {scoreColor(m.avg_quality_score)}; background: {scoreBg(m.avg_quality_score)}; padding: 2px 10px; border-radius: 6px;"
              >
                {m.avg_quality_score.toFixed(2)}
              </div>
              <div class="stat-label">Avg quality</div>
            </div>
            <div class="stat">
              <div class="stat-value">{m.pending_queue}</div>
              <div class="stat-label">Pending</div>
            </div>
          </div>
          {#if m.top_rejection_reasons.length > 0}
            <div class="reasons">
              <div class="reasons-label">Top rejection reasons</div>
              {#each m.top_rejection_reasons as r}
                <div class="reason-row">
                  <span class="reason-name">{r.reason}</span>
                  <span class="reason-count">{r.count}</span>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .page { max-width: 960px; }
  h1 { font-size: 20px; font-weight: 700; margin-bottom: 24px; }
  .grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 20px; }
  .card { background: #fff; border-radius: 8px; padding: 24px; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
  .market-name { font-size: 13px; font-weight: 700; text-transform: uppercase; letter-spacing: .08em; color: #6366f1; margin-bottom: 20px; }
  .stats { display: flex; gap: 20px; margin-bottom: 20px; }
  .stat { flex: 1; text-align: center; }
  .stat-value { font-size: 22px; font-weight: 700; color: #1a1a2e; display: inline-block; }
  .stat-label { font-size: 11px; color: #999; text-transform: uppercase; letter-spacing: .05em; margin-top: 4px; }
  .reasons { border-top: 1px solid #f0f0f0; padding-top: 14px; }
  .reasons-label { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: #999; margin-bottom: 8px; }
  .reason-row { display: flex; justify-content: space-between; align-items: center; padding: 4px 0; font-size: 12px; }
  .reason-name { color: #555; }
  .reason-count { background: #f0f0f5; padding: 1px 8px; border-radius: 10px; font-size: 11px; font-weight: 600; color: #666; }
  .notice { padding: 14px 16px; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; font-size: 13px; color: #92400e; }
  .notice code { background: #fef3c7; padding: 1px 5px; border-radius: 3px; }
</style>
