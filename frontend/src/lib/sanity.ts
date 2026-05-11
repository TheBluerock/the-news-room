import { createClient } from '@sanity/client'

const projectId = import.meta.env.SANITY_PROJECT_ID
const dataset   = import.meta.env.SANITY_DATASET || 'production'
const token     = import.meta.env.SANITY_READ_TOKEN

export const sanity = createClient({
  projectId: projectId || 'placeholder',
  dataset,
  token,
  useCdn: true,
  apiVersion: '2021-06-07',
})

export const isSanityConfigured = Boolean(projectId)
