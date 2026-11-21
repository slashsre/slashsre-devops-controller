// Package k8sutil contains Kubernetes helpers shared by controllers and HTTP handlers.
package k8sutil

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

// ResolveJobTemplate returns the Job template described by src, reading the referenced CronJob if needed.
// The returned CronJob is nil for inline templates.
func ResolveJobTemplate(ctx context.Context, c client.Reader, namespace string, src v1alpha1.JobSource) (*batchv1.JobTemplateSpec, *batchv1.CronJob, error) {
	switch {
	case src.Template != nil:
		return src.Template.DeepCopy(), nil, nil
	case src.CronJobRef != nil:
		var cj batchv1.CronJob
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: src.CronJobRef.Name}, &cj); err != nil {
			return nil, nil, fmt.Errorf("getting cronjob %s/%s: %w", namespace, src.CronJobRef.Name, err)
		}
		return cj.Spec.JobTemplate.DeepCopy(), &cj, nil
	default:
		return nil, nil, fmt.Errorf("job source must set cronJobRef or template")
	}
}

// CreateJobOptions configures CreateJob.
type CreateJobOptions struct {
	Namespace string
	// BaseName is used as generateName prefix.
	BaseName string
	// Owner becomes the controller owner of the Job (optional).
	Owner client.Object
	// OwnerKind is stored in the owner-kind label.
	OwnerKind string
	// CronJob, if set, is recorded as annotation like `kubectl create job --from`.
	CronJob                 *batchv1.CronJob
	TTLSecondsAfterFinished *int32
}

// CreateJob creates a Job from tmpl and returns it.
func CreateJob(ctx context.Context, c client.Client, scheme *runtime.Scheme, tmpl *batchv1.JobTemplateSpec, opts CreateJobOptions) (*batchv1.Job, error) {
	labels := map[string]string{}
	for k, v := range tmpl.Labels {
		labels[k] = v
	}
	labels[v1alpha1.LabelManagedBy] = v1alpha1.ManagedByValue
	if opts.Owner != nil {
		labels[v1alpha1.LabelOwnerKind] = opts.OwnerKind
		labels[v1alpha1.LabelOwnerName] = opts.Owner.GetName()
	}
	annotations := map[string]string{}
	for k, v := range tmpl.Annotations {
		annotations[k] = v
	}
	if opts.CronJob != nil {
		annotations["cronjob.kubernetes.io/instantiate"] = "manual"
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: truncate(opts.BaseName, 52) + "-",
			Namespace:    opts.Namespace,
			Labels:       labels,
			Annotations:  annotations,
		},
		Spec: *tmpl.Spec.DeepCopy(),
	}
	if job.Spec.TTLSecondsAfterFinished == nil && opts.TTLSecondsAfterFinished != nil {
		ttl := *opts.TTLSecondsAfterFinished
		job.Spec.TTLSecondsAfterFinished = &ttl
	}
	if opts.Owner != nil {
		if err := controllerutil.SetControllerReference(opts.Owner, job, scheme); err != nil {
			return nil, err
		}
	} else if opts.CronJob != nil {
		job.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(opts.CronJob, batchv1.SchemeGroupVersion.WithKind("CronJob"))}
	}
	if err := c.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("creating job: %w", err)
	}
	return job, nil
}

// CountActiveOwnedJobs counts unfinished Jobs created for the given owner.
func CountActiveOwnedJobs(ctx context.Context, c client.Reader, namespace, ownerKind, ownerName string) (int, error) {
	var jobs batchv1.JobList
	if err := c.List(ctx, &jobs, client.InNamespace(namespace), client.MatchingLabels{
		v1alpha1.LabelOwnerKind: ownerKind,
		v1alpha1.LabelOwnerName: ownerName,
	}); err != nil {
		return 0, err
	}
	n := 0
	for i := range jobs.Items {
		if !IsJobFinished(&jobs.Items[i]) {
			n++
		}
	}
	return n, nil
}

// IsJobFinished reports whether a Job completed or failed.
func IsJobFinished(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
