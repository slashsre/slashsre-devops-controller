package server

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/slashsre/slashsre-devops-controller/internal/version"
)

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func versionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-API-Version", version.VERSION)
		w.Header().Set("X-API-Revision", version.REVISION)
		next.ServeHTTP(w, r)
	})
}

// JSONResponse writes result with status 200.
func (s *Server) JSONResponse(w http.ResponseWriter, r *http.Request, result any) {
	s.JSONResponseCode(w, r, result, http.StatusOK)
}

// JSONResponseCode writes result with the given status.
func (s *Server) JSONResponseCode(w http.ResponseWriter, _ *http.Request, result any, code int) {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		s.logger.Error("JSON marshal failed", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

// ErrorResponse writes an ErrorBody with the given status.
func (s *Server) ErrorResponse(w http.ResponseWriter, r *http.Request, msg string, code int) {
	s.JSONResponseCode(w, r, ErrorBody{Code: code, Message: msg}, code)
}
