package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type requestContextKey struct{}

// NewRequestID returns a short random identifier used to correlate one MCP
// tool call with every provider event it caused.
func NewRequestID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "req-" + time.Now().UTC().Format("20060102150405")
	}
	return "req-" + hex.EncodeToString(buf)
}

// WithRequestID attaches a request identifier to a context. Tool handlers set
// this once; every provider that records through RecordContext inherits it.
func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestContextKey{}, id)
}

// clientContextKey carries the MCP host identity (derived from User-Agent)
// so every event recorded during the call is attributed to one client.
type clientContextKey struct{}

// WithClient attaches the MCP client identity to the context.
func WithClient(ctx context.Context, client string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == "" {
		return ctx
	}
	return context.WithValue(ctx, clientContextKey{}, client)
}

// ClientFromContext returns the MCP client identity attached by WithClient, if any.
func ClientFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(clientContextKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestID returns the identifier attached by WithRequestID, if any.
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestContextKey{}).(string); ok {
		return id
	}
	return ""
}

// RequestCollector is a request-scoped event buffer. Tool handlers create one,
// flush it at the end of the call and pass it to RecordContext so provider
// goroutines do not need to thread their context through search interfaces.
type RequestCollector struct {
	mu     sync.Mutex
	events []Event
}

// Add appends an event to the collector.
func (c *RequestCollector) Add(event Event) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

// Flush records every buffered event and returns the count. A nil store or
// collector is a no-op.
func (c *RequestCollector) Flush(store *Store) int {
	if c == nil || store == nil {
		return 0
	}
	c.mu.Lock()
	events := c.events
	c.events = nil
	c.mu.Unlock()
	for _, event := range events {
		_ = store.Record(event)
	}
	return len(events)
}

// EventCount reports how many events are currently buffered.
func (c *RequestCollector) EventCount() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// EventSnapshot returns a copy of the buffered events without clearing them.
func (c *RequestCollector) EventSnapshot() []Event {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

// CollectorFromContext returns the collector attached by WithRequestCollector.
func CollectorFromContext(ctx context.Context) *RequestCollector {
	if ctx == nil {
		return nil
	}
	if collector, ok := ctx.Value(collectorContextKey{}).(*RequestCollector); ok {
		return collector
	}
	return nil
}

type collectorContextKey struct{}

// WithRequestCollector attaches a request-scoped collector to a context.
func WithRequestCollector(ctx context.Context, collector *RequestCollector) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if collector == nil {
		return ctx
	}
	return context.WithValue(ctx, collectorContextKey{}, collector)
}

// RecordContext records an event, stamping the request ID and routing the
// event into the request collector when one is attached. Callers should prefer
// this over Record whenever they have a context.
func (s *Store) RecordContext(ctx context.Context, event Event) {
	if s == nil {
		return
	}
	if event.RequestID == "" {
		event.RequestID = RequestID(ctx)
	}
	if event.Client == "" {
		event.Client = ClientFromContext(ctx)
	}
	if collector := CollectorFromContext(ctx); collector != nil {
		collector.Add(event)
		return
	}
	_ = s.Record(event)
}

// RecordEventContext routes an event through the default store and, when a
// request collector is attached, also buffers it so tool handlers can derive
// the per-request provider chain.
func RecordEventContext(ctx context.Context, event Event) {
	if event.RequestID == "" {
		event.RequestID = RequestID(ctx)
	}
	if event.Client == "" {
		event.Client = ClientFromContext(ctx)
	}
	if collector := CollectorFromContext(ctx); collector != nil {
		collector.Add(event)
	}
	Record(event)
}

// RecordContext records through the default store.
func RecordContext(ctx context.Context, event Event) {
	if store := Default(); store != nil {
		store.RecordContext(ctx, event)
	}
}
