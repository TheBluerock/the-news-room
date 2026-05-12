<script lang="ts">
  import type { PageData, ActionData } from './$types'
  import { enhance } from '$app/forms'
  import { goto } from '$app/navigation'
  import { page } from '$app/stores'

  const { data, form }: { data: PageData; form: ActionData } = $props()

  const MARKETS = ['italy', 'usa', 'china']

  function fmt(iso: string) {
    return new Date(iso).toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short' })
  }
</script>

<div class="page">
  <div class="header">
    <h1>Moderation Queue</h1>
    <select
      value={data.market}
      onchange={(e) => goto(`?market=${(e.target as HTMLSelectElement).value}`)}
    >
      <option value="">All markets</option>
      {#each MARKETS as m}
        <option value={m}>{m}</option>
      {/each}
    </select>
  </div>

  {#if form?.error}
    <div class="error">{form.error}</div>
  {/if}

  {#if data.unimplemented}
    <div class="notice">
      <strong>API not yet implemented</strong> — <code>GET /api/moderation/queue</code> needs an
      HTTP server in the moderation service. See ADMIN-01.
    </div>
  {:else if data.items.length === 0}
    <div class="empty">No articles pending moderation.</div>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Article ID</th>
          <th>Market</th>
          <th>Topic</th>
          <th>Score</th>
          <th>Flags</th>
          <th>Received</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        {#each data.items as item}
          <tr>
            <td class="mono">{item.article_id}</td>
            <td>{item.market}</td>
            <td>{item.topic}</td>
            <td>
              <span class="score" class:low={item.score < 0.6}>{item.score.toFixed(2)}</span>
            </td>
            <td>
              {#each item.rejection_reasons as r}
                <span class="flag">{r}</span>
              {/each}
            </td>
            <td class="mono">{fmt(item.created_at)}</td>
            <td class="actions">
              <form method="POST" action="?/approve" use:enhance>
                <input type="hidden" name="id" value={item.id} />
                <button class="btn-approve">Approve</button>
              </form>
              <form method="POST" action="?/reject" use:enhance>
                <input type="hidden" name="id" value={item.id} />
                <input
                  name="reason"
                  placeholder="Reason"
                  class="reason-input"
                  required
                />
                <button class="btn-reject">Reject</button>
              </form>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .page { max-width: 1200px; }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
  h1 { font-size: 20px; font-weight: 700; }
  select { padding: 6px 10px; border: 1px solid #ddd; border-radius: 5px; font-size: 13px; }
  table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
  th { background: #f9f9fb; padding: 10px 14px; text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: #888; border-bottom: 1px solid #eee; }
  td { padding: 10px 14px; border-bottom: 1px solid #f0f0f0; vertical-align: middle; }
  .mono { font-family: monospace; font-size: 12px; }
  .score { background: #d1fae5; color: #065f46; padding: 2px 7px; border-radius: 4px; font-size: 12px; font-weight: 600; }
  .score.low { background: #fee2e2; color: #991b1b; }
  .flag { background: #fef3c7; color: #92400e; padding: 1px 6px; border-radius: 4px; font-size: 11px; margin-right: 4px; }
  .actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .actions form { display: flex; gap: 6px; align-items: center; }
  .reason-input { padding: 4px 8px; border: 1px solid #ddd; border-radius: 4px; font-size: 12px; width: 120px; }
  .btn-approve { padding: 4px 12px; background: #10b981; color: #fff; border: none; border-radius: 4px; font-size: 12px; cursor: pointer; }
  .btn-approve:hover { background: #059669; }
  .btn-reject { padding: 4px 12px; background: #ef4444; color: #fff; border: none; border-radius: 4px; font-size: 12px; cursor: pointer; }
  .btn-reject:hover { background: #dc2626; }
  .notice { padding: 14px 16px; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; font-size: 13px; color: #92400e; }
  .notice code { background: #fef3c7; padding: 1px 5px; border-radius: 3px; }
  .error { padding: 10px 14px; background: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; color: #b91c1c; font-size: 13px; margin-bottom: 16px; }
  .empty { color: #888; font-size: 14px; }
</style>
