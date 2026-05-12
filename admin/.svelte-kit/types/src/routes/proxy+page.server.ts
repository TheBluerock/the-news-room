// @ts-nocheck
import { redirect } from '@sveltejs/kit'
import type { PageServerLoad } from './$types'

export const load = () => {
  redirect(302, '/audit')
}
;null as any as PageServerLoad;