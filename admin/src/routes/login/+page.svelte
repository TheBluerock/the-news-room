<script lang="ts">
  import { enhance } from '$app/forms'
  import type { ActionData } from './$types'

  const { form }: { form: ActionData } = $props()
</script>

<div class="wrap">
  <div class="card">
    <h1>Newsroom Admin</h1>
    <p class="sub">Editorial access only</p>

    <form method="POST" use:enhance>
      {#if form?.error}
        <div class="error">{form.error}</div>
      {/if}
      <label>
        Email
        <input type="email" name="email" required autocomplete="username" />
      </label>
      <label>
        Password
        <input type="password" name="password" required autocomplete="current-password" />
      </label>
      <button type="submit">Sign in</button>
    </form>
  </div>
</div>

<style>
  .wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #f4f4f8;
  }
  .card {
    background: #fff;
    border-radius: 8px;
    padding: 40px;
    width: 360px;
    box-shadow: 0 2px 16px rgba(0, 0, 0, 0.08);
  }
  h1 {
    font-size: 20px;
    font-weight: 700;
    color: #1a1a2e;
    margin-bottom: 4px;
  }
  .sub {
    font-size: 13px;
    color: #888;
    margin-bottom: 28px;
  }
  form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: #555;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  input {
    padding: 9px 12px;
    border: 1px solid #ddd;
    border-radius: 5px;
    font-size: 14px;
    outline: none;
  }
  input:focus {
    border-color: #6366f1;
  }
  button {
    margin-top: 4px;
    padding: 10px;
    background: #6366f1;
    color: #fff;
    border: none;
    border-radius: 5px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
  }
  button:hover {
    background: #4f52d0;
  }
  .error {
    padding: 10px 12px;
    background: #fef2f2;
    border: 1px solid #fecaca;
    border-radius: 5px;
    color: #b91c1c;
    font-size: 13px;
  }
</style>
