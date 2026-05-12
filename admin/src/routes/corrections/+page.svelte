<script lang="ts">
  import type { PageData, ActionData } from './$types'
  import { enhance } from '$app/forms'

  const { data, form }: { data: PageData; form: ActionData } = $props()

  const MARKETS = ['italy', 'usa', 'china']
</script>

<div class="page">
  <div class="header">
    <div>
      <h1>Corrections</h1>
      <p class="sub">
        Publishes an <code>editor.correction</code> event to RedPanda. The agent picks it up on next
        run for the affected market (fast path: Redis, 48h TTL).
      </p>
    </div>
  </div>

  {#if form?.success}
    <div class="success">Correction submitted. Agent will apply it on next generation run.</div>
  {/if}

  {#if form?.error}
    <div class="error">{form.error}</div>
  {/if}

  <div class="card">
    <form method="POST" use:enhance>
      <label>
        Market
        <select name="market" required value={form?.market ?? ''}>
          <option value="">Select market</option>
          {#each MARKETS as m}
            <option value={m}>{m}</option>
          {/each}
        </select>
      </label>

      <label>
        Article ID
        <input
          type="text"
          name="article_id"
          placeholder="e.g. 01J3XKJFGQ..."
          required
          value={form?.article_id ?? ''}
        />
        <span class="hint">The Sanity document ID of the article to correct</span>
      </label>

      <label>
        Correction
        <textarea
          name="correction"
          rows="5"
          placeholder="Describe the factual or editorial correction. Be specific — the agent uses this verbatim in the next generation prompt."
          required
        >{form?.correction ?? ''}</textarea>
      </label>

      <button type="submit">Submit correction</button>
    </form>
  </div>
</div>

<style>
  .page { max-width: 640px; }
  .header { margin-bottom: 24px; }
  h1 { font-size: 20px; font-weight: 700; margin-bottom: 6px; }
  .sub { font-size: 13px; color: #666; line-height: 1.5; }
  .sub code { background: #f0f0f5; padding: 1px 5px; border-radius: 3px; font-size: 12px; }
  .card { background: #fff; border-radius: 8px; padding: 28px; box-shadow: 0 1px 4px rgba(0,0,0,.06); }
  form { display: flex; flex-direction: column; gap: 18px; }
  label { display: flex; flex-direction: column; gap: 6px; font-size: 12px; font-weight: 600; color: #555; text-transform: uppercase; letter-spacing: .05em; }
  input, select, textarea { padding: 9px 12px; border: 1px solid #ddd; border-radius: 5px; font-size: 14px; font-family: inherit; outline: none; resize: vertical; }
  input:focus, select:focus, textarea:focus { border-color: #6366f1; }
  .hint { font-size: 11px; font-weight: 400; text-transform: none; letter-spacing: 0; color: #999; margin-top: -2px; }
  button { padding: 10px 20px; background: #6366f1; color: #fff; border: none; border-radius: 5px; font-size: 14px; font-weight: 600; cursor: pointer; align-self: flex-start; }
  button:hover { background: #4f52d0; }
  .success { padding: 12px 16px; background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 6px; color: #15803d; font-size: 13px; margin-bottom: 20px; }
  .error { padding: 12px 16px; background: #fef2f2; border: 1px solid #fecaca; border-radius: 6px; color: #b91c1c; font-size: 13px; margin-bottom: 20px; }
</style>
