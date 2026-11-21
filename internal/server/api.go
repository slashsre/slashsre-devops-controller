package server

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/posttest"
)

// ResourcesResponse lists every custom resource managed by the controller.
type ResourcesResponse struct {
	// Items are full Kubernetes objects; see the CRD schemas for their fields.
	NodeLifecycleRules []v1alpha1.NodeLifecycleRule `json:"nodeLifecycleRules" swaggertype:"array,object"`
	QueueTriggeredJobs []v1alpha1.QueueTriggeredJob `json:"queueTriggeredJobs" swaggertype:"array,object"`
	AlertRemediations  []v1alpha1.AlertRemediation  `json:"alertRemediations" swaggertype:"array,object"`
	PostDeployTests    []v1alpha1.PostDeployTest    `json:"postDeployTests" swaggertype:"array,object"`
}

// RunResponse is returned by the run endpoints.
type RunResponse struct {
	JobName     string `json:"jobName,omitempty"`
	PipelineURL string `json:"pipelineURL,omitempty"`
	PipelineID  string `json:"pipelineID,omitempty"`
}

// listResourcesHandler godoc
// @Summary List resources
// @Description Lists all slashsre.controller.io resources with their status.
// @Tags API
// @Produce json
// @Success 200 {object} server.ResourcesResponse
// @Router /api/v1/resources [get]
func (s *Server) listResourcesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var (
		nlr  v1alpha1.NodeLifecycleRuleList
		qtj  v1alpha1.QueueTriggeredJobList
		arem v1alpha1.AlertRemediationList
		pdt  v1alpha1.PostDeployTestList
	)
	for _, l := range []client.ObjectList{&nlr, &qtj, &arem, &pdt} {
		if err := s.client.List(ctx, l); err != nil {
			s.logger.Error("listing resources", zap.Error(err))
			s.ErrorResponse(w, r, "internal error", http.StatusInternalServerError)
			return
		}
	}
	for i := range nlr.Items {
		nlr.Items[i].ManagedFields = nil
	}
	for i := range qtj.Items {
		qtj.Items[i].ManagedFields = nil
	}
	for i := range arem.Items {
		arem.Items[i].ManagedFields = nil
	}
	for i := range pdt.Items {
		pdt.Items[i].ManagedFields = nil
	}
	s.JSONResponse(w, r, ResourcesResponse{
		NodeLifecycleRules: orEmpty(nlr.Items),
		QueueTriggeredJobs: orEmpty(qtj.Items),
		AlertRemediations:  orEmpty(arem.Items),
		PostDeployTests:    orEmpty(pdt.Items),
	})
}

// runPostDeployTestHandler godoc
// @Summary Run a PostDeployTest
// @Description Starts the Job and/or pipeline of a PostDeployTest immediately, ignoring cooldown.
// @Tags API
// @Produce json
// @Param namespace path string true "Namespace"
// @Param name path string true "PostDeployTest name"
// @Success 202 {object} server.RunResponse
// @Failure 404 {object} server.ErrorBody
// @Failure 502 {object} server.ErrorBody
// @Security BearerAuth
// @Router /api/v1/namespaces/{namespace}/postdeploytests/{name}/run [post]
func (s *Server) runPostDeployTestHandler(w http.ResponseWriter, r *http.Request) {
	key := client.ObjectKey{Namespace: mux.Vars(r)["namespace"], Name: mux.Vars(r)["name"]}
	var pdt v1alpha1.PostDeployTest
	if err := s.client.Get(r.Context(), key, &pdt); err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	res, err := s.tests.Run(r.Context(), &pdt, "manual run via API", true)
	if err != nil && !errors.Is(err, posttest.ErrCooldown) {
		s.ErrorResponse(w, r, err.Error(), http.StatusBadGateway)
		return
	}
	s.JSONResponseCode(w, r, RunResponse{JobName: res.JobName, PipelineURL: res.PipelineURL, PipelineID: res.PipelineID}, http.StatusAccepted)
}

// runQueueTriggeredJobHandler godoc
// @Summary Run a QueueTriggeredJob
// @Description Creates a Job for a QueueTriggeredJob immediately, ignoring queue depth.
// @Tags API
// @Produce json
// @Param namespace path string true "Namespace"
// @Param name path string true "QueueTriggeredJob name"
// @Success 202 {object} server.RunResponse
// @Failure 404 {object} server.ErrorBody
// @Security BearerAuth
// @Router /api/v1/namespaces/{namespace}/queuetriggeredjobs/{name}/run [post]
func (s *Server) runQueueTriggeredJobHandler(w http.ResponseWriter, r *http.Request) {
	key := client.ObjectKey{Namespace: mux.Vars(r)["namespace"], Name: mux.Vars(r)["name"]}
	name, err := s.jobs.TriggerNowByKey(r.Context(), key)
	if err != nil {
		s.notFoundOrError(w, r, err)
		return
	}
	s.JSONResponseCode(w, r, RunResponse{JobName: name}, http.StatusAccepted)
}

func (s *Server) notFoundOrError(w http.ResponseWriter, r *http.Request, err error) {
	if apierrors.IsNotFound(err) {
		s.ErrorResponse(w, r, err.Error(), http.StatusNotFound)
		return
	}
	s.logger.Error("api request failed", zap.Error(err))
	s.ErrorResponse(w, r, err.Error(), http.StatusInternalServerError)
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
