package controller

import (
	"context"
	"errors"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/nodeevents"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
)

// NodeDrainer drains a node; implemented by k8sutil.Drainer.
type NodeDrainer interface {
	Drain(ctx context.Context, node *corev1.Node, opts v1alpha1.DrainOptions) error
}

// NodeFinder resolves an instance ID to a node.
type NodeFinder func(ctx context.Context, c client.Reader, instanceID string, selector *metav1.LabelSelector) (*corev1.Node, error)

// NodeLifecycleRuleReconciler reads node lifecycle events from a queue and drains nodes.
type NodeLifecycleRuleReconciler struct {
	client.Client
	Queues   queue.Factory
	Drainer  NodeDrainer
	FindNode NodeFinder
	Recorder record.EventRecorder
	// SyncDrain makes drains run inline (tests). By default drains run in the
	// background so several nodes can be drained within the provider's notice window.
	SyncDrain bool

	inflight sync.Map // node name -> struct{}
}

// Visibility timeout while a message is being processed: enough for parsing and
// deletion; deletion happens before draining.
const nodeEventVisibility = 90 * time.Second

// +kubebuilder:rbac:groups=slashsre.controller.io,resources=nodelifecyclerules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=slashsre.controller.io,resources=nodelifecyclerules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch

// Reconcile implements reconcile.Reconciler.
func (r *NodeLifecycleRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var rule v1alpha1.NodeLifecycleRule
	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	original := rule.DeepCopy()
	interval := durationOr(rule.Spec.PollInterval, defaultNodeEventPollInterval)
	rule.Status.ObservedGeneration = rule.Generation

	if rule.Spec.Suspend {
		setCondition(&rule.Status.Conditions, rule.Generation, metav1.ConditionFalse, v1alpha1.ReasonSuspended, "event processing suspended")
		return ctrl.Result{}, r.patchStatus(ctx, original, &rule)
	}

	provider, err := r.Queues.For(ctx, rule.Spec.Source)
	if err != nil {
		setCondition(&rule.Status.Conditions, rule.Generation, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &rule)
	}
	msgs, err := provider.Receive(ctx, queue.ReceiveOptions{
		MaxMessages:       10,
		VisibilityTimeout: nodeEventVisibility,
		WaitTime:          min(interval, 5*time.Second),
	})
	if err != nil {
		logger.Error(err, "receiving node events")
		setCondition(&rule.Status.Conditions, rule.Generation, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &rule)
	}
	rule.Status.LastPollTime = &metav1.Time{Time: time.Now()}

	for _, msg := range msgs {
		ev, err := nodeevents.Parse(rule.Spec.EventFormat, []byte(msg.Body))
		if err != nil {
			// Leave the message in the queue: it becomes visible again and, if it keeps
			// failing, the queue's redrive policy should move it to a dead-letter queue.
			logger.Error(err, "unparseable node event left in queue", "messageID", msg.ID)
			r.Recorder.Eventf(&rule, corev1.EventTypeWarning, "InvalidEvent", "message %s: %v", msg.ID, err)
			continue
		}
		if ev.Test {
			if err := provider.Delete(ctx, msg); err != nil {
				logger.Error(err, "deleting test notification")
			} else {
				logger.Info("test notification acknowledged", "type", ev.Type)
			}
			continue
		}

		// Delete before draining: if this controller runs on the node being drained it
		// would be evicted and the message would loop forever.
		if err := provider.Delete(ctx, msg); err != nil {
			logger.Error(err, "deleting node event, will retry", "instanceID", ev.InstanceID)
			continue
		}
		rule.Status.LastEventTime = &metav1.Time{Time: time.Now()}

		node, err := r.FindNode(ctx, r.Client, ev.InstanceID, rule.Spec.NodeSelector)
		if err != nil {
			logger.Error(err, "looking up node", "instanceID", ev.InstanceID)
			continue
		}
		if node == nil {
			logger.Info("no matching node for event, ignoring", "instanceID", ev.InstanceID, "type", ev.Type)
			continue
		}
		r.startDrain(ctx, &rule, node, ev)
	}

	setCondition(&rule.Status.Conditions, rule.Generation, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "queue polled successfully")
	return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &rule)
}

func (r *NodeLifecycleRuleReconciler) startDrain(ctx context.Context, rule *v1alpha1.NodeLifecycleRule, node *corev1.Node, ev nodeevents.Event) {
	if _, busy := r.inflight.LoadOrStore(node.Name, struct{}{}); busy {
		return
	}
	logger := log.FromContext(ctx).WithValues("node", node.Name, "instanceID", ev.InstanceID, "type", ev.Type)
	r.Recorder.Eventf(rule, corev1.EventTypeNormal, "DrainStarted", "draining node %s (%s)", node.Name, ev.Type)
	logger.Info("draining node")

	ruleName := rule.Name
	opts := *rule.Spec.Drain.DeepCopy()
	run := func(ctx context.Context) {
		defer r.inflight.Delete(node.Name)
		err := r.Drainer.Drain(ctx, node, opts)
		if err != nil {
			logger.Error(err, "drain failed")
		} else {
			logger.Info("node drained")
		}
		r.recordDrain(ctx, ruleName, node.Name, err)
	}
	if r.SyncDrain {
		run(ctx)
		return
	}
	// The drain outlives this reconcile; give it its own deadline.
	timeout := 120 * time.Second
	if opts.Timeout != nil && opts.Timeout.Duration > 0 {
		timeout = opts.Timeout.Duration
	}
	go func() {
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout+30*time.Second)
		defer cancel()
		run(dctx)
	}()
}

func (r *NodeLifecycleRuleReconciler) recordDrain(ctx context.Context, ruleName, nodeName string, drainErr error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var rule v1alpha1.NodeLifecycleRule
		if err := r.Get(ctx, client.ObjectKey{Name: ruleName}, &rule); err != nil {
			return err
		}
		if drainErr != nil {
			rule.Status.FailedDrains++
			r.Recorder.Eventf(&rule, corev1.EventTypeWarning, "DrainFailed", "node %s: %v", nodeName, drainErr)
		} else {
			rule.Status.DrainedNodes++
			rule.Status.LastDrainedNode = nodeName
			r.Recorder.Eventf(&rule, corev1.EventTypeNormal, "Drained", "node %s drained", nodeName)
		}
		return r.Status().Update(ctx, &rule)
	})
	if err != nil && !apierrors.IsNotFound(err) && !errors.Is(err, context.Canceled) {
		log.FromContext(ctx).Error(err, "recording drain result")
	}
}

func (r *NodeLifecycleRuleReconciler) patchStatus(ctx context.Context, original, updated *v1alpha1.NodeLifecycleRule) error {
	if err := r.Status().Patch(ctx, updated, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// SetupWithManager registers the controller.
func (r *NodeLifecycleRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NodeLifecycleRule{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 4}).
		Named("nodelifecyclerule").
		Complete(r)
}
