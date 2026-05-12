<script lang="ts">
  import type { PageData, ActionData } from './$types'
  import { enhance } from '$app/forms'
  import { goto } from '$app/navigation'

  const { data, form }: { data: PageData; form: ActionData } = $props()

  const MARKETS = ['italy', 'usa', 'china']

  function fmt(iso: string) {
    return new Date(iso).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'short' })
  }

  let showForm = $state(false)
</script>

<div class="page">
  <div class="header">
    <h1>Editorial Calendar</h1>
    <div class="controls">
      <select value={data.market} onchange={(e) => goto(`?market=${(e.target as HTMLSelectElement).value}`)}>
        {#each MARKETS as m}
          <option value={m}>{m}</option>
        {/each}
      </select>
      <button class="btn-new" onclick={() => (showForm = !showForm)}>
        {showForm ? 'Cancel' : '+ New entry'}
      </button>
    </div>
  </div>

  {#if form?.error}
    <div class="error">{form.error}</div>
  {/if}

  {#if showForm}
    <div class="card form-card">
      <h2>Schedule coverage</h2>
      <form method="POST" action="?/create" use:enhance onsubmit={() => (showForm = false)}>
        <input type="hidden" name="market" value={data.market} />
        <div class="row">
          <label>
            Topic
            <input type="text" name="topic_name" required placeholder="e.g. Barolo 2021 vintage" />
          </label>
          <label>
            Scheduled at
            <input type="datetime-local" name="scheduled_at" required />
          </label>
        </div>
        <label>
          Angle <span class="opt">(optional)</span>
          <input type="text" name="angle" placeholder="e.g. Producer profile — Giacomo Conterno" />
        </label>
        <label>
          Source URL <span class="opt">(optional)</span>
          <input type="url" name="source_url" placeholder="https://..." />
        </label>
        <button type="submit">Schedule</button>
      </form>
    </div>
  {/if}

  {#if data.unimplemented}
    <div class="notice">
      <strong>API not yet implemented</strong> — <code>GET/POST/DELETE /api/calendar/:market</code>
      needs to be added to the analytics service. See ADMIN-01 + PHASE4-00.
    </div>
  {:else if data.entries.length === 0}
    <div class="empty">No calendar entries for {data.market}.</div>
  {:else}
    <table>
      <thead>
        <tr>
          <th>Topic</th>
          <th>Angle</th>
          <th>Scheduled</th>
          <th>Status</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each data.entries as entry}
          <tr class:dispatched={entry.dispatched}>
            <td>{entry.topic_name}</td>
            <td class="muted">{entry.angle ?? '—'}</td>
            <td class="mono">{fmt(entry.scheduled_at)}</td>
            <td>
              {#if entry.dispatched}
                <span class="badge done">Dispatched</span>
              {:else}
                <span class="badge pending">Pending</span>
              {/if}
            </td>
            <td>
              {#if !entry.dispatched}
                <form method="POST" action="?/delete" use:enhance>
                  <input type="hidden" name="market" value={data.market} />
                  <input type="hidden" name="id" value={entry.id} />
                  <button class="btn-del">Delete</button>
                </form>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .page { max-width: 960px; }
  .header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }
  h1 { font-size: 20px; font-weight: 700; }
  .controls { display: flex; gap: 10px; align-items: center; }
  select { padding: 6px 10px; border: 1px solid #ddd; border-radius: 5px; font-size: 13px; }
  .btn-new { padding: 7px 14px; background: #6366f1; color: #fff; border: none; border-radius: 5px; font-size: 13px; font-weight: 600; cursor: pointer; }
  .btn-new:hover { background: #4f52d0; }
  .card { background: #fff; border-radius: 8px; padding: 24px; box-shadow: 0 1px 4px rgba(0,0,0,.06); margin-bottom: 20px; }
  .form-card h2 { font-size: 15px; font-weight: 600; margin-bottom: 16px; }
  .form-card form { display: flex; flex-direction: column; gap: 14px; }
  .row { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  label { display: flex; flex-direction: column; gap: 5px; font-size: 12px; font-weight: 600; color: #555; text-transform: uppercase; letter-spacing: .05em; }
  input { padding: 8px 10px; border: 1px solid #ddd; border-radius: 5px; font-size: 13px; }
  input:focus { border-color: #6366f1; outline: none; }
  .opt { font-weight: 400; text-transform: none; letter-spacing: 0; color: #999; }
  button[type="submit"] { padding: 8px 18px; background: #6366f1; color: #fff; border: none; border-radius: 5px; font-size: 13px; font-weight: 600; cursor: pointer; align-self: flex-start; }
  table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
  th { background: #f9f9fb; padding: 10px 14px; text-align: left; font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: #888; border-bottom: 1px solid #eee; }
  td { padding: 10px 14px; border-bottom: 1px solid #f0f0f0; }
  tr.dispatched td { opacity: 0.55; }
  .mono { font-family: monospace; font-size: 12px; }
  .muted { color: #888; font-size: 13px; }
  .badge { padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; }
  .badge.done { background: #d1fae5; color: #065f46; }
  .badge.pending { background: #e0e7ff; color: #3730a3; }
  .btn-del { padding: 3px 10px; background: #fef2f2; color: #b91c1c; border: 1px solid #fecaca; border-radius: 4px; font-size: 12px; cursor: pointer; }
  .btn-del:hover { background: #fee2e2; }
  .notice { padding: 14px 16px; background: #fffbeb; border: 1px solid #fde68a; border-radius: 6px; font-size: 13px; color: #92400e; }
  .notice code { background: #fef3c7; padding: 1px 5px; border-radius: 3px; }
  .error { padding: 10px 14px; background: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; color: #b91c1c; font-size: 13px; margin-bottom: 16px; }
  .empty { color: #888; font-size: 14px; }
</style>
