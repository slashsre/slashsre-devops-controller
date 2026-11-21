package posttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
)

type fakePipeline struct{ reqs []pipeline.Request }

func (f *fakePipeline) Trigger(_ context.Context, req pipeline.Request) (pipeline.Result, error) {
	f.reqs = append(f.reqs, req)
	return pipeline.Result{ID: "1", URL: "https://ci.example.com/runs/1"}, nil
}

func fluxEvent(reason, msg string) FluxEvent {
	var ev FluxEvent
	ev.InvolvedObject.Kind = "Kustomization"
	ev.InvolvedObject.Name = "shop"
	ev.InvolvedObject.Namespace = "flux-system"
	ev.Reason = reason
	ev.Message = msg
	return ev
}

func TestMatchesFlux(t *testing.T) {
	trig := &v1alpha1.FluxTrigger{Kind: "kustomization", Name: "shop", MessageContains: "Health check passed"}
	if !MatchesFlux(trig, fluxEvent("ReconciliationSucceeded", "Health check passed in 10s")) {
		t.Error("expected match")
	}
	if MatchesFlux(trig, fluxEvent("ReconciliationSucceeded", "applied revision")) {
		t.Error("message filter ignored")
	}
	if MatchesFlux(&v1alpha1.FluxTrigger{Reasons: []string{"Progressing"}}, fluxEvent("ReconciliationSucceeded", "")) {
		t.Error("reason filter ignored")
	}
	if MatchesFlux(nil, fluxEvent("Progressing", "")) {
		t.Error("nil trigger must not match")
	}
}

func TestHandleFluxEventRunsJobAndPipeline(t *testing.T) {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)

	pdt := &v1alpha1.PostDeployTest{
		ObjectMeta: metav1.ObjectMeta{Name: "smoke", Namespace: "qa", UID: "p1"},
		Spec: v1alpha1.PostDeployTestSpec{
			Trigger: v1alpha1.PostDeployTrigger{Flux: &v1alpha1.FluxTrigger{Name: "shop", Reasons: []string{"Progressing"}}},
			Job: &v1alpha1.JobSource{Template: &batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "t", Image: "busybox"}}},
			}}}},
			Pipeline: &v1alpha1.PipelineAction{
				Provider: v1alpha1.PipelineProviderGitHub, Repository: "acme/e2e", Pipeline: "smoke.yml",
				CredentialsSecretRef: v1alpha1.LocalObjectReference{Name: "ci"},
			},
			Cooldown: metav1.Duration{Duration: time.Minute},
		},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ci", Namespace: "qa"}, Data: map[string][]byte{"token": []byte("t0k")}}
	other := &v1alpha1.PostDeployTest{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "qa", UID: "p2"},
		Spec: v1alpha1.PostDeployTestSpec{
			Trigger: v1alpha1.PostDeployTrigger{Flux: &v1alpha1.FluxTrigger{Name: "billing"}},
			Job:     &v1alpha1.JobSource{CronJobRef: &v1alpha1.LocalObjectReference{Name: "missing"}},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(pdt, other, secret).
		WithStatusSubresource(&v1alpha1.PostDeployTest{}).Build()

	gh := &fakePipeline{}
	reg := pipeline.NewRegistry()
	reg.Register(v1alpha1.PipelineProviderGitHub, gh)
	r := &Runner{Client: c, Scheme: s, Pipelines: reg, Recorder: record.NewFakeRecorder(10), Log: logr.Discard()}
	ctx := context.Background()

	started, err := r.HandleFluxEvent(ctx, fluxEvent("Progressing", "update"))
	if err != nil || started != 1 {
		t.Fatalf("started = %d, %v", started, err)
	}
	if len(gh.reqs) != 1 || gh.reqs[0].Credentials.Token != "t0k" || gh.reqs[0].Repository != "acme/e2e" {
		t.Fatalf("pipeline requests = %+v", gh.reqs)
	}
	var jobs batchv1.JobList
	_ = c.List(ctx, &jobs, client.InNamespace("qa"))
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs = %d", len(jobs.Items))
	}
	var got v1alpha1.PostDeployTest
	_ = c.Get(ctx, client.ObjectKeyFromObject(pdt), &got)
	if got.Status.Runs != 1 || got.Status.LastPipelineURL == "" || got.Status.LastError != "" {
		t.Errorf("status = %+v", got.Status)
	}

	// Cooldown applies to webhook triggers but not to forced runs.
	if started, _ := r.HandleFluxEvent(ctx, fluxEvent("Progressing", "update")); started != 0 {
		t.Errorf("cooldown ignored")
	}
	if _, err := r.Run(ctx, pdt, "manual", false); !errors.Is(err, ErrCooldown) {
		t.Errorf("expected ErrCooldown, got %v", err)
	}
	if _, err := r.Run(ctx, pdt, "manual", true); err != nil {
		t.Errorf("forced run failed: %v", err)
	}
}
