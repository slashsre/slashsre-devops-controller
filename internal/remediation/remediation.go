// Package remediation matches Alertmanager alerts against AlertRemediation rules and executes actions.
package remediation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
)

// Alert is a single Alertmanager alert (webhook payload v4).
type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
}

// WebhookMessage is the Alertmanager webhook payload.
type WebhookMessage struct {
	Version  string  `json:"version"`
	GroupKey string  `json:"groupKey"`
	Status   string  `json:"status"`
	Receiver string  `json:"receiver"`
	Alerts   []Alert `json:"alerts"`
}

// Match is a rule/alert pair that must be remediated.
type Match struct {
	Rule      *v1alpha1.AlertRemediation
	Namespace string
	Pod       string
}

// Matches reports whether alert triggers rule, and why not otherwise.
func Matches(rule *v1alpha1.AlertRemediation, alert Alert) (bool, string) {
	if rule.Spec.Suspend {
		return false, "rule suspended"
	}
	if alert.Status != "" && alert.Status != "firing" {
		return false, "alert not firing"
	}
	if alert.Labels["alertname"] != rule.Spec.AlertName {
		return false, "alertname mismatch"
	}
	if alert.Labels["namespace"] != rule.Namespace {
		return false, "namespace mismatch"
	}
	for k, v := range rule.Spec.MatchLabels {
		if alert.Labels[k] != v {
			return false, "label " + k + " mismatch"
		}
	}
	if podLabel(rule) == "" || alert.Labels[podLabel(rule)] == "" {
		return false, "alert has no pod label"
	}
	if rule.Spec.Threshold != "" {
		annotation := rule.Spec.ValueAnnotation
		if annotation == "" {
			annotation = "value"
		}
		ok, err := ThresholdExceeded(alert.Annotations[annotation], rule.Spec.Threshold)
		if err != nil {
			return false, err.Error()
		}
		if !ok {
			return false, "threshold not reached"
		}
	}
	return true, ""
}

func podLabel(rule *v1alpha1.AlertRemediation) string {
	if rule.Spec.PodLabel == "" {
		return "pod"
	}
	return rule.Spec.PodLabel
}

// ThresholdExceeded parses value and threshold (optionally suffixed with "%") and
// reports value >= threshold.
func ThresholdExceeded(value, threshold string) (bool, error) {
	v, err := parseNumber(value)
	if err != nil {
		return false, fmt.Errorf("invalid alert value %q: %w", value, err)
	}
	t, err := parseNumber(threshold)
	if err != nil {
		return false, fmt.Errorf("invalid threshold %q: %w", threshold, err)
	}
	return v >= t, nil
}

func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	return strconv.ParseFloat(s, 64)
}

// Executor runs remediation actions honoring per-target cooldowns.
type Executor struct {
	Client   client.Client
	Recorder record.EventRecorder
	Log      logr.Logger
	Now      func() time.Time

	mu   sync.Mutex
	last map[string]time.Time // rule UID + target pod -> last action
}

// +kubebuilder:rbac:groups=slashsre.controller.io,resources=alertremediations,verbs=get;list;watch
// +kubebuilder:rbac:groups=slashsre.controller.io,resources=alertremediations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;list;watch;patch

// FindMatches lists AlertRemediations and returns those triggered by msg.
func (e *Executor) FindMatches(ctx context.Context, msg WebhookMessage) ([]Match, error) {
	var rules v1alpha1.AlertRemediationList
	if err := e.Client.List(ctx, &rules); err != nil {
		return nil, err
	}
	var out []Match
	for _, alert := range msg.Alerts {
		for i := range rules.Items {
			rule := &rules.Items[i]
			ok, why := Matches(rule, alert)
			if !ok {
				if alert.Labels["alertname"] == rule.Spec.AlertName {
					e.Log.V(1).Info("alert not remediated", "rule", client.ObjectKeyFromObject(rule), "reason", why)
				}
				continue
			}
			out = append(out, Match{Rule: rule, Namespace: rule.Namespace, Pod: alert.Labels[podLabel(rule)]})
		}
	}
	return out, nil
}

// Execute runs the action of every match, skipping targets in cooldown.
func (e *Executor) Execute(ctx context.Context, matches []Match) {
	for _, m := range matches {
		if !e.reserve(m) {
			e.Log.Info("remediation skipped, target in cooldown", "rule", client.ObjectKeyFromObject(m.Rule), "pod", m.Pod)
			continue
		}
		var target string
		var err error
		switch m.Rule.Spec.Action {
		case v1alpha1.ActionRolloutRestart:
			target, err = k8sutil.RolloutRestartPodOwner(ctx, e.Client, m.Namespace, m.Pod, e.now())
		default:
			target, err = k8sutil.DeletePod(ctx, e.Client, m.Namespace, m.Pod)
		}
		if err != nil {
			e.Log.Error(err, "remediation failed", "rule", client.ObjectKeyFromObject(m.Rule), "pod", m.Pod)
			e.Recorder.Eventf(m.Rule, corev1.EventTypeWarning, "RemediationFailed", "%s: %v", m.Pod, err)
		} else {
			e.Log.Info("remediation executed", "rule", client.ObjectKeyFromObject(m.Rule), "target", target)
			e.Recorder.Eventf(m.Rule, corev1.EventTypeNormal, "Remediated", "%s %s", m.Rule.Spec.Action, target)
		}
		e.recordStatus(ctx, m.Rule, target, err)
	}
}

// reserve returns false if the target was remediated within the cooldown window.
func (e *Executor) reserve(m Match) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		e.last = map[string]time.Time{}
	}
	cooldown := m.Rule.Spec.Cooldown.Duration
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	key := string(m.Rule.UID) + "/" + m.Pod
	now := e.now()
	if t, ok := e.last[key]; ok && now.Sub(t) < cooldown {
		return false
	}
	e.last[key] = now
	return true
}

func (e *Executor) recordStatus(ctx context.Context, rule *v1alpha1.AlertRemediation, target string, actionErr error) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var cur v1alpha1.AlertRemediation
		if err := e.Client.Get(ctx, client.ObjectKeyFromObject(rule), &cur); err != nil {
			return err
		}
		if actionErr != nil {
			cur.Status.LastError = actionErr.Error()
		} else {
			cur.Status.LastError = ""
			cur.Status.LastTarget = target
			cur.Status.LastRemediationTime = &metav1.Time{Time: e.now()}
			cur.Status.Remediations++
		}
		return e.Client.Status().Update(ctx, &cur)
	})
	if err != nil {
		e.Log.Error(err, "updating AlertRemediation status", "rule", client.ObjectKeyFromObject(rule))
	}
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
