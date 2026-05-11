import type { APIRoute } from 'astro'

export const prerender = false

export const POST: APIRoute = async ({ request }) => {
  const { email, lang } = await request.json() as { email?: string; lang?: string }

  if (!email || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
    return new Response('Invalid email', { status: 400 })
  }

  const apiKey  = import.meta.env.BREVO_API_KEY
  const listId  = Number(import.meta.env.BREVO_LIST_ID)

  if (!apiKey || !listId) {
    return new Response('Newsletter not configured', { status: 503 })
  }

  const res = await fetch('https://api.brevo.com/v3/contacts', {
    method: 'POST',
    headers: {
      'api-key': apiKey,
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    },
    body: JSON.stringify({
      email,
      listIds: [listId],
      updateEnabled: true,
      attributes: { LANGUAGE: lang ?? 'it' },
    }),
  })

  if (!res.ok && res.status !== 204) {
    const body = await res.text()
    console.error('Brevo error', res.status, body)
    return new Response('Upstream error', { status: 502 })
  }

  return new Response(null, { status: 204 })
}
