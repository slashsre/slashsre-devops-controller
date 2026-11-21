package controller

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
)

const (
	kindQueueTriggeredJob        = "QueueTriggeredJob"
	defaultQueuePollInterval     = 30 * time.Second
	defaultNodeEventPollInterval = 5 * time.Second
)

// QueueTriggeredJobReconciler polls queues and creates Jobs when work is pending.
type QueueTriggeredJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Queues   queue.Factory
	Recorder record.EventRecorder
	// Now is overridable in tests.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=slashsre.controller.io,resources=queuetriggeredjobs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=slashsre.controller.io,resources=queuetriggeredjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements reconcile.Reconciler.
func (r *QueueTriggeredJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var qtj v1alpha1.QueueTriggeredJob
	if err := r.Get(ctx, req.NamespacedName, &qtj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	original := qtj.DeepCopy()
	now := r.now()
	interval := durationOr(qtj.Spec.PollInterval, defaultQueuePollInterval)
	qtj.Status.ObservedGeneration = qtj.Generation

	if qtj.Spec.Suspend {
		setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionFalse, v1alpha1.ReasonSuspended, "polling suspended")
		return ctrl.Result{}, r.patchStatus(ctx, original, &qtj)
	}

	provider, err := r.Queues.For(ctx, qtj.Spec.Queue)
	if err != nil {
		setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &qtj)
	}
	depth, err := provider.ApproximateDepth(ctx)
	if err != nil {
		logger.Error(err, "reading queue depth")
		setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionFalse, v1alpha1.ReasonProviderError, err.Error())
		return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &qtj)
	}
	qtj.Status.LastPollTime = &metav1.Time{Time: now}
	qtj.Status.LastQueueDepth = int32(depth)

	tmpl, cronJob, err := k8sutil.ResolveJobTemplate(ctx, r.Client, qtj.Namespace, qtj.Spec.Job)
	if err != nil {
		setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionFalse, v1alpha1.ReasonJobError, err.Error())
		return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &qtj)
	}

	active, err := k8sutil.CountActiveOwnedJobs(ctx, r.Client, qtj.Namespace, kindQueueTriggeredJob, qtj.Name)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cronJob != nil {
		active += len(cronJob.Status.Active)
	}
	qtj.Status.ActiveJobs = int32(active)

	minMessages := max(qtj.Spec.MinMessages, 1)
	maxActive := max(qtj.Spec.MaxActiveJobs, 1)
	if int32(depth) >= minMessages && int32(active) < maxActive {
		job, err := k8sutil.CreateJob(ctx, r.Client, r.Scheme, tmpl, k8sutil.CreateJobOptions{
			Namespace:               qtj.Namespace,
			BaseName:                qtj.Name,
			Owner:                   &qtj,
			OwnerKind:               kindQueueTriggeredJob,
			CronJob:                 cronJob,
			TTLSecondsAfterFinished: qtj.Spec.Job.TTLSecondsAfterFinished,
		})
		if err != nil {
			r.Recorder.Event(&qtj, corev1.EventTypeWarning, "JobCreateFailed", err.Error())
			setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionFalse, v1alpha1.ReasonJobError, err.Error())
			return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &qtj)
		}
		logger.Info("job created", "job", job.Name, "queueDepth", depth)
		r.Recorder.Eventf(&qtj, corev1.EventTypeNormal, "JobCreated", "created job %s (queue depth %d)", job.Name, depth)
		qtj.Status.TriggeredJobs++
		qtj.Status.LastTriggerTime = &metav1.Time{Time: now}
		qtj.Status.LastJobName = job.Name
		qtj.Status.ActiveJobs++
	}

	setCondition(&qtj.Status.Conditions, qtj.Generation, metav1.ConditionTrue, v1alpha1.ReasonReconciled, "queue polled successfully")
	return ctrl.Result{RequeueAfter: interval}, r.patchStatus(ctx, original, &qtj)
}

// TriggerNowByKey creates a Job for the named QueueTriggeredJob immediately, ignoring
// queue depth (used by the manual API). It returns the Job name.
func (r *QueueTriggeredJobReconciler) TriggerNowByKey(ctx context.Context, key client.ObjectKey) (string, error) {
	var qtj v1alpha1.QueueTriggeredJob
	if err := r.Get(ctx, key, &qtj); err != nil {
		return "", err
	}
	job, err := r.TriggerNow(ctx, &qtj)
	if err != nil {
		return "", err
	}
	r.Recorder.Eventf(&qtj, corev1.EventTypeNormal, "JobCreated", "created job %s (manual run)", job.Name)
	return job.Name, nil
}

// TriggerNow creates a Job immediately, ignoring queue depth.
func (r *QueueTriggeredJobReconciler) TriggerNow(ctx context.Context, qtj *v1alpha1.QueueTriggeredJob) (*batchv1.Job, error) {
	tmpl, cronJob, err := k8sutil.ResolveJobTemplate(ctx, r.Client, qtj.Namespace, qtj.Spec.Job)
	if err != nil {
		return nil, err
	}
	return k8sutil.CreateJob(ctx, r.Client, r.Scheme, tmpl, k8sutil.CreateJobOptions{
		Namespace: qtj.Namespace, BaseName: qtj.Name, Owner: qtj, OwnerKind: kindQueueTriggeredJob,
		CronJob: cronJob, TTLSecondsAfterFinished: qtj.Spec.Job.TTLSecondsAfterFinished,
	})
}

func (r *QueueTriggeredJobReconciler) patchStatus(ctx context.Context, original, updated *v1alpha1.QueueTriggeredJob) error {
	if err := r.Status().Patch(ctx, updated, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *QueueTriggeredJobReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// SetupWithManager registers the controller.
func (r *QueueTriggeredJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Status updates must not retrigger polling: only spec changes do.
		For(&v1alpha1.QueueTriggeredJob{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&batchv1.Job{}).
		Named("queuetriggeredjob").
		Complete(r)
}

func setCondition(conds *[]metav1.Condition, generation int64, status metav1.ConditionStatus, reason, msg string) {
	meta.SetStatusCondition(conds, metav1.Condition{
		Type:               v1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: generation,
	})
}

func durationOr(d metav1.Duration, def time.Duration) time.Duration {
	if d.Duration <= 0 {
		return def
	}
	return d.Duration
}
