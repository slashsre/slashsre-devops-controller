// Static description of every custom resource shown in the UI.
// Columns are rendered by ResourceView; labels come from i18n (columns.<id>).
import { readyCondition, since } from './api.js'

const ready = (o) => readyCondition(o).status

export const kinds = [
  {
    id: 'nodeLifecycleRules',
    route: 'node-lifecycle',
    kind: 'NodeLifecycleRule',
    plural: 'nodelifecyclerules',
    namespaced: false,
    color: '#8b5cf6',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><line x1="6" y1="6" x2="6.01" y2="6"/><line x1="6" y1="18" x2="6.01" y2="18"/></svg>',
    columns: [
      { id: 'format', value: (o) => o.spec.eventFormat },
      { id: 'drained', value: (o) => o.status?.drainedNodes ?? 0 },
      { id: 'failed', value: (o) => o.status?.failedDrains ?? 0 },
      { id: 'lastNode', value: (o) => o.status?.lastDrainedNode || '—' },
      { id: 'lastPoll', value: (o) => since(o.status?.lastPollTime) },
      { id: 'ready', value: ready, status: true },
    ],
    sample: `apiVersion: slashsre.controller.io/v1alpha1
kind: NodeLifecycleRule
metadata:
  name: spot-interruptions
spec:
  eventFormat: aws-spot-interruption   # or aws-asg-lifecycle-hook
  source:
    provider: aws-sqs
    url: https://sqs.eu-central-1.amazonaws.com/123456789012/spot-interruptions
  pollInterval: 5s
  nodeSelector:
    matchLabels:
      node.kubernetes.io/lifecycle: spot
  drain:
    timeout: 90s
    ignoreDaemonSets: true`,
  },
  {
    id: 'queueTriggeredJobs',
    route: 'queue-jobs',
    kind: 'QueueTriggeredJob',
    plural: 'queuetriggeredjobs',
    namespaced: true,
    runnable: true,
    color: '#10b981',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 11l3 3 5-5"/></svg>',
    columns: [
      { id: 'depth', value: (o) => o.status?.lastQueueDepth ?? 0 },
      { id: 'minMessages', value: (o) => o.spec.minMessages ?? 1 },
      { id: 'active', value: (o) => `${o.status?.activeJobs ?? 0}/${o.spec.maxActiveJobs ?? 1}` },
      { id: 'triggered', value: (o) => o.status?.triggeredJobs ?? 0 },
      { id: 'lastJob', value: (o) => o.status?.lastJobName || '—' },
      { id: 'ready', value: ready, status: true },
    ],
    sample: `apiVersion: slashsre.controller.io/v1alpha1
kind: QueueTriggeredJob
metadata:
  name: image-resizer
  namespace: workers
spec:
  queue:
    url: https://sqs.eu-central-1.amazonaws.com/123456789012/image-resize
  minMessages: 2
  pollInterval: 10s
  maxActiveJobs: 1
  job:
    cronJobRef:
      name: image-resizer      # or an inline "template"
    ttlSecondsAfterFinished: 600`,
  },
  {
    id: 'alertRemediations',
    route: 'alert-remediation',
    kind: 'AlertRemediation',
    plural: 'alertremediations',
    namespaced: true,
    color: '#f59e0b',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21.5 2v6h-6M2.5 22v-6h6M2 11.5a10 10 0 0 1 18.8-4.3M22 12.5a10 10 0 0 1-18.8 4.2"/></svg>',
    columns: [
      { id: 'alert', value: (o) => o.spec.alertName },
      { id: 'action', value: (o) => o.spec.action || 'DeletePod' },
      { id: 'threshold', value: (o) => o.spec.threshold || '—' },
      { id: 'remediations', value: (o) => o.status?.remediations ?? 0 },
      { id: 'lastTarget', value: (o) => o.status?.lastTarget || '—' },
      { id: 'lastRun', value: (o) => since(o.status?.lastRemediationTime) },
    ],
    sample: `apiVersion: slashsre.controller.io/v1alpha1
kind: AlertRemediation
metadata:
  name: checkout-cpu-throttling
  namespace: shop
spec:
  alertName: CPUThrottlingHigh
  matchLabels:
    container: checkout
  threshold: "50%"
  action: DeletePod            # or RolloutRestart
  cooldown: 10m`,
  },
  {
    id: 'postDeployTests',
    route: 'post-deploy-tests',
    kind: 'PostDeployTest',
    plural: 'postdeploytests',
    namespaced: true,
    runnable: true,
    color: '#ef4444',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg>',
    columns: [
      { id: 'trigger', value: (o) => (o.spec.trigger?.flux ? `flux ${o.spec.trigger.flux.kind || '*'}/${o.spec.trigger.flux.name || '*'}` : 'manual') },
      { id: 'target', value: (o) => [o.spec.job ? 'job' : null, o.spec.pipeline?.provider].filter(Boolean).join(' + ') },
      { id: 'runs', value: (o) => o.status?.runs ?? 0 },
      { id: 'lastRun', value: (o) => since(o.status?.lastRunTime) },
      { id: 'lastError', value: (o) => o.status?.lastError || '—' },
    ],
    sample: `apiVersion: slashsre.controller.io/v1alpha1
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
  pipeline:
    provider: github           # or bitbucket
    repository: example-org/e2e-tests
    pipeline: e2e.yml
    variables:
      environment: staging
    credentialsSecretRef:
      name: ci-credentials
  cooldown: 5m`,
  },
]

export const kindByRoute = Object.fromEntries(kinds.map((k) => [k.route, k]))
