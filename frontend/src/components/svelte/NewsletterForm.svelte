<script lang="ts">
  import type { NewsletterCopy } from '../../i18n'

  type State = 'idle' | 'loading' | 'success' | 'error'

  let { copy, lang }: { copy: NewsletterCopy; lang: string } = $props()

  let email = $state('')
  let status = $state<State>('idle')
  let errorMsg = $state('')

  async function subscribe() {
    if (!email || status === 'loading') return
    status = 'loading'
    try {
      const res = await fetch('/api/subscribe', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, lang }),
      })
      if (!res.ok) throw new Error(await res.text())
      status = 'success'
    } catch (e) {
      errorMsg = copy.err
      status = 'error'
    }
  }
</script>

<div class="form">
  <p>{copy.body}</p>

  {#if status === 'success'}
    <div class="success">{copy.ok}</div>
  {:else}
    <div class="field">
      <input
        type="email"
        placeholder={copy.placeholder}
        bind:value={email}
        onkeydown={(e) => e.key === 'Enter' && subscribe()}
        disabled={status === 'loading'}
      />
      <button onclick={subscribe} disabled={status === 'loading'}>
        {status === 'loading' ? '…' : copy.btn}
      </button>
    </div>
    {#if status === 'error'}
      <span style="color:var(--accent);font-size:12px">{errorMsg}</span>
    {/if}
  {/if}

  <small>{copy.gdpr}</small>
</div>
