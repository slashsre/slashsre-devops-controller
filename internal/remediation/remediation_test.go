package remediation

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
)

func TestThresholdExceeded(t *testing.T) {
	cases := []struct {
		value, threshold string
		want             bool
		wantErr          bool
	}{
		{"98.55%", "50%", true, false},
		{"5%", "50%", false, false},
		{"100%", "50%", true, false}, // the legacy implementation compared only two characters
		{"50%", "50%", true, false},
		{"0.7", "0.5", true, false},
		{"", "50%", false, true},
		{"abc", "50%", false, true},
	}
	for _, c := range cases {
		got, err := ThresholdExceeded(c.value, c.threshold)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("ThresholdExceeded(%q,%q) = %v,%v", c.value, c.threshold, got, err)
		}
	}
}

func rule() *v1alpha1.AlertRemediation {
	return &v1alpha1.AlertRemediation{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "shop", UID: "r1"},
		Spec: v1alpha1.AlertRemediationSpec{
			AlertName:   "CPUThrottlingHigh",
			MatchLabels: map[string]string{"container": "checkout"},
			Threshold:   "50%",
			Cooldown:    metav1.Duration{Duration: 5 * time.Minute},
		},
	}
}

func alert(value string) Alert {
	return Alert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "CPUThrottlingHigh", "namespace": "shop", "container": "checkout", "pod": "checkout-abc"},
		Annotations: map[string]string{"value": value},
	}
}

func TestMatches(t *testing.T) {
	if ok, why := Matches(rule(), alert("80%")); !ok {
		t.Fatalf("expected match: %s", why)
	}
	a := alert("80%")
	a.Labels["namespace"] = "other"
	if ok, _ := Matches(rule(), a); ok {
		t.Error("cross-namespace alert must not match")
	}
	a = alert("80%")
	a.Labels["container"] = "cart"
	if ok, _ := Matches(rule(), a); ok {
		t.Error("label mismatch must not match")
	}
	if ok, _ := Matches(rule(), alert("10%")); ok {
		t.Error("below threshold must not match")
	}
	r := rule()
	r.Spec.Suspend = true
	if ok, _ := Matches(r, alert("80%")); ok {
		t.Error("suspended rule must not match")
	}
}

func scheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	return s
}

func TestExecutorDeletePodWithCooldown(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-abc", Namespace: "shop"}}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(rule(), pod).
		WithStatusSubresource(&v1alpha1.AlertRemediation{}).Build()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := &Executor{Client: c, Recorder: record.NewFakeRecorder(10), Log: logr.Discard(), Now: func() time.Time { return now }}
	ctx := context.Background()

	matches, err := e.FindMatches(ctx, WebhookMessage{Alerts: []Alert{alert("90%")}})
	if err != nil || len(matches) != 1 {
		t.Fatalf("matches = %v, %v", matches, err)
	}
	e.Execute(ctx, matches)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); !apierrors.IsNotFound(err) {
		t.Fatalf("pod should be deleted, got %v", err)
	}
	var got v1alpha1.AlertRemediation
	_ = c.Get(ctx, client.ObjectKey{Namespace: "shop", Name: "cpu"}, &got)
	if got.Status.Remediations != 1 || got.Status.LastTarget != "Pod/checkout-abc" {
		t.Errorf("status = %+v", got.Status)
	}

	// Same target within cooldown: skipped (pod recreated to detect deletion).
	_ = c.Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-abc", Namespace: "shop"}})
	now = now.Add(time.Minute)
	e.Execute(ctx, matches)
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); err != nil {
		t.Fatalf("pod should survive during cooldown: %v", err)
	}
}

func TestExecutorRolloutRestart(t *testing.T) {
	r := rule()
	r.Spec.Action = v1alpha1.ActionRolloutRestart
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: "shop", UID: "d1"}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "checkout-7d", Namespace: "shop", UID: "rs1",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: "checkout", UID: "d1", Controller: ptr.To(true)}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "checkout-abc", Namespace: "shop",
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "checkout-7d", UID: "rs1", Controller: ptr.To(true)}}}}
	c := fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(r, dep, rs, pod).
		WithStatusSubresource(&v1alpha1.AlertRemediation{}).Build()
	e := &Executor{Client: c, Recorder: record.NewFakeRecorder(10), Log: logr.Discard()}
	ctx := context.Background()

	matches, _ := e.FindMatches(ctx, WebhookMessage{Alerts: []Alert{alert("90%")}})
	e.Execute(ctx, matches)

	var got appsv1.Deployment
	_ = c.Get(ctx, client.ObjectKeyFromObject(dep), &got)
	if got.Spec.Template.Annotations[k8sutil.RestartedAtAnnotation] == "" {
		t.Fatal("deployment was not restarted")
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}); err != nil {
		t.Fatalf("pod must not be deleted on RolloutRestart: %v", err)
	}
}
