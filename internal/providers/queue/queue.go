// Package queue defines the pluggable message queue abstraction used by the controllers.
package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
)

// Message is a single queue message.
type Message struct {
	ID     string
	Body   string
	Handle string // provider-specific token used to delete the message
}

// ReceiveOptions tunes a Receive call.
type ReceiveOptions struct {
	MaxMessages       int
	VisibilityTimeout time.Duration
	WaitTime          time.Duration
}

// Provider is implemented by every queue backend.
type Provider interface {
	// ApproximateDepth returns the approximate number of visible messages.
	ApproximateDepth(ctx context.Context) (int, error)
	// Receive returns up to opts.MaxMessages messages, hiding them for opts.VisibilityTimeout.
	Receive(ctx context.Context, opts ReceiveOptions) ([]Message, error)
	// Delete acknowledges a message.
	Delete(ctx context.Context, msg Message) error
}

// Constructor builds a Provider for a queue source.
type Constructor func(ctx context.Context, src v1alpha1.QueueSource) (Provider, error)

// Factory resolves QueueSources to Providers, caching instances.
type Factory interface {
	For(ctx context.Context, src v1alpha1.QueueSource) (Provider, error)
}

// Registry is a Factory backed by registered constructors.
type Registry struct {
	mu           sync.Mutex
	constructors map[v1alpha1.QueueProviderType]Constructor
	cache        map[v1alpha1.QueueSource]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		constructors: map[v1alpha1.QueueProviderType]Constructor{},
		cache:        map[v1alpha1.QueueSource]Provider{},
	}
}

// Register adds a constructor for a provider type.
func (r *Registry) Register(t v1alpha1.QueueProviderType, c Constructor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.constructors[t] = c
}

// For returns a (cached) provider for src.
func (r *Registry) For(ctx context.Context, src v1alpha1.QueueSource) (Provider, error) {
	if src.Provider == "" {
		src.Provider = v1alpha1.QueueProviderAWSSQS
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.cache[src]; ok {
		return p, nil
	}
	c, ok := r.constructors[src.Provider]
	if !ok {
		return nil, fmt.Errorf("unsupported queue provider %q", src.Provider)
	}
	p, err := c(ctx, src)
	if err != nil {
		return nil, err
	}
	r.cache[src] = p
	return p, nil
}
