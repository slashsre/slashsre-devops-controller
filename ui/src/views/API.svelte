<script>
  import { _ } from 'svelte-i18n';
</script>

<div class="view-container">
  <div class="view-header">
    <div class="icon-badge api-color">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/>
        <polyline points="14 2 14 8 20 8"/>
        <line x1="16" y1="13" x2="8" y2="13"/>
        <line x1="16" y1="17" x2="8" y2="17"/>
        <line x1="10" y1="9" x2="8" y2="9"/>
      </svg>
    </div>
    <div>
      <h1 class="view-title">{$_('apiDocs.title')}</h1>
      <p class="view-subtitle">{$_('apiDocs.subtitle')}</p>
    </div>
  </div>

  <div class="content-card">
    <h2 class="card-title">{$_('apiDocs.interactive.title')}</h2>
    <div class="swagger-section">
      <div class="swagger-info">
        <div class="info-icon">📖</div>
        <div class="info-content">
          <h3>Swagger / OpenAPI</h3>
          <p>
            {$_('apiDocs.interactive.description')}
          </p>
        </div>
      </div>
      <div class="swagger-buttons">
        <a href="/swagger/index.html" class="swagger-button primary" target="_blank">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <polyline points="12 16 16 12 12 8"/>
            <line x1="8" y1="12" x2="16" y2="12"/>
          </svg>
          {$_('apiDocs.interactive.openSwagger')}
        </a>
        <a href="/swagger/doc.json" class="swagger-button secondary" target="_blank">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="7 10 12 15 17 10"/>
            <line x1="12" y1="15" x2="12" y2="3"/>
          </svg>
          {$_('apiDocs.interactive.downloadJson')}
        </a>
      </div>
    </div>
  </div>
</div>

<style>
  .api-color {
    background-color: #6366f120;
    color: #6366f1;
  }

  .swagger-section {
    display: flex;
    flex-direction: column;
    gap: 2rem;
  }

  .swagger-info {
    display: flex;
    gap: 1.5rem;
    align-items: flex-start;
  }

  .info-icon {
    font-size: 3rem;
    flex-shrink: 0;
  }

  .info-content h3 {
    font-size: 1.5rem;
    font-weight: 600;
    color: var(--text-primary);
    margin-bottom: 0.75rem;
  }

  .info-content p {
    color: var(--text-secondary);
    line-height: 1.7;
    font-size: 1rem;
  }

  .swagger-buttons {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 1rem;
  }

  .swagger-button {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.75rem;
    padding: 1rem 1.5rem;
    border-radius: 0.5rem;
    font-weight: 600;
    text-decoration: none;
    transition: all 0.2s;
    border: 2px solid;
  }

  .swagger-button svg {
    width: 20px;
    height: 20px;
  }

  .swagger-button.primary {
    background: linear-gradient(135deg, #6366f1, #4f46e5);
    color: white;
    border-color: #6366f1;
  }

  .swagger-button.primary:hover {
    transform: translateY(-2px);
    box-shadow: 0 8px 16px rgba(99, 102, 241, 0.3);
  }

  .swagger-button.secondary {
    background: white;
    color: #6366f1;
    border-color: #6366f1;
  }

  .swagger-button.secondary:hover {
    background: #f0f0ff;
    transform: translateY(-2px);
    box-shadow: 0 8px 16px rgba(99, 102, 241, 0.15);
  }

  @media (max-width: 768px) {
    .swagger-info {
      flex-direction: column;
    }

    .swagger-buttons {
      grid-template-columns: 1fr;
    }
  }
</style>
