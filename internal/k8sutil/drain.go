package k8sutil

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

// FindNodeByInstanceID returns the node whose spec.providerID ends with instanceID,
// or nil when none matches (or it does not match selector).
func FindNodeByInstanceID(ctx context.Context, c client.Reader, instanceID string, selector *metav1.LabelSelector) (*corev1.Node, error) {
	opts := []client.ListOption{}
	if selector != nil {
		sel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, fmt.Errorf("invalid node selector: %w", err)
		}
		if sel != labels.Everything() {
			opts = append(opts, client.MatchingLabelsSelector{Selector: sel})
		}
	}
	var nodes corev1.NodeList
	if err := c.List(ctx, &nodes, opts...); err != nil {
		return nil, err
	}
	for i := range nodes.Items {
		if ProviderInstanceID(nodes.Items[i].Spec.ProviderID) == instanceID {
			return &nodes.Items[i], nil
		}
	}
	return nil, nil
}

// ProviderInstanceID returns the last path segment of a providerID
// (aws:///eu-central-1a/i-0abc -> i-0abc).
func ProviderInstanceID(providerID string) string {
	parts := strings.Split(providerID, "/")
	return parts[len(parts)-1]
}

// Drainer cordons and drains nodes.
type Drainer struct {
	Clientset kubernetes.Interface
	Log       logr.Logger
}

// Drain cordons node and evicts its pods according to opts.
func (d *Drainer) Drain(ctx context.Context, node *corev1.Node, opts v1alpha1.DrainOptions) error {
	out := &logWriter{log: d.Log.WithValues("node", node.Name)}
	timeout := 120 * time.Second
	if opts.Timeout != nil && opts.Timeout.Duration > 0 {
		timeout = opts.Timeout.Duration
	}
	grace := -1
	if opts.GracePeriodSeconds != nil {
		grace = int(*opts.GracePeriodSeconds)
	}
	helper := &drain.Helper{
		Ctx:                 ctx,
		Client:              d.Clientset,
		Force:               boolOr(opts.Force, true),
		IgnoreAllDaemonSets: boolOr(opts.IgnoreDaemonSets, true),
		DeleteEmptyDirData:  boolOr(opts.DeleteEmptyDirData, true),
		DisableEviction:     opts.DisableEviction,
		GracePeriodSeconds:  grace,
		Timeout:             timeout,
		Out:                 out,
		ErrOut:              out,
	}
	if err := drain.RunCordonOrUncordon(helper, node, true); err != nil {
		return fmt.Errorf("cordoning node %s: %w", node.Name, err)
	}
	if err := drain.RunNodeDrain(helper, node.Name); err != nil {
		return fmt.Errorf("draining node %s: %w", node.Name, err)
	}
	return nil
}

func boolOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

// logWriter forwards kubectl drain output to a logr.Logger.
type logWriter struct{ log logr.Logger }

func (w *logWriter) Write(p []byte) (int, error) {
	s := bufio.NewScanner(bytes.NewReader(p))
	for s.Scan() {
		if line := strings.TrimSpace(s.Text()); line != "" {
			w.log.Info("drain", "message", line)
		}
	}
	return len(p), nil
}
