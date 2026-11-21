package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
	fakequeue "github.com/slashsre/slashsre-devops-controller/internal/providers/queue/fake"
)

const queueURL = "https://sqs.eu-central-1.amazonaws.com/123456789012/work"

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newClient(t *testing.T, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme(t)).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.QueueTriggeredJob{}, &v1alpha1.NodeLifecycleRule{}).
		Build()
}

func cronJob() *batchv1.CronJob {
	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "apps"},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 0 1 1 *",
			JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "w", Image: "busybox"}}},
			}}},
		},
	}
}

func qtjObject() *v1alpha1.QueueTriggeredJob {
	return &v1alpha1.QueueTriggeredJob{
		ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "apps", UID: "uid-1"},
		Spec: v1alpha1.QueueTriggeredJobSpec{
			Queue:         v1alpha1.QueueSource{URL: queueURL},
			MinMessages:   2,
			MaxActiveJobs: 1,
			PollInterval:  metav1.Duration{Duration: 10 * time.Second},
			Job:           v1alpha1.JobSource{CronJobRef: &v1alpha1.LocalObjectReference{Name: "worker"}},
		},
	}
}

func reconcileQTJ(t *testing.T, r *QueueTriggeredJobReconciler) ctrl.Result {
	t.Helper()
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "apps", Name: "worker"}})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func listJobs(t *testing.T, c client.Client) []batchv1.Job {
	t.Helper()
	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs, client.InNamespace("apps")); err != nil {
		t.Fatal(err)
	}
	return jobs.Items
}

func TestQueueTriggeredJobBelowThreshold(t *testing.T) {
	c := newClient(t, qtjObject(), cronJob())
	queues := fakequeue.Factory{}
	q, _ := queues.For(context.Background(), v1alpha1.QueueSource{URL: queueURL})
	q.(*fakequeue.Queue).Push("a")

	r := &QueueTriggeredJobReconciler{Client: c, Scheme: c.Scheme(), Queues: queues, Recorder: record.NewFakeRecorder(10)}
	res := reconcileQTJ(t, r)
	if res.RequeueAfter != 10*time.Second {
		t.Errorf("RequeueAfter = %v", res.RequeueAfter)
	}
	if n := len(listJobs(t, c)); n != 0 {
		t.Fatalf("expected no jobs, got %d", n)
	}
	var got v1alpha1.QueueTriggeredJob
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "apps", Name: "worker"}, &got)
	if got.Status.LastQueueDepth != 1 {
		t.Errorf("LastQueueDepth = %d", got.Status.LastQueueDepth)
	}
}

func TestQueueTriggeredJobCreatesJobAndRespectsMaxActive(t *testing.T) {
	c := newClient(t, qtjObject(), cronJob())
	queues := fakequeue.Factory{}
	q, _ := queues.For(context.Background(), v1alpha1.QueueSource{URL: queueURL})
	q.(*fakequeue.Queue).Push("a", "b", "c")

	r := &QueueTriggeredJobReconciler{Client: c, Scheme: c.Scheme(), Queues: queues, Recorder: record.NewFakeRecorder(10)}
	reconcileQTJ(t, r)
	jobs := listJobs(t, c)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.Labels[v1alpha1.LabelOwnerName] != "worker" || job.Labels[v1alpha1.LabelOwnerKind] != kindQueueTriggeredJob {
		t.Errorf("labels = %v", job.Labels)
	}
	if job.Spec.TTLSecondsAfterFinished != nil {
		t.Errorf("unexpected TTL without spec value")
	}
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].Kind != "QueueTriggeredJob" {
		t.Errorf("ownerReferences = %v", job.OwnerReferences)
	}

	// The first job is still running: no new job.
	reconcileQTJ(t, r)
	if n := len(listJobs(t, c)); n != 1 {
		t.Fatalf("maxActiveJobs not respected: %d jobs", n)
	}

	// Mark it finished: a new job is created.
	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}
	if err := c.Status().Update(context.Background(), &job); err != nil {
		t.Fatal(err)
	}
	reconcileQTJ(t, r)
	if n := len(listJobs(t, c)); n != 2 {
		t.Fatalf("expected 2 jobs after completion, got %d", n)
	}
	var got v1alpha1.QueueTriggeredJob
	_ = c.Get(context.Background(), types.NamespacedName{Namespace: "apps", Name: "worker"}, &got)
	if got.Status.TriggeredJobs != 2 {
		t.Errorf("TriggeredJobs = %d", got.Status.TriggeredJobs)
	}
}

func TestQueueTriggeredJobSuspended(t *testing.T) {
	obj := qtjObject()
	obj.Spec.Suspend = true
	c := newClient(t, obj, cronJob())
	queues := fakequeue.Factory{}
	q, _ := queues.For(context.Background(), v1alpha1.QueueSource{URL: queueURL})
	q.(*fakequeue.Queue).Push("a", "b")

	r := &QueueTriggeredJobReconciler{Client: c, Scheme: c.Scheme(), Queues: queues, Recorder: record.NewFakeRecorder(10)}
	if res := reconcileQTJ(t, r); res.RequeueAfter != 0 {
		t.Errorf("suspended resource should not requeue")
	}
	if n := len(listJobs(t, c)); n != 0 {
		t.Fatalf("suspended resource created %d jobs", n)
	}
}

type recordingDrainer struct{ drained []string }

func (d *recordingDrainer) Drain(_ context.Context, node *corev1.Node, _ v1alpha1.DrainOptions) error {
	d.drained = append(d.drained, node.Name)
	return nil
}

func TestNodeLifecycleRuleDrainsMatchingNode(t *testing.T) {
	rule := &v1alpha1.NodeLifecycleRule{
		ObjectMeta: metav1.ObjectMeta{Name: "spot"},
		Spec: v1alpha1.NodeLifecycleRuleSpec{
			Source:      v1alpha1.QueueSource{URL: queueURL},
			EventFormat: v1alpha1.NodeEventFormatAWSSpotInterruption,
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}, Spec: corev1.NodeSpec{ProviderID: "aws:///eu-central-1a/i-0abc"}}
	other := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}, Spec: corev1.NodeSpec{ProviderID: "aws:///eu-central-1a/i-0zzz"}}
	c := newClient(t, rule, node, other)

	queues := fakequeue.Factory{}
	q, _ := queues.For(context.Background(), v1alpha1.QueueSource{URL: queueURL})
	fq := q.(*fakequeue.Queue)
	fq.Push(
		`{"detail-type":"EC2 Spot Instance Interruption Warning","detail":{"instance-id":"i-0abc","instance-action":"terminate"}}`,
		`{"detail-type":"EC2 Spot Instance Interruption Warning","detail":{"instance-id":"i-unknown"}}`,
		`not json`,
	)

	drainer := &recordingDrainer{}
	r := &NodeLifecycleRuleReconciler{
		Client: c, Queues: queues, Drainer: drainer, FindNode: k8sutil.FindNodeByInstanceID,
		Recorder: record.NewFakeRecorder(20), SyncDrain: true,
	}
	res, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: "spot"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != defaultNodeEventPollInterval {
		t.Errorf("RequeueAfter = %v", res.RequeueAfter)
	}
	if len(drainer.drained) != 1 || drainer.drained[0] != "node-a" {
		t.Fatalf("drained = %v", drainer.drained)
	}
	// Both valid events are deleted; the unparseable one stays for the DLQ.
	if len(fq.Deleted) != 2 {
		t.Errorf("deleted = %v", fq.Deleted)
	}
	if depth, _ := fq.ApproximateDepth(context.Background()); depth != 1 {
		t.Errorf("remaining depth = %d", depth)
	}
	var got v1alpha1.NodeLifecycleRule
	_ = c.Get(context.Background(), types.NamespacedName{Name: "spot"}, &got)
	if got.Status.DrainedNodes != 1 || got.Status.LastDrainedNode != "node-a" {
		t.Errorf("status = %+v", got.Status)
	}
}
