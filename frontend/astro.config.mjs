import { defineConfig } from 'astro/config'
import svelte from '@astrojs/svelte'
import vercel from '@astrojs/vercel'

export default defineConfig({
  site: process.env.SITE_URL || 'https://enoica.it',
  integrations: [svelte()],
  output: 'static',
  adapter: vercel(),
  build: {
    format: 'directory',
  },
  image: {
    domains: ['cdn.sanity.io'],
  },
})
