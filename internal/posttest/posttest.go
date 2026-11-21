// Package posttest matches deployment events to PostDeployTests and runs them.
package posttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
)

const kindPostDeployTest = "PostDeployTest"

// ErrCooldown is returned when a run is requested within the cooldown window.
var ErrCooldown = errors.New("post-deploy test is in cooldown")

// FluxEvent is the subset of the Flux notification-controller event used for matching.
type FluxEvent struct {
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

// MatchesFlux reports whether ev satisfies trigger.
func MatchesFlux(t *v1alpha1.FluxTrigger, ev FluxEvent) bool {
	if t == nil {
		return false
	}
	if t.Kind != "" && !strings.EqualFold(t.Kind, ev.InvolvedObject.Kind) {
		return false
	}
	if t.Name != "" && t.Name != ev.InvolvedObject.Name {
		return false
	}
	if t.Namespace != "" && t.Namespace != ev.InvolvedObject.Namespace {
		return false
	}
	if len(t.Reasons) > 0 && !slices.Contains(t.Reasons, ev.Reason) {
		return false
	}
	if t.MessageContains != "" && !strings.Contains(ev.Message, t.MessageContains) {
		return false
	}
	return true
}

// RunResult describes what a run started.
type RunResult struct {
	JobName     string `json:"jobName,omitempty"`
	PipelineURL string `json:"pipelineURL,omitempty"`
	PipelineID  string `json:"pipelineID,omitempty"`
}

// Runner executes PostDeployTests.
type Runner struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	Pipelines *pipeline.Registry
	Recorder  record.EventRecorder
	Log       logr.Logger
	Now       func() time.Time

	mu      sync.Mutex
	lastRun map[string]time.Time
}

// +kubebuilder:rbac:groups=slashsre.controller.io,resources=postdeploytests,verbs=get;list;watch
// +kubebuilder:rbac:groups=slashsre.controller.io,resources=postdeploytests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch

// HandleFluxEvent runs every non-suspended PostDeployTest matching ev.
// It returns the number of tests started.
func (r *Runner) HandleFluxEvent(ctx context.Context, ev FluxEvent) (int, error) {
	var list v1alpha1.PostDeployTestList
	if err := r.Client.List(ctx, &list); err != nil {
		return 0, err
	}
	started := 0
	trigger := fmt.Sprintf("flux %s %s/%s: %s", ev.InvolvedObject.Kind, ev.InvolvedObject.Namespace, ev.InvolvedObject.Name, ev.Reason)
	for i := range list.Items {
		pdt := &list.Items[i]
		if pdt.Spec.Suspend || !MatchesFlux(pdt.Spec.Trigger.Flux, ev) {
			continue
		}
		if _, err := r.Run(ctx, pdt, trigger, false); err != nil {
			if !errors.Is(err, ErrCooldown) {
				r.Log.Error(err, "post-deploy test failed to start", "test", client.ObjectKeyFromObject(pdt))
			}
			continue
		}
		started++
	}
	return started, nil
}

// Run starts the Job and/or pipeline of pdt. force bypasses the cooldown.
func (r *Runner) Run(ctx context.Context, pdt *v1alpha1.PostDeployTest, trigger string, force bool) (RunResult, error) {
	if !force && !r.reserve(pdt) {
		return RunResult{}, ErrCooldown
	}
	var res RunResult
	var errs []error

	if pdt.Spec.Job != nil {
		tmpl, cronJob, err := k8sutil.ResolveJobTemplate(ctx, r.Client, pdt.Namespace, *pdt.Spec.Job)
		if err == nil {
			var job interface{ GetName() string }
			job, err = k8sutil.CreateJob(ctx, r.Client, r.Scheme, tmpl, k8sutil.CreateJobOptions{
				Namespace: pdt.Namespace, BaseName: pdt.Name, Owner: pdt, OwnerKind: kindPostDeployTest,
				CronJob: cronJob, TTLSecondsAfterFinished: pdt.Spec.Job.TTLSecondsAfterFinished,
			})
			if err == nil {
				res.JobName = job.GetName()
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("job: %w", err))
		}
	}

	if p := pdt.Spec.Pipeline; p != nil {
		pr, err := r.triggerPipeline(ctx, pdt.Namespace, p)
		if err != nil {
			errs = append(errs, fmt.Errorf("pipeline: %w", err))
		} else {
			res.PipelineURL, res.PipelineID = pr.URL, pr.ID
		}
	}

	runErr := errors.Join(errs...)
	if runErr != nil {
		r.Recorder.Eventf(pdt, corev1.EventTypeWarning, "RunFailed", "%s: %v", trigger, runErr)
	} else {
		r.Recorder.Eventf(pdt, corev1.EventTypeNormal, "RunStarted", "%s (job=%q pipeline=%q)", trigger, res.JobName, res.PipelineURL)
	}
	r.recordStatus(ctx, pdt, trigger, res, runErr)
	return res, runErr
}

func (r *Runner) triggerPipeline(ctx context.Context, namespace string, p *v1alpha1.PipelineAction) (pipeline.Result, error) {
	provider, err := r.Pipelines.Get(p.Provider)
	if err != nil {
		return pipeline.Result{}, err
	}
	var secret corev1.Secret
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: p.CredentialsSecretRef.Name}, &secret); err != nil {
		return pipeline.Result{}, fmt.Errorf("reading credentials secret: %w", err)
	}
	return provider.Trigger(ctx, pipeline.Request{
		Repository: p.Repository,
		Ref:        p.Ref,
		Pipeline:   p.Pipeline,
		Variables:  p.Variables,
		BaseURL:    p.BaseURL,
		Credentials: pipeline.Credentials{
			Token:    string(secret.Data["token"]),
			Username: string(secret.Data["username"]),
			Password: string(secret.Data["password"]),
		},
	})
}

func (r *Runner) reserve(pdt *v1alpha1.PostDeployTest) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastRun == nil {
		r.lastRun = map[string]time.Time{}
	}
	cooldown := pdt.Spec.Cooldown.Duration
	if cooldown <= 0 {
		cooldown = time.Minute
	}
	now := r.now()
	key := string(pdt.UID)
	if t, ok := r.lastRun[key]; ok && now.Sub(t) < cooldown {
		return false
	}
	r.lastRun[key] = now
	return true
}

func (r *Runner) recordStatus(ctx context.Context, pdt *v1alpha1.PostDeployTest, trigger string, res RunResult, runErr error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cur v1alpha1.PostDeployTest
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(pdt), &cur); err != nil {
			return err
		}
		cur.Status.Runs++
		cur.Status.LastRunTime = &metav1.Time{Time: r.now()}
		cur.Status.LastTrigger = trigger
		cur.Status.LastJobName = res.JobName
		cur.Status.LastPipelineURL = res.PipelineURL
		cur.Status.LastError = ""
		if runErr != nil {
			cur.Status.LastError = runErr.Error()
		}
		return r.Client.Status().Update(ctx, &cur)
	})
	if err != nil {
		r.Log.Error(err, "updating PostDeployTest status", "test", client.ObjectKeyFromObject(pdt))
	}
}

func (r *Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}
