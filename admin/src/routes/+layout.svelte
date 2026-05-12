<script lang="ts">
  import { page } from '$app/stores'
  import type { LayoutData } from './$types'

  const { data, children }: { data: LayoutData; children: import('svelte').Snippet } = $props()

  const nav = [
    { href: '/audit', label: 'Audit Log', icon: '◎' },
    { href: '/moderation', label: 'Moderation', icon: '⊞' },
    { href: '/corrections', label: 'Corrections', icon: '✎' },
    { href: '/calendar', label: 'Calendar', icon: '◫' },
    { href: '/analytics', label: 'Analytics', icon: '◈' },
  ]
</script>

<div class="shell">
  <aside class="sidebar">
    <div class="brand">Newsroom<br /><span>Admin</span></div>
    <nav>
      {#each nav as item}
        <a
          href={item.href}
          class:active={$page.url.pathname.startsWith(item.href)}
        >
          <span class="icon">{item.icon}</span>
          {item.label}
        </a>
      {/each}
    </nav>
    <div class="user-block">
      <div class="user-id">{data.user?.id ?? ''}</div>
      <a href="/logout" class="logout">Sign out</a>
    </div>
  </aside>

  <main>
    {@render children()}
  </main>
</div>

<style>
  :global(*, *::before, *::after) {
    box-sizing: border-box;
    margin: 0;
    padding: 0;
  }
  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    font-size: 14px;
    color: #1a1a2e;
    background: #f4f4f8;
  }

  .shell {
    display: flex;
    min-height: 100vh;
  }

  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: #1a1a2e;
    color: #c8c8e0;
    display: flex;
    flex-direction: column;
    padding: 0;
  }

  .brand {
    padding: 24px 20px 20px;
    font-size: 15px;
    font-weight: 700;
    color: #fff;
    line-height: 1.3;
    border-bottom: 1px solid #2e2e50;
  }
  .brand span {
    font-weight: 400;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    color: #8888aa;
  }

  nav {
    flex: 1;
    padding: 12px 0;
  }

  nav a {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 20px;
    color: #9090b0;
    text-decoration: none;
    font-size: 13px;
    border-left: 3px solid transparent;
    transition: color 0.15s, background 0.15s;
  }
  nav a:hover {
    color: #e0e0f0;
    background: #22224a;
  }
  nav a.active {
    color: #fff;
    border-left-color: #6366f1;
    background: #22224a;
  }
  .icon {
    font-size: 15px;
    width: 18px;
    text-align: center;
  }

  .user-block {
    padding: 16px 20px;
    border-top: 1px solid #2e2e50;
    font-size: 12px;
  }
  .user-id {
    color: #8888aa;
    margin-bottom: 6px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .logout {
    color: #9090b0;
    text-decoration: none;
    font-size: 12px;
  }
  .logout:hover {
    color: #e0e0f0;
  }

  main {
    flex: 1;
    padding: 32px;
    overflow: auto;
  }
</style>
