// Package fake provides an in-memory queue.Provider for tests and local development.
package fake

import (
	"context"
	"strconv"
	"sync"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
)

// Queue is an in-memory queue.
type Queue struct {
	mu       sync.Mutex
	messages []queue.Message
	next     int
	// Deleted records deleted message IDs.
	Deleted []string
	// Err, when set, is returned by every call.
	Err error
}

// Push appends messages with the given bodies.
func (q *Queue) Push(bodies ...string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, b := range bodies {
		q.next++
		id := strconv.Itoa(q.next)
		q.messages = append(q.messages, queue.Message{ID: id, Body: b, Handle: id})
	}
}

// ApproximateDepth implements queue.Provider.
func (q *Queue) ApproximateDepth(context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.Err != nil {
		return 0, q.Err
	}
	return len(q.messages), nil
}

// Receive implements queue.Provider (messages stay until deleted).
func (q *Queue) Receive(_ context.Context, opts queue.ReceiveOptions) ([]queue.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.Err != nil {
		return nil, q.Err
	}
	n := opts.MaxMessages
	if n <= 0 || n > len(q.messages) {
		n = len(q.messages)
	}
	return append([]queue.Message(nil), q.messages[:n]...), nil
}

// Delete implements queue.Provider.
func (q *Queue) Delete(_ context.Context, msg queue.Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.Err != nil {
		return q.Err
	}
	for i, m := range q.messages {
		if m.Handle == msg.Handle {
			q.messages = append(q.messages[:i], q.messages[i+1:]...)
			q.Deleted = append(q.Deleted, m.ID)
			return nil
		}
	}
	return nil
}

// Factory returns the same fake queue for every source URL it knows.
type Factory map[string]*Queue

// For implements queue.Factory.
func (f Factory) For(_ context.Context, src v1alpha1.QueueSource) (queue.Provider, error) {
	q, ok := f[src.URL]
	if !ok {
		q = &Queue{}
		f[src.URL] = q
	}
	return q, nil
}
