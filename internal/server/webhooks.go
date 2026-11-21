package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/slashsre/slashsre-devops-controller/internal/posttest"
	"github.com/slashsre/slashsre-devops-controller/internal/remediation"
)

const maxWebhookBody = 1 << 20 // 1 MiB

// alertmanagerHandler godoc
// @Summary Alertmanager webhook
// @Description Receives Alertmanager notifications and runs matching AlertRemediation actions.
// @Tags Webhooks
// @Accept json
// @Param payload body remediation.WebhookMessage true "Alertmanager webhook payload"
// @Success 200 {string} string "No matching rule"
// @Success 202 {string} string "Remediation started"
// @Failure 400 {object} server.ErrorBody
// @Security BearerAuth
// @Router /webhooks/alertmanager [post]
func (s *Server) alertmanagerHandler(w http.ResponseWriter, r *http.Request) {
	var msg remediation.WebhookMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWebhookBody)).Decode(&msg); err != nil {
		s.ErrorResponse(w, r, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	if msg.Status == "resolved" {
		w.WriteHeader(http.StatusOK)
		return
	}
	matches, err := s.remediation.FindMatches(r.Context(), msg)
	if err != nil {
		s.logger.Error("listing AlertRemediations", zap.Error(err))
		s.ErrorResponse(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	if len(matches) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	// Alertmanager times out quickly: answer now and remediate in the background.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.remediation.Execute(ctx, matches)
	}()
	w.WriteHeader(http.StatusAccepted)
}

// fluxHandler godoc
// @Summary Flux notification webhook
// @Description Receives Flux notification-controller events (generic provider) and starts matching PostDeployTests.
// @Tags Webhooks
// @Accept json
// @Param payload body posttest.FluxEvent true "Flux event"
// @Success 202 {object} server.FluxResponse
// @Failure 400 {object} server.ErrorBody
// @Security BearerAuth
// @Router /webhooks/flux [post]
func (s *Server) fluxHandler(w http.ResponseWriter, r *http.Request) {
	var ev posttest.FluxEvent
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWebhookBody)).Decode(&ev); err != nil {
		s.ErrorResponse(w, r, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	started, err := s.tests.HandleFluxEvent(r.Context(), ev)
	if err != nil {
		s.logger.Error("handling flux event", zap.Error(err))
		s.ErrorResponse(w, r, "internal error", http.StatusInternalServerError)
		return
	}
	s.JSONResponseCode(w, r, FluxResponse{Started: started}, http.StatusAccepted)
}

// FluxResponse reports how many PostDeployTests were started.
type FluxResponse struct {
	Started int `json:"started"`
}
