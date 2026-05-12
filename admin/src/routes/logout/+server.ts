import { redirect } from '@sveltejs/kit'
import type { RequestHandler } from './$types'

export const GET: RequestHandler = ({ cookies }) => {
  cookies.delete('token', { path: '/' })
  cookies.delete('refresh_token', { path: '/' })
  redirect(303, '/login')
}
