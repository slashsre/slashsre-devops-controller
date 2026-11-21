<script>
  import { _ } from 'svelte-i18n'
  import { resources, runResource } from '../lib/api.js'
  import { kindByRoute } from '../lib/kinds.js'

  export let params = {}

  $: def = kindByRoute[params.kind]
  $: items = def && $resources.data ? $resources.data[def.id] || [] : []

  let running = {}
  let message = null

  async function run(obj) {
    const key = `${obj.metadata.namespace}/${obj.metadata.name}`
    running = { ...running, [key]: true }
    message = null
    try {
      const res = await runResource(def.plural, obj.metadata.namespace, obj.metadata.name, $_('common.tokenPrompt'))
      message = { ok: true, text: `${key}: ${res.jobName || res.pipelineURL || $_('common.started')}` }
    } catch (err) {
      message = { ok: false, text: `${key}: ${err.message}` }
    } finally {
      running = { ...running, [key]: false }
    }
  }
</script>

{#if !def}
  <div class="content-card"><p class="card-text">{$_('common.notFound')}</p></div>
{:else}
  <div class="view-container">
    <div class="view-header">
      <div class="icon-badge" style="background-color: {def.color}20; color: {def.color}">{@html def.icon}</div>
      <div>
        <h1 class="view-title">{def.kind}</h1>
        <p class="view-subtitle">{$_(`kinds.${def.id}.subtitle`)}</p>
      </div>
    </div>

    <div class="content-card">
      <h2 class="card-title">{$_('common.howItWorks')}</h2>
      <p class="card-text">{$_(`kinds.${def.id}.description`)}</p>
      <ul class="feature-list">
        {#each [1, 2, 3, 4] as n}
          <li class="feature-item">
            <div class="feature-item-icon">✓</div>
            <div>{$_(`kinds.${def.id}.features.f${n}`)}</div>
          </li>
        {/each}
      </ul>
    </div>

    <div class="content-card">
      <div class="table-header">
        <h2 class="card-title">{$_('common.resources')} ({items.length})</h2>
        {#if $resources.error}<span class="badge error">{$_('common.loadError')}: {$resources.error}</span>{/if}
      </div>
      {#if message}
        <p class="run-message" class:error={!message.ok}>{message.text}</p>
      {/if}
      {#if $resources.loading}
        <p class="card-text">{$_('common.loading')}</p>
      {:else if items.length === 0}
        <p class="card-text">{$_('common.empty')}</p>
      {:else}
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                {#if def.namespaced}<th>{$_('columns.namespace')}</th>{/if}
                <th>{$_('columns.name')}</th>
                {#each def.columns as col}<th>{$_(`columns.${col.id}`)}</th>{/each}
                {#if def.runnable}<th></th>{/if}
              </tr>
            </thead>
            <tbody>
              {#each items as obj (obj.metadata.uid)}
                <tr class:suspended={obj.spec.suspend}>
                  {#if def.namespaced}<td>{obj.metadata.namespace}</td>{/if}
                  <td class="name">{obj.metadata.name}{#if obj.spec.suspend} <span class="badge">{$_('common.suspended')}</span>{/if}</td>
                  {#each def.columns as col}
                    {@const v = col.value(obj)}
                    <td>
                      {#if col.status}
                        <span class="badge" class:ok={v === 'True'} class:error={v === 'False'}>{v}</span>
                      {:else}{v}{/if}
                    </td>
                  {/each}
                  {#if def.runnable}
                    <td>
                      <button class="run-btn" disabled={running[`${obj.metadata.namespace}/${obj.metadata.name}`]} on:click={() => run(obj)}>
                        {$_('common.runNow')}
                      </button>
                    </td>
                  {/if}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <div class="content-card">
      <h2 class="card-title">{$_('common.example')}</h2>
      <pre class="yaml"><code>{def.sample}</code></pre>
    </div>
  </div>
{/if}

<style>
  .table-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
    flex-wrap: wrap;
  }
  .table-wrap {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.9rem;
  }
  th,
  td {
    text-align: left;
    padding: 0.6rem 0.75rem;
    border-bottom: 1px solid var(--border-color);
    white-space: nowrap;
  }
  th {
    color: var(--text-secondary);
    font-weight: 600;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  td.name {
    font-weight: 600;
  }
  tr.suspended td {
    opacity: 0.6;
  }
  .badge {
    display: inline-block;
    padding: 0.1rem 0.5rem;
    border-radius: 999px;
    font-size: 0.75rem;
    background: var(--border-color);
    color: var(--text-secondary);
  }
  .badge.ok {
    background: #10b98120;
    color: #047857;
  }
  .badge.error {
    background: #ef444420;
    color: #b91c1c;
  }
  .run-btn {
    border: 1px solid var(--primary-color);
    background: transparent;
    color: var(--primary-color);
    border-radius: 0.5rem;
    padding: 0.3rem 0.8rem;
    cursor: pointer;
    font-weight: 600;
  }
  .run-btn:hover:not(:disabled) {
    background: var(--primary-color);
    color: white;
  }
  .run-btn:disabled {
    opacity: 0.5;
    cursor: wait;
  }
  .run-message {
    margin: 0.75rem 0;
    padding: 0.5rem 0.75rem;
    border-radius: 0.5rem;
    background: #10b98115;
    color: #047857;
    font-size: 0.9rem;
  }
  .run-message.error {
    background: #ef444415;
    color: #b91c1c;
  }
  .yaml {
    background: #0f172a;
    color: #e2e8f0;
    padding: 1rem;
    border-radius: 0.5rem;
    overflow-x: auto;
    font-size: 0.85rem;
    line-height: 1.5;
  }
</style>
