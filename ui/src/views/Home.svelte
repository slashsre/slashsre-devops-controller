<script>
  import { _ } from 'svelte-i18n'
  import { resources } from '../lib/api.js'
  import { kinds } from '../lib/kinds.js'

  $: counts = Object.fromEntries(kinds.map((k) => [k.id, $resources.data ? ($resources.data[k.id] || []).length : null]))
</script>

<div class="view-container">
  <div class="hero">
    <h1 class="view-title">{$_('home.title')}</h1>
    <p class="view-subtitle">{$_('home.subtitle')}</p>
  </div>

  <div class="grid">
    {#each kinds as k}
      <a class="kind-card" href={`#/kinds/${k.route}`}>
        <div class="icon-badge" style="background-color: {k.color}20; color: {k.color}">{@html k.icon}</div>
        <div class="kind-body">
          <div class="kind-top">
            <h2>{k.kind}</h2>
            <span class="count">{counts[k.id] ?? '…'}</span>
          </div>
          <p>{$_(`kinds.${k.id}.subtitle`)}</p>
        </div>
      </a>
    {/each}
  </div>

  <div class="content-card">
    <h2 class="card-title">{$_('home.gettingStarted.title')}</h2>
    <pre class="yaml"><code>{`# CRDs + controller
kubectl apply --server-side -k config/default

# Examples
kubectl apply -k config/samples
kubectl get slashsre -A`}</code></pre>
    <p class="card-text">{$_('home.gettingStarted.description')}</p>
  </div>
</div>

<style>
  .hero {
    margin-bottom: 2rem;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 1rem;
    margin-bottom: 1.5rem;
  }
  .kind-card {
    display: flex;
    gap: 1rem;
    padding: 1.25rem;
    background: var(--surface-color);
    border: 1px solid var(--border-color);
    border-radius: 0.75rem;
    text-decoration: none;
    color: inherit;
    transition: box-shadow 0.2s, transform 0.2s;
  }
  .kind-card:hover {
    box-shadow: var(--shadow-md);
    transform: translateY(-2px);
  }
  .kind-body {
    flex: 1;
    min-width: 0;
  }
  .kind-top {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 0.5rem;
  }
  .kind-top h2 {
    font-size: 1.1rem;
  }
  .count {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--primary-color);
  }
  .kind-body p {
    color: var(--text-secondary);
    font-size: 0.9rem;
  }
  .yaml {
    background: #0f172a;
    color: #e2e8f0;
    padding: 1rem;
    border-radius: 0.5rem;
    overflow-x: auto;
    font-size: 0.85rem;
    margin-bottom: 1rem;
  }
</style>
