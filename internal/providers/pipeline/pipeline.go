// Package pipeline defines the pluggable CI pipeline abstraction.
package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

// Credentials authenticate against the CI API.
type Credentials struct {
	Token    string
	Username string
	Password string
}

// Request describes a pipeline run.
type Request struct {
	Repository  string // owner/repo or workspace/repo
	Ref         string
	Pipeline    string
	Variables   map[string]string
	Credentials Credentials
	BaseURL     string
}

// Result describes a started run.
type Result struct {
	ID  string
	URL string
}

// Provider is implemented by every CI backend.
type Provider interface {
	Trigger(ctx context.Context, req Request) (Result, error)
}

// Registry maps provider types to implementations.
type Registry struct {
	mu        sync.RWMutex
	providers map[v1alpha1.PipelineProviderType]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[v1alpha1.PipelineProviderType]Provider{}}
}

// Register adds a provider.
func (r *Registry) Register(t v1alpha1.PipelineProviderType, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[t] = p
}

// Get returns the provider for t.
func (r *Registry) Get(t v1alpha1.PipelineProviderType) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[t]
	if !ok {
		return nil, fmt.Errorf("unsupported pipeline provider %q", t)
	}
	return p, nil
}

// DefaultHTTPClient is used by the built-in providers.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// APIError is returned when the CI API answers with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pipeline API returned %d: %s", e.StatusCode, e.Body)
}
