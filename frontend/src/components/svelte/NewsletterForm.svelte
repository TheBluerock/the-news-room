<script lang="ts">
  type State = 'idle' | 'loading' | 'success' | 'error'

  let { lang }: { lang: 'it' | 'en' | 'zh' } = $props()

  let email = $state('')
  let status = $state<State>('idle')
  let errorMsg = $state('')

  const COPY = {
    it: {
      body: 'Una newsletter sobria: tre articoli, un vino della settimana, due eventi. Niente push, niente spam, possibilità di disiscriversi con un clic.',
      ph:   'la-tua@email.it',
      btn:  'Iscriviti',
      ok:   '✓ Grazie. Ti scriveremo venerdì.',
      err:  'Errore. Riprova tra poco.',
      gdpr: 'Trattamento dati ai sensi del Reg. UE 2016/679 · vedi privacy policy',
    },
    en: {
      body: 'A sober newsletter: three articles, one wine of the week, two events. No push, no spam, one-click unsubscribe.',
      ph:   'your@email.com',
      btn:  'Subscribe',
      ok:   '✓ Thank you. We\'ll write to you on Friday.',
      err:  'Error. Please try again shortly.',
      gdpr: 'Data handled under EU Reg. 2016/679 · see privacy policy',
    },
    zh: {
      body: '一份克制的邮件简报：三篇文章、本周一款酒、两场活动。无推送、无垃圾邮件、一键退订。',
      ph:   'your@email.com',
      btn:  '订阅',
      ok:   '✓ 感谢订阅。我们将于本周五与您联系。',
      err:  '出现错误，请稍后重试。',
      gdpr: '依据欧盟 2016/679 号条例处理个人数据 · 详见隐私政策',
    },
  }

  const copy = $derived(COPY[lang])

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
        placeholder={copy.ph}
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
