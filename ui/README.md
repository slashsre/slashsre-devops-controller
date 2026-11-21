# slashsre-devops-controller UI

Svelte 4 + Vite single page application embedded in the controller binary.
It reads `GET /api/v1/resources` and shows the live status of every custom resource.

```bash
npm ci
npm run dev      # http://localhost:3000, proxies the API to localhost:9898 if configured
npm run build    # outputs dist/
```

From the repository root, `./build-ui.sh` builds the UI and copies it to
`internal/server/ui-dist`, which is embedded with `go:embed`.

```
src/
├── App.svelte              layout, navigation, routes
├── lib/api.js              API client and polling store
├── lib/kinds.js            per-kind columns and examples
├── views/Home.svelte       overview
├── views/ResourceView.svelte  generic view for each custom resource
├── views/API.svelte        Swagger links
└── i18n/locales/{en,es}.json
```
