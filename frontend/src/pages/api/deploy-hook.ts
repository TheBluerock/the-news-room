import type { APIRoute } from 'astro'
import { Webhook } from 'svix'

export const prerender = false

export const POST: APIRoute = async ({ request }) => {
  const secret = import.meta.env.SANITY_WEBHOOK_SECRET
  const deployHookUrl = import.meta.env.VERCEL_DEPLOY_HOOK_URL

  if (!secret || !deployHookUrl) {
    return new Response('Webhook not configured', { status: 503 })
  }

  const body = await request.text()
  const wh = new Webhook(secret)

  let payload: Record<string, unknown>
  try {
    payload = wh.verify(body, {
      'svix-id':        request.headers.get('svix-id') ?? '',
      'svix-timestamp': request.headers.get('svix-timestamp') ?? '',
      'svix-signature': request.headers.get('svix-signature') ?? '',
    }) as Record<string, unknown>
  } catch {
    return new Response('Invalid signature', { status: 401 })
  }

  // Only redeploy on article or ad publish events
  const type = payload._type as string | undefined
  if (type !== 'article' && type !== 'ad') {
    return new Response(null, { status: 204 })
  }

  const res = await fetch(deployHookUrl, { method: 'POST' })
  if (!res.ok) {
    console.error('Vercel deploy hook failed', res.status)
    return new Response('Deploy hook error', { status: 502 })
  }

  return new Response(null, { status: 204 })
}
