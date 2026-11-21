// Package server exposes the HTTP API, webhook receivers, metrics and the embedded UI.
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/slashsre/slashsre-devops-controller/internal/posttest"
	"github.com/slashsre/slashsre-devops-controller/internal/remediation"
	"github.com/slashsre/slashsre-devops-controller/internal/server/docs"
)

// @title slashsre-devops-controller API
// @version 1.0
// @description Kubernetes controller for node lifecycle, queue-triggered jobs, alert remediation and post-deploy tests.
// @contact.name Source Code
// @contact.url https://github.com/slashsre/slashsre-devops-controller
// @BasePath /
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

// Config holds HTTP server settings.
type Config struct {
	Host            string
	Port            int
	Domain          string
	Hostname        string
	UIMessage       string
	AllowedOrigins  string
	ShutdownTimeout time.Duration
	// APIToken protects mutating /api/v1 endpoints when set.
	APIToken string
	// WebhookToken protects /webhooks endpoints when set.
	WebhookToken string
}

// JobTrigger creates a Job for a QueueTriggeredJob on demand.
type JobTrigger interface {
	TriggerNowByKey(ctx context.Context, key client.ObjectKey) (string, error)
}

// Server is a manager.Runnable serving HTTP on every replica.
type Server struct {
	config      Config
	logger      *zap.Logger
	client      client.Client
	remediation *remediation.Executor
	tests       *posttest.Runner
	jobs        JobTrigger
	router      *mux.Router
	ready       atomic.Bool
	healthy     atomic.Bool
	// cacheSynced blocks until the manager cache is ready; nil means ready.
	cacheSynced func(ctx context.Context) bool
}

var _ manager.Runnable = (*Server)(nil)
var _ manager.LeaderElectionRunnable = (*Server)(nil)

// New builds a server.
func New(cfg Config, logger *zap.Logger, c client.Client, rem *remediation.Executor, tests *posttest.Runner, jobs JobTrigger, cacheSynced func(context.Context) bool) *Server {
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	s := &Server{
		config: cfg, logger: logger, client: c, remediation: rem, tests: tests, jobs: jobs,
		router: mux.NewRouter(), cacheSynced: cacheSynced,
	}
	s.healthy.Store(true)
	s.registerMiddlewares()
	s.registerHandlers()
	return s
}

// Handler returns the root HTTP handler (useful for tests).
func (s *Server) Handler() http.Handler { return s.router }

// NeedLeaderElection implements manager.LeaderElectionRunnable: webhooks are served by all replicas.
func (s *Server) NeedLeaderElection() bool { return false }

func (s *Server) registerHandlers() {
	r := s.router
	r.Handle("/metrics", promhttp.HandlerFor(ctrlmetrics.Registry, promhttp.HandlerOpts{}))

	r.HandleFunc("/version", s.versionHandler).Methods(http.MethodGet)
	r.HandleFunc("/healthz", s.healthzHandler).Methods(http.MethodGet)
	r.HandleFunc("/readyz", s.readyzHandler).Methods(http.MethodGet)
	r.HandleFunc("/api/info", s.infoHandler).Methods(http.MethodGet)

	// Webhook receivers. The legacy paths are kept as aliases for existing integrations.
	hooks := r.NewRoute().Subrouter()
	hooks.Use(s.tokenMiddleware(s.config.WebhookToken))
	hooks.HandleFunc("/webhooks/alertmanager", s.alertmanagerHandler).Methods(http.MethodPost, http.MethodPut)
	hooks.HandleFunc("/webhooks/flux", s.fluxHandler).Methods(http.MethodPost)
	hooks.HandleFunc("/alertmanager/custom", s.alertmanagerHandler).Methods(http.MethodPost, http.MethodPut)
	hooks.HandleFunc("/flux/notification", s.fluxHandler).Methods(http.MethodPost)

	api := r.PathPrefix("/api/v1").Subrouter()
	api.Use(s.corsMiddleware)
	api.HandleFunc("/resources", s.listResourcesHandler).Methods(http.MethodGet)
	mutating := api.NewRoute().Subrouter()
	mutating.Use(s.tokenMiddleware(s.config.APIToken))
	mutating.HandleFunc("/namespaces/{namespace}/postdeploytests/{name}/run", s.runPostDeployTestHandler).Methods(http.MethodPost)
	mutating.HandleFunc("/namespaces/{namespace}/queuetriggeredjobs/{name}/run", s.runQueueTriggeredJobHandler).Methods(http.MethodPost)
	api.PathPrefix("/").Methods(http.MethodOptions).HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	r.PathPrefix("/swagger/").Handler(httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))

	// The SPA must be last so it does not shadow API routes.
	spa, err := newSPAHandler()
	if err != nil {
		s.logger.Warn("UI not available", zap.Error(err))
		return
	}
	r.PathPrefix("/").Handler(spa)
}

func (s *Server) registerMiddlewares() {
	s.router.Use(NewPrometheusMiddleware().Handler)
	s.router.Use(NewLoggingMiddleware(s.logger).Handler)
	s.router.Use(versionMiddleware)
}

// tokenMiddleware requires "Authorization: Bearer <token>" when token is non-empty.
func (s *Server) tokenMiddleware(token string) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		want := []byte("Bearer " + token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
				s.ErrorResponse(w, r, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.config.AllowedOrigins != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.config.AllowedOrigins)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		next.ServeHTTP(w, r)
	})
}

// Start implements manager.Runnable.
func (s *Server) Start(ctx context.Context) error {
	if s.config.Port == 0 {
		<-ctx.Done()
		return nil
	}
	docs.SwaggerInfo.Host = s.config.Domain

	addr := net.JoinHostPort(s.config.Host, itoa(s.config.Port))
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("starting HTTP server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	go func() {
		if s.cacheSynced == nil || s.cacheSynced(ctx) {
			s.ready.Store(true)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	s.ready.Store(false)
	s.healthy.Store(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()
	s.logger.Info("shutting down HTTP server", zap.Duration("timeout", s.config.ShutdownTimeout))
	return srv.Shutdown(shutdownCtx)
}

func itoa(i int) string { return strconv.Itoa(i) }
