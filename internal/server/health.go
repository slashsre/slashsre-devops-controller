package server

import (
	"net/http"
)

// healthzHandler godoc
// @Summary Liveness check
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /healthz [get]
func (s *Server) healthzHandler(w http.ResponseWriter, r *http.Request) {
	if s.healthy.Load() {
		s.JSONResponse(w, r, map[string]string{"status": "OK"})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}

// readyzHandler godoc
// @Summary Readiness check
// @Description Ready once the Kubernetes informer cache has synced.
// @Tags Health
// @Produce json
// @Success 200 {object} map[string]string
// @Router /readyz [get]
func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if s.ready.Load() {
		s.JSONResponse(w, r, map[string]string{"status": "OK"})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
}
