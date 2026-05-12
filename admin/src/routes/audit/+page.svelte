<script lang="ts">
  import type { PageData } from './$types'
  import { goto } from '$app/navigation'
  import { page } from '$app/stores'

  const { data }: { data: PageData } = $props()

  const EVENT_TYPES = [
    'article.approved',
    'article.rejected',
    'editor.correction',
    'role.changed',
    'correction.reversed',
  ]

  const MARKETS = ['italy', 'usa', 'china']

  function navigate(params: Record<string, string>) {
    const sp = new URLSearchParams($page.url.searchParams)
    for (const [k, v] of Object.entries(params)) {
      v ? sp.set(k, v) : sp.delete(k)
    }
    sp.delete('page')
    goto(`?${sp}`)
  }

  function fmt(iso: string) {
    return new Date(iso).toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short' })
  }
</script>

<div class="page">
  <div class="header">
    <h1>Audit Log</h1>
    <div class="filters">
      <select
        value={data.event_type}
        onchange={(e) => navigate({ event_type: (e.target as HTMLSelectElement).value })}
      >
        <option value="">All events</option>
        {#each EVENT_TYPES as et}
          <option value={et}>{et}</option>
        {/each}
      </select>
      <select
        value={data.market}
        onchange={(e) => navigate({ market: (e.target as HTMLSelectElement).value })}
      >
        <option value="">All markets</option>
        {#each MARKETS as m}
          <option value={m}>{m}</option>
        {/each}
      </select>
    </div>
  </div>

  {#if data.unimplemented}
    <div class="notice">
      <strong>API not yet implemented</strong> — <code>GET /api/admin/audit</code> needs to be added
      to the auth service. See ADMIN-01.
    </div>
  {:else if data.entries.length === 0}
    <div class="empty">No audit entries found.</div>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Time</th>
          <th>Event</th>
          <th>Actor</th>
          <th>Market</th>
          <th>Payload</th>
        </tr>
      </thead>
      <tbody>
        {#each data.entries as entry}
          <tr>
            <td class="mono">{fmt(entry.created_at)}</td>
            <td><span class="badge">{entry.event_type}</span></td>
            <td class="mono">{entry.actor_id}</td>
            <td>{entry.market}</td>
            <td class="mono payload">{JSON.stringify(entry.payload)}</td>
          </tr>
        {/each}
      </tbody>
    </table>

    <div class="pager">
      {#if data.page > 1}
        <a href="?page={data.page - 1}">← Prev</a>
      {/if}
      <span>Page {data.page} · {data.total} entries</span>
      {#if data.page * data.limit < data.total}
        <a href="?page={data.page + 1}">Next →</a>
      {/if}
    </div>
  {/if}
</div>

<style>
  .page { max-width: 1100px; }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
  h1 { font-size: 20px; font-weight: 700; }
  .filters { display: flex; gap: 10px; }
  select { padding: 6px 10px; border: 1px solid #ddd; border-radius: 5px; font-size: 13px; }
  table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
  th { background: #f9f9fb; padding: 10px 14px; text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: #888; border-bottom: 1px solid #eee; }
  td { padding: 10px 14px; border-bottom: 1px solid #f0f0f0; vertical-align: top; }
  .mono { font-family: monospace; font-size: 12px; }
  .payload { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #888; }
  .badge { background: #ede9fe; color: #5b21b6; padding: 2px 7px; border-radius: 4px; font-size: 11px; white-space: nowrap; }
  .pager { margin-top: 16px; display: flex; align-items: center; gap: 16px; font-size: 13px; color: #888; }
  .pager a { color: #6366f1; text-decoration: none; }
  .notice { padding: 14px 16px; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; font-size: 13px; color: #92400e; }
  .notice code { background: #fef3c7; padding: 1px 5px; border-radius: 3px; }
  .empty { color: #888; font-size: 14px; }
</style>
