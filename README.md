# slashsre-devops-controller

A Kubernetes controller for day-2 operations, configured entirely through custom resources
(`slashsre.controller.io/v1alpha1`).

| Kind | Scope | What it does |
|------|-------|--------------|
| `NodeLifecycleRule` | Cluster | Reads node lifecycle events from a queue (EC2 Spot interruption warnings, Auto Scaling lifecycle hooks) and cordons + drains the matching node. |
| `QueueTriggeredJob` | Namespaced | Polls a queue and creates a Job (from a CronJob or an inline template) when enough messages are waiting. |
| `AlertRemediation` | Namespaced | Receives Alertmanager webhooks and deletes the affected pod or rolling-restarts its workload. |
| `PostDeployTest` | Namespaced | Receives Flux notification events and starts a Job and/or a CI pipeline (GitHub Actions, Bitbucket Pipelines). |

It also serves a web UI with the live status of every resource, a REST API, Swagger docs and
Prometheus metrics on a single port (`9898`).

## Quick start

```bash
# Install CRDs, RBAC and the controller (namespace slashsre-system)
make deploy IMG=ghcr.io/slashsre/slashsre-devops-controller:latest

# Try the examples
kubectl apply -k config/samples
kubectl get slashsre -A
```

Local development against the current kubeconfig context:

```bash
make install   # CRDs only
make run       # go run ./cmd --level=debug
open http://localhost:9898
```

## Resources

### NodeLifecycleRule

```yaml
apiVersion: slashsre.controller.io/v1alpha1
kind: NodeLifecycleRule
metadata:
  name: spot-interruptions
spec:
  eventFormat: aws-spot-interruption        # aws-spot-interruption | aws-asg-lifecycle-hook
  source:
    provider: aws-sqs
    url: https://sqs.eu-central-1.amazonaws.com/123456789012/spot-interruptions
  pollInterval: 5s
  nodeSelector: {}                           # optional
  drain:
    timeout: 120s
    gracePeriodSeconds: -1
    ignoreDaemonSets: true
    deleteEmptyDirData: true
```

Nodes are matched by the last segment of `spec.providerID` (`aws:///eu-central-1a/i-0abc` → `i-0abc`).
The queue message is deleted *before* draining so the controller can drain the node it runs on
without reprocessing the event forever. Unparseable messages are left in the queue; configure a
redrive policy (dead-letter queue) on it. Auto Scaling test notifications are acknowledged and ignored.

See [docs/aws-lifecycle-hooks.md](docs/aws-lifecycle-hooks.md) for the AWS side.

### QueueTriggeredJob

```yaml
apiVersion: slashsre.controller.io/v1alpha1
kind: QueueTriggeredJob
metadata:
  name: image-resizer
  namespace: workers
spec:
  queue:
    url: https://sqs.eu-central-1.amazonaws.com/123456789012/image-resize
  minMessages: 2          # approximate depth that triggers a Job
  pollInterval: 10s
  maxActiveJobs: 1        # Jobs started by the CronJob itself also count
  job:
    cronJobRef:
      name: image-resizer # or: template: {spec: ...}
    ttlSecondsAfterFinished: 600
```

### AlertRemediation

```yaml
apiVersion: slashsre.controller.io/v1alpha1
kind: AlertRemediation
metadata:
  name: checkout-cpu-throttling
  namespace: shop          # only pods in this namespace are acted upon
spec:
  alertName: CPUThrottlingHigh
  matchLabels:
    container: checkout
  threshold: "50%"         # compared with the "value" annotation (valueAnnotation)
  action: DeletePod        # DeletePod | RolloutRestart
  cooldown: 10m
```

Alertmanager receiver:

```yaml
receivers:
- name: slashsre-devops-controller
  webhook_configs:
  - url: http://slashsre-devops-controller.slashsre-system.svc:9898/webhooks/alertmanager
    send_resolved: false
```

### PostDeployTest

```yaml
apiVersion: slashsre.controller.io/v1alpha1
kind: PostDeployTest
metadata:
  name: shop-e2e
  namespace: shop
spec:
  trigger:
    flux:
      kind: Kustomization
      name: shop
      messageContains: Health check passed
  job:                       # optional, in-cluster tests
    cronJobRef: {name: api-integration-tests}
  pipeline:                  # optional, external CI
    provider: github         # github | bitbucket
    repository: example-org/e2e-tests
    ref: main
    pipeline: e2e.yml        # GitHub workflow file / Bitbucket custom pipeline
    variables: {environment: staging}
    credentialsSecretRef:
      name: ci-credentials   # github: token | bitbucket: token or username+password
  cooldown: 5m
```

Flux needs a `generic` Provider pointing at `/webhooks/flux` (see `config/samples/postdeploytest.yaml`).

## HTTP endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Web UI |
| GET | `/api/v1/resources` | All custom resources with status |
| POST | `/api/v1/namespaces/{ns}/postdeploytests/{name}/run` | Run a PostDeployTest now |
| POST | `/api/v1/namespaces/{ns}/queuetriggeredjobs/{name}/run` | Create a Job now |
| POST | `/webhooks/alertmanager` | Alertmanager receiver (alias: `/alertmanager/custom`) |
| POST | `/webhooks/flux` | Flux notification receiver (alias: `/flux/notification`) |
| GET | `/healthz`, `/readyz` | Probes |
| GET | `/metrics` | Prometheus metrics (controller-runtime + HTTP) |
| GET | `/version`, `/api/info` | Build and runtime info |
| GET | `/swagger/` | Swagger UI |

Set `--api-token` / `--webhook-token` (or `DEVOPS_API_TOKEN` / `DEVOPS_WEBHOOK_TOKEN`) to require
`Authorization: Bearer <token>` on mutating API calls and webhooks.

## Configuration

Flags can also be set with `DEVOPS_*` environment variables (`--leader-elect` → `DEVOPS_LEADER_ELECT`)
or an optional `config.yaml`.

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `9898` | HTTP port |
| `--level` | `info` | Log level |
| `--leader-elect` | `false` | Leader election (required with >1 replica) |
| `--api-token` | | Bearer token for mutating API calls |
| `--webhook-token` | | Bearer token for webhooks |
| `--allowed-origins` | | CORS origin for `/api/v1` |
| `--logstash-host` / `--logstash-port` | / `9800` | Optional TCP log shipping |

AWS credentials come from the default chain: use IRSA or EKS Pod Identity on the
`slashsre-devops-controller` ServiceAccount with `sqs:ReceiveMessage`, `sqs:DeleteMessage` and
`sqs:GetQueueAttributes` on the queues you reference.

## Architecture

```
cmd/                      entrypoint (manager, providers, HTTP server)
api/v1alpha1/             CRD types
internal/controller/      NodeLifecycleRule and QueueTriggeredJob reconcilers
internal/remediation/     Alertmanager matching and actions
internal/posttest/        Flux matching and test runner
internal/providers/queue/     pluggable queues (aws-sqs, fake)
internal/providers/pipeline/  pluggable CI (github, bitbucket)
internal/k8sutil/         jobs, drain, rollout restart helpers
internal/server/          HTTP API, webhooks, embedded UI
config/                   kustomize: crd, rbac, manager, samples
ui/                       Svelte UI
```

Adding a provider means implementing `queue.Provider` or `pipeline.Provider`, registering it in
`cmd/main.go` and adding the new value to the enum in `api/v1alpha1`.

## Development

```bash
make generate manifests   # after changing api/
make test
make swagger              # after changing handler annotations
make ui                   # after changing ui/
make docker-build IMG=...
```

## License

TBD
