package server

import (
	"net/http"
	"runtime"

	"github.com/slashsre/slashsre-devops-controller/internal/version"
)

// RuntimeResponse describes the running process.
type RuntimeResponse struct {
	Hostname     string `json:"hostname"`
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	Message      string `json:"message"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	Runtime      string `json:"runtime"`
	NumGoroutine int    `json:"num_goroutine"`
	NumCPU       int    `json:"num_cpu"`
}

// infoHandler godoc
// @Summary Runtime information
// @Tags API
// @Produce json
// @Success 200 {object} server.RuntimeResponse
// @Router /api/info [get]
func (s *Server) infoHandler(w http.ResponseWriter, r *http.Request) {
	s.JSONResponse(w, r, RuntimeResponse{
		Hostname:     s.config.Hostname,
		Version:      version.VERSION,
		Revision:     version.REVISION,
		Message:      s.config.UIMessage,
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		Runtime:      runtime.Version(),
		NumGoroutine: runtime.NumGoroutine(),
		NumCPU:       runtime.NumCPU(),
	})
}

// versionHandler godoc
// @Summary Version
// @Tags API
// @Produce json
// @Success 200 {object} map[string]string
// @Router /version [get]
func (s *Server) versionHandler(w http.ResponseWriter, r *http.Request) {
	s.JSONResponse(w, r, map[string]string{"version": version.VERSION, "commit": version.REVISION})
}
