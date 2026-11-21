# slashsre-devops-controller

Kubernetes controller (controller-runtime) for day-2 operations configured through CRDs in the
`slashsre.controller.io/v1alpha1` API group. Go module `github.com/slashsre/slashsre-devops-controller`, Go 1.24.

## Layout

```
cmd/main.go                    flags/viper (DEVOPS_ prefix), manager, provider registries, HTTP server
api/v1alpha1/                  CRD types (+kubebuilder markers); zz_generated.deepcopy.go is generated
internal/controller/           NodeLifecycleRule + QueueTriggeredJob reconcilers (poll via RequeueAfter)
internal/remediation/          AlertRemediation matching/threshold logic + Executor (webhook driven)
internal/posttest/             PostDeployTest Flux matching + Runner (Job and/or pipeline)
internal/providers/queue/      queue.Provider interface, Registry; awssqs (SDK v2), fake (tests)
internal/providers/pipeline/   pipeline.Provider interface, Registry; github, bitbucket
internal/nodeevents/           parsers for aws-spot-interruption and aws-asg-lifecycle-hook
internal/k8sutil/              Job creation, node lookup/drain, rollout restart
internal/server/               gorilla/mux server as manager.Runnable (all replicas), webhooks, /api/v1, embedded UI
internal/logging/              zap logger with optional Logstash TCP output
config/                        kustomize: crd/bases (generated), rbac/role.yaml (generated), manager, default, samples
ui/                            Svelte 4 UI, built into internal/server/ui-dist
```

## Commands

```bash
make generate manifests   # after editing api/ or +kubebuilder:rbac markers
make test                 # unit tests (fake client, no envtest)
make swagger              # after editing swag annotations (needs GOTOOLCHAIN=go1.25.x on recent macOS)
make ui                   # build Svelte and copy to internal/server/ui-dist
make build | run | docker-build | install | deploy
```

## Conventions

- Reconcilers use `GenerationChangedPredicate` so their own status patches do not retrigger polling;
  periodic work is scheduled with `RequeueAfter`.
- Webhook-driven kinds (AlertRemediation, PostDeployTest) have no reconciler; status is updated
  by the Executor/Runner with `retry.RetryOnConflict`.
- Secrets, Pods and apps/v1 workloads are read uncached (see `cache.DisableFor` in main.go) so RBAC
  only needs `get` on secrets.
- NodeLifecycleRule deletes the queue message before draining (self-drain safety).
- AlertRemediation only acts on pods in its own namespace.
- New providers: implement the interface, register in `cmd/main.go`, extend the enum in `api/v1alpha1`.
- Test data must use placeholder values only (account `123456789012`, `example-org`, generic service names).
  Never commit real account IDs, cluster names, internal service names or company references.
