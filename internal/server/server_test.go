package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/posttest"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
	"github.com/slashsre/slashsre-devops-controller/internal/remediation"
)

type noopJobs struct{}

func (noopJobs) TriggerNowByKey(context.Context, client.ObjectKey) (string, error) {
	return "job-1", nil
}

func newTestServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = v1alpha1.AddToScheme(s)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(&v1alpha1.AlertRemediation{
		ObjectMeta: metav1.ObjectMeta{Name: "cpu", Namespace: "shop"},
		Spec:       v1alpha1.AlertRemediationSpec{AlertName: "CPUThrottlingHigh"},
	}).Build()
	rec := record.NewFakeRecorder(10)
	return New(cfg, zap.NewNop(), c,
		&remediation.Executor{Client: c, Recorder: rec, Log: logr.Discard()},
		&posttest.Runner{Client: c, Scheme: s, Pipelines: pipeline.NewRegistry(), Recorder: rec, Log: logr.Discard()},
		noopJobs{}, nil)
}

func do(t *testing.T, h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHealthAndResources(t *testing.T) {
	h := newTestServer(t, Config{}).Handler()
	if rr := do(t, h, http.MethodGet, "/healthz", "", nil); rr.Code != http.StatusOK {
		t.Errorf("healthz = %d", rr.Code)
	}
	rr := do(t, h, http.MethodGet, "/api/v1/resources", "", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"alertRemediations"`) || !strings.Contains(rr.Body.String(), `"cpu"`) {
		t.Errorf("resources = %d %s", rr.Code, rr.Body.String())
	}
}

func TestAlertmanagerWebhook(t *testing.T) {
	h := newTestServer(t, Config{}).Handler()
	if rr := do(t, h, http.MethodPost, "/webhooks/alertmanager", "{bad", nil); rr.Code != http.StatusBadRequest {
		t.Errorf("bad payload = %d", rr.Code)
	}
	body := `{"status":"firing","alerts":[{"status":"firing","labels":{"alertname":"Other","namespace":"shop","pod":"p"}}]}`
	if rr := do(t, h, http.MethodPost, "/webhooks/alertmanager", body, nil); rr.Code != http.StatusOK {
		t.Errorf("no match = %d", rr.Code)
	}
	// Legacy alias still routed.
	if rr := do(t, h, http.MethodPost, "/alertmanager/custom", `{"status":"resolved"}`, nil); rr.Code != http.StatusOK {
		t.Errorf("legacy alias = %d", rr.Code)
	}
}

func TestTokens(t *testing.T) {
	h := newTestServer(t, Config{APIToken: "api", WebhookToken: "hook"}).Handler()
	path := "/api/v1/namespaces/shop/queuetriggeredjobs/x/run"
	if rr := do(t, h, http.MethodPost, path, "", nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("missing api token = %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, path, "", map[string]string{"Authorization": "Bearer api"}); rr.Code != http.StatusAccepted {
		t.Errorf("valid api token = %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, "/webhooks/flux", `{}`, nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("missing webhook token = %d", rr.Code)
	}
	if rr := do(t, h, http.MethodPost, "/webhooks/flux", `{}`, map[string]string{"Authorization": "Bearer hook"}); rr.Code != http.StatusAccepted {
		t.Errorf("valid webhook token = %d", rr.Code)
	}
	// Read-only endpoints stay open.
	if rr := do(t, h, http.MethodGet, "/api/v1/resources", "", nil); rr.Code != http.StatusOK {
		t.Errorf("resources = %d", rr.Code)
	}
}

func TestPostDeployTestRunNotFound(t *testing.T) {
	h := newTestServer(t, Config{}).Handler()
	if rr := do(t, h, http.MethodPost, "/api/v1/namespaces/qa/postdeploytests/missing/run", "", nil); rr.Code != http.StatusNotFound {
		t.Errorf("missing = %d", rr.Code)
	}
}
