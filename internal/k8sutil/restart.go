package k8sutil

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RestartedAtAnnotation is the annotation `kubectl rollout restart` sets.
const RestartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"

// DeletePod deletes a pod and returns "Pod/<name>".
func DeletePod(ctx context.Context, c client.Client, namespace, name string) (string, error) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
	if err := c.Delete(ctx, pod); err != nil {
		return "", fmt.Errorf("deleting pod %s/%s: %w", namespace, name, err)
	}
	return "Pod/" + name, nil
}

// RolloutRestartPodOwner finds the workload owning a pod (Deployment, StatefulSet or
// DaemonSet) and triggers a rolling restart. Returns "<Kind>/<name>".
func RolloutRestartPodOwner(ctx context.Context, c client.Client, namespace, podName string, now time.Time) (string, error) {
	var pod corev1.Pod
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: podName}, &pod); err != nil {
		return "", fmt.Errorf("getting pod %s/%s: %w", namespace, podName, err)
	}
	ref := metav1.GetControllerOf(&pod)
	if ref == nil {
		return "", fmt.Errorf("pod %s/%s has no controller", namespace, podName)
	}

	var target client.Object
	var template *corev1.PodTemplateSpec
	switch ref.Kind {
	case "ReplicaSet":
		var rs appsv1.ReplicaSet
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &rs); err != nil {
			return "", fmt.Errorf("getting replicaset: %w", err)
		}
		rsOwner := metav1.GetControllerOf(&rs)
		if rsOwner == nil || rsOwner.Kind != "Deployment" {
			return "", fmt.Errorf("replicaset %s is not owned by a Deployment", rs.Name)
		}
		var d appsv1.Deployment
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rsOwner.Name}, &d); err != nil {
			return "", fmt.Errorf("getting deployment: %w", err)
		}
		target, template = &d, &d.Spec.Template
	case "StatefulSet":
		var s appsv1.StatefulSet
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &s); err != nil {
			return "", fmt.Errorf("getting statefulset: %w", err)
		}
		target, template = &s, &s.Spec.Template
	case "DaemonSet":
		var ds appsv1.DaemonSet
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &ds); err != nil {
			return "", fmt.Errorf("getting daemonset: %w", err)
		}
		target, template = &ds, &ds.Spec.Template
	default:
		return "", fmt.Errorf("unsupported pod controller kind %q", ref.Kind)
	}

	patch := client.MergeFrom(target.DeepCopyObject().(client.Object))
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[RestartedAtAnnotation] = now.UTC().Format(time.RFC3339)
	if err := c.Patch(ctx, target, patch); err != nil {
		return "", fmt.Errorf("patching %s: %w", target.GetName(), err)
	}
	kind := ref.Kind
	if kind == "ReplicaSet" {
		kind = "Deployment"
	}
	return kind + "/" + target.GetName(), nil
}
