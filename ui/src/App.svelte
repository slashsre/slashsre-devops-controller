<script>
  import { onMount } from 'svelte'
  import Router, { location } from 'svelte-spa-router'
  import { _, locale } from 'svelte-i18n'
  import './i18n/i18n'

  import Home from './views/Home.svelte'
  import ResourceView from './views/ResourceView.svelte'
  import API from './views/API.svelte'
  import { kinds } from './lib/kinds.js'
  import { startPolling } from './lib/api.js'

  import './assets/styles.css'

  const routes = {
    '/': Home,
    '/kinds/:kind': ResourceView,
    '/api': API
  }

  onMount(() => startPolling())

  // Reactive helper function to check if a link is active
  $: isActive = (path) => {
    return $location === path;
  }

  // Language switcher
  function switchLanguage(lang) {
    locale.set(lang)
  }
</script>

<div id="app">
  <header class="app-header">
    <div class="container">
      <div class="header-content">
        <div class="logo-section">
          <svg class="logo-icon" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 2L2 7L12 12L22 7L12 2Z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M2 17L12 22L22 17" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
            <path d="M2 12L12 17L22 12" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
          </svg>
          <h1 class="logo-text">slashsre devops controller</h1>
        </div>
        <div class="header-right">
          <nav class="main-nav">
            <a href="#/" class="nav-link" class:active={isActive('/')}>{$_('nav.home')}</a>
            {#each kinds as k}
              <a href={`#/kinds/${k.route}`} class="nav-link" class:active={isActive(`/kinds/${k.route}`)}>{$_(`nav.${k.id}`)}</a>
            {/each}
            <a href="#/api" class="nav-link" class:active={isActive('/api')}>{$_('nav.apiDocs')}</a>
          </nav>
          <div class="language-switcher">
            <button 
              class="lang-btn" 
              class:active={$locale === 'en'}
              on:click={() => switchLanguage('en')}
            >
              EN
            </button>
            <button 
              class="lang-btn" 
              class:active={$locale === 'es'}
              on:click={() => switchLanguage('es')}
            >
              ES
            </button>
          </div>
        </div>
      </div>
    </div>
  </header>

  <main class="app-main">
    <div class="container">
      <Router {routes} />
    </div>
  </main>

  <footer class="app-footer">
    <div class="container">
      <p>{$_('footer.copyright')}</p>
    </div>
  </footer>
</div>

<style>
  :global(*) {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
  }

  :global(:root) {
    --primary-color: #2563eb;
    --primary-dark: #1e40af;
    --secondary-color: #10b981;
    --bg-color: #f8fafc;
    --surface-color: #ffffff;
    --text-primary: #1e293b;
    --text-secondary: #64748b;
    --border-color: #e2e8f0;
    --shadow-sm: 0 1px 2px 0 rgba(0, 0, 0, 0.05);
    --shadow-md: 0 4px 6px -1px rgba(0, 0, 0, 0.1);
    --shadow-lg: 0 10px 15px -3px rgba(0, 0, 0, 0.1);
  }

  :global(body) {
    font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
    background-color: var(--bg-color);
    color: var(--text-primary);
    line-height: 1.6;
  }

  #app {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
  }

  :global(.container) {
    max-width: 1200px;
    margin: 0 auto;
    padding: 0 1.5rem;
  }

  /* Header Styles */
  .app-header {
    background: var(--surface-color);
    border-bottom: 1px solid var(--border-color);
    box-shadow: var(--shadow-sm);
    position: sticky;
    top: 0;
    z-index: 100;
  }

  .header-content {
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding: 1rem 0;
  }

  .logo-section {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    align-self: flex-start;
  }

  .logo-icon {
    width: 32px;
    height: 32px;
    color: var(--primary-color);
  }

  .logo-text {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--text-primary);
  }

  .header-right {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    gap: 1rem;
  }

  .main-nav {
    display: flex;
    gap: 0.25rem;
    flex-wrap: nowrap;
    flex: 1;
  }

  .language-switcher {
    display: flex;
    gap: 0.25rem;
    background: var(--bg-color);
    padding: 0.25rem;
    border-radius: 0.375rem;
  }

  .lang-btn {
    padding: 0.375rem 0.75rem;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-weight: 600;
    font-size: 0.75rem;
    border-radius: 0.25rem;
    cursor: pointer;
    transition: all 0.2s;
  }

  .lang-btn:hover {
    color: var(--text-primary);
  }

  .lang-btn.active {
    background: var(--surface-color);
    color: var(--primary-color);
    box-shadow: var(--shadow-sm);
  }

  .nav-link {
    padding: 0.5rem 0.75rem;
    text-decoration: none;
    color: var(--text-secondary);
    font-weight: 500;
    border-radius: 0.375rem;
    transition: all 0.2s;
    font-size: 0.8125rem;
    white-space: nowrap;
  }

  .nav-link:hover {
    background-color: var(--bg-color);
    color: var(--text-primary);
  }

  .nav-link.active {
    background-color: var(--primary-color);
    color: white;
  }

  /* Main Content */
  .app-main {
    flex: 1;
    padding: 2rem 0;
  }

  /* Footer */
  .app-footer {
    background: var(--surface-color);
    border-top: 1px solid var(--border-color);
    padding: 1.5rem 0;
    margin-top: 3rem;
  }

  .app-footer p {
    text-align: center;
    color: var(--text-secondary);
    font-size: 0.875rem;
  }

  /* Responsive */
  @media (max-width: 1024px) {
    .main-nav {
      gap: 0.125rem;
    }

    .nav-link {
      font-size: 0.75rem;
      padding: 0.5rem 0.625rem;
    }
  }

  @media (max-width: 768px) {
    .header-content {
      gap: 0.75rem;
    }

    .header-right {
      flex-direction: column;
      gap: 0.75rem;
      align-items: stretch;
    }

    .main-nav {
      flex-wrap: wrap;
      justify-content: center;
      gap: 0.25rem;
    }

    .language-switcher {
      align-self: center;
    }

    .nav-link {
      font-size: 0.75rem;
      padding: 0.4rem 0.625rem;
    }
  }

  @media (max-width: 480px) {
    .logo-text {
      font-size: 1.25rem;
    }

    .nav-link {
      font-size: 0.6875rem;
      padding: 0.375rem 0.5rem;
    }
  }
</style>
